package templateagent

import (
	"fmt"

	"github.com/valyala/fasttemplate"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	rbacv1lister "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

const (
	NodePlacementPrivateValueKey = "__NODE_PLACEMENT"
	RegistriesPrivateValueKey    = "__REGISTRIES"
)

// templateBuiltinValues includes the built-in values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the built-in values can not be overridden by getValuesFuncs
type templateCRDBuiltinValues struct {
	ClusterName           string `json:"CLUSTER_NAME,omitempty"`
	AddonInstallNamespace string `json:"INSTALL_NAMESPACE,omitempty"`
}

// templateDefaultValues includes the default values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the default values can be overridden by getValuesFuncs
type templateCRDDefaultValues struct {
	HubKubeConfigPath     string `json:"HUB_KUBECONFIG,omitempty"`
	ManagedKubeConfigPath string `json:"MANAGED_KUBECONFIG,omitempty"`
}

type CRDTemplateAgentAddon struct {
	getValuesFuncs     []addonfactory.GetValuesFunc
	trimCRDDescription bool

	hubKubeClient       kubernetes.Interface
	addonClient         addonv1alpha1client.Interface
	addonLister         addonlisterv1alpha1.ManagedClusterAddOnLister
	addonTemplateLister addonlisterv1alpha1.AddOnTemplateLister
	rolebindingLister   rbacv1lister.RoleBindingLister
	addonName           string
	agentName           string
}

// NewCRDTemplateAgentAddon creates a CRDTemplateAgentAddon instance
func NewCRDTemplateAgentAddon(
	addonName, agentName string,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformers.SharedInformerFactory,
	rolebindingLister rbacv1lister.RoleBindingLister,
	getValuesFuncs ...addonfactory.GetValuesFunc,
) *CRDTemplateAgentAddon {

	a := &CRDTemplateAgentAddon{
		getValuesFuncs:     getValuesFuncs,
		trimCRDDescription: true,

		hubKubeClient:       hubKubeClient,
		addonClient:         addonClient,
		addonLister:         addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
		addonTemplateLister: addonInformers.Addon().V1alpha1().AddOnTemplates().Lister(),
		rolebindingLister:   rolebindingLister,
		addonName:           addonName,
		agentName:           agentName,
	}

	return a
}

func (a *CRDTemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	template, err := a.GetDesiredAddOnTemplateByAddon(addon)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("addon %s/%s template not found in status", addon.Namespace, addon.Name)
	}
	return a.renderObjects(cluster, addon, template)
}

func (a *CRDTemplateAgentAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	// TODO: consider a new way for developers to define their supported config GVRs
	supportedConfigGVRs := []schema.GroupVersionResource{}
	for gvr := range utils.BuiltInAddOnConfigGVRs {
		supportedConfigGVRs = append(supportedConfigGVRs, gvr)
	}
	return agent.AgentAddonOptions{
		AddonName:           a.addonName,
		InstallStrategy:     nil,
		HealthProber:        nil,
		SupportedConfigGVRs: supportedConfigGVRs,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: a.TemplateCSRConfigurationsFunc(),
			PermissionConfig:  a.TemplatePermissionConfigFunc(),
			CSRApproveCheck:   a.TemplateCSRApproveCheckFunc(),
			CSRSign:           a.TemplateCSRSignFunc(),
		},
	}
}

func (a *CRDTemplateAgentAddon) renderObjects(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate) ([]runtime.Object, error) {
	var objects []runtime.Object
	presetValues, configValues, privateValues, err := a.getValues(cluster, addon, template)
	if err != nil {
		return objects, err
	}
	klog.V(4).Infof("presetValues %v\t configValues: %v\t privateValues: %v", presetValues, configValues, privateValues)

	for _, manifest := range template.Spec.AgentSpec.Workload.Manifests {
		t := fasttemplate.New(string(manifest.Raw), "{{", "}}")
		manifestStr := t.ExecuteString(configValues)
		klog.V(4).Infof("addon %s/%s render result: %v", addon.Namespace, addon.Name, manifestStr)
		object := &unstructured.Unstructured{}
		if err := object.UnmarshalJSON([]byte(manifestStr)); err != nil {
			return objects, err
		}
		objects = append(objects, object)
	}

	objects, err = a.decorateObjects(template, objects, presetValues, configValues, privateValues)
	if err != nil {
		return objects, err
	}
	return objects, nil
}

func (a *CRDTemplateAgentAddon) decorateObjects(
	template *addonapiv1alpha1.AddOnTemplate,
	objects []runtime.Object,
	orderedValues orderedValues,
	configValues, privateValues addonfactory.Values) ([]runtime.Object, error) {
	decorators := []deploymentDecorator{
		newEnvironmentDecorator(orderedValues),
		newVolumeDecorator(a.addonName, template),
		newNodePlacementDecorator(privateValues),
		newImageDecorator(privateValues),
	}
	for index, obj := range objects {
		deployment, err := a.convertToDeployment(obj)
		if err != nil {
			continue
		}

		for _, decorator := range decorators {
			err = decorator.decorate(deployment)
			if err != nil {
				return objects, err
			}
		}
		objects[index] = deployment
	}

	return objects, nil
}

func (a *CRDTemplateAgentAddon) convertToDeployment(obj runtime.Object) (*appsv1.Deployment, error) {
	if obj.GetObjectKind().GroupVersionKind().Group != "apps" ||
		obj.GetObjectKind().GroupVersionKind().Kind != "Deployment" {
		return nil, fmt.Errorf("not deployment object, %v", obj.GetObjectKind())
	}

	deployment := &appsv1.Deployment{}
	uobj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return deployment, fmt.Errorf("not unstructured object, %v", obj.GetObjectKind())
	}

	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(uobj.Object, deployment)
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// GetDesiredAddOnTemplateByAddon returns the desired template of the addon
func (a *CRDTemplateAgentAddon) GetDesiredAddOnTemplateByAddon(
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.AddOnTemplate, error) {
	ok, templateRef := AddonTemplateConfigRef(addon.Status.ConfigReferences)
	if !ok {
		klog.V(4).Infof("Addon %s template config in status is empty", addon.Name)
		return nil, nil
	}

	desiredTemplate := templateRef.DesiredConfig
	if desiredTemplate == nil || desiredTemplate.SpecHash == "" {
		klog.Infof("Addon %s template spec hash is empty", addon.Name)
		return nil, fmt.Errorf("addon %s template desired spec hash is empty", addon.Name)
	}

	template, err := a.addonTemplateLister.Get(desiredTemplate.Name)
	if err != nil {
		return nil, err
	}

	return template.DeepCopy(), nil
}
