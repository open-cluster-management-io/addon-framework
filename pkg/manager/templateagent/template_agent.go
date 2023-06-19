package templateagent

import (
	"fmt"
	"sync"

	"github.com/valyala/fasttemplate"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
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
	addonName string,
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
		// TODO: agentName should not be changed after restarting the agent
		agentName: utilrand.String(5),
	}

	return a
}

func (a *CRDTemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	template, err := GetDesiredAddOnTemplate(a.addonTemplateLister, addon)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("addon %s/%s template not found in status", addon.Namespace, addon.Name)
	}
	return a.renderObjects(cluster, addon, template)
}

func (a *CRDTemplateAgentAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:       a.addonName,
		InstallStrategy: nil,
		HealthProber:    nil,
		// set supportedConfigGVRs to empty to disable the framework to start duplicated config related controllers
		SupportedConfigGVRs: []schema.GroupVersionResource{},
		Registration: &agent.RegistrationOption{
			CSRConfigurations: TemplateCSRConfigurationsFunc(a.addonName, a.agentName,
				DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister)),
			PermissionConfig: TemplatePermissionConfigFunc(a.addonName,
				DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister),
				a.hubKubeClient, a.rolebindingLister),
			CSRApproveCheck: TemplateCSRApproveCheckFunc(a.addonName, a.agentName,
				DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister)),
			CSRSign: TemplateCSRSignFunc(a.addonName, a.agentName,
				DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister),
				a.hubKubeClient),
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

	var wg sync.WaitGroup
	wg.Add(1)
	var gerr error
	go func() {
		defer wg.Done()

		for _, manifest := range template.Spec.AgentSpec.Workload.Manifests {

			t := fasttemplate.New(string(manifest.Raw), "{{", "}}")
			manifestStr := t.ExecuteString(configValues)
			klog.V(4).Infof("addon %s/%s render result: %v", addon.Namespace, addon.Name, manifestStr)
			object := &unstructured.Unstructured{}
			if err := object.UnmarshalJSON([]byte(manifestStr)); err != nil {
				gerr = err
				return
			}
			objects = append(objects, object)
		}
	}()
	wg.Wait()
	if gerr != nil {
		return objects, gerr
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
	if ok {
		err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(uobj.Object, deployment)
		if err != nil {
			return nil, err
		}
		return deployment, nil
	}

	deployment, ok = obj.(*appsv1.Deployment)
	if ok {
		return deployment, nil
	}

	return nil, fmt.Errorf("not deployment object, %v", obj.GetObjectKind())
}
