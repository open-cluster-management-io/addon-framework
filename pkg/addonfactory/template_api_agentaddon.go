package addonfactory

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/valyala/fasttemplate"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

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
	getValuesFuncs     []GetValuesFunc
	trimCRDDescription bool

	hubKubeClient       kubernetes.Interface
	addonClient         addonv1alpha1client.Interface
	addonLister         addonlisterv1alpha1.ManagedClusterAddOnLister
	addonTemplateLister addonlisterv1alpha1.AddOnTemplateLister
	addonName           string
	agentName           string
}

// NewCRDTemplateAgentAddon creates a CRDTemplateAgentAddon instance
func NewCRDTemplateAgentAddon(
	addonName string,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformers.SharedInformerFactory,
	getValuesFuncs ...GetValuesFunc,
) *CRDTemplateAgentAddon {
	a := &CRDTemplateAgentAddon{
		getValuesFuncs:     getValuesFuncs,
		trimCRDDescription: true,

		hubKubeClient:       hubKubeClient,
		addonClient:         addonClient,
		addonLister:         addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
		addonTemplateLister: addonInformers.Addon().V1alpha1().AddOnTemplates().Lister(),
		addonName:           addonName,
		agentName:           utilrand.String(5),
	}

	return a
}

func (a *CRDTemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	template, err := utils.GetDesiredAddOnTemplate(a.addonTemplateLister, addon)
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
			CSRConfigurations: utils.TemplateCSRConfigurationsFunc(a.addonName, a.agentName,
				utils.DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister)),
			PermissionConfig: utils.TemplatePermissionConfigFunc(a.addonName,
				utils.DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister),
				a.hubKubeClient),
			CSRApproveCheck: utils.TemplateCSRApproveCheckFunc(a.addonName, a.agentName,
				utils.DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister)),
			CSRSign: utils.TemplateCSRSignFunc(a.addonName, a.agentName,
				utils.DefaultDesiredAddonTemplateGetter(a.addonLister, a.addonTemplateLister),
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
	configValues, privateValues Values) ([]runtime.Object, error) {
	for index, obj := range objects {
		deployment, err := a.convertToDeployment(obj)
		if err != nil {
			continue
		}
		for _, decorator := range []decorateDeployment{
			a.injectEnvironments,
			a.injectVolumes,
			a.injectNodePlacement,
			a.overrideImages,
		} {
			err = decorator(template, deployment, orderedValues, configValues, privateValues)
			if err != nil {
				return objects, err
			}
		}
		objects[index] = deployment
	}

	return objects, nil
}

type decorateDeployment func(template *addonapiv1alpha1.AddOnTemplate, deployment *appsv1.Deployment,
	orderedValues orderedValues, configValues, privateValues Values) error

func (a *CRDTemplateAgentAddon) injectEnvironments(_ *addonapiv1alpha1.AddOnTemplate,
	deployment *appsv1.Deployment, orderedValues orderedValues, _, _ Values) error {

	envVars := make([]corev1.EnvVar, len(orderedValues))
	for index, value := range orderedValues {
		envVars[index] = corev1.EnvVar{
			Name:  value.name,
			Value: value.value,
		}
	}

	for j := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[j].Env = append(
			deployment.Spec.Template.Spec.Containers[j].Env,
			envVars...)
	}

	return nil
}

func (a *CRDTemplateAgentAddon) injectVolumes(template *addonapiv1alpha1.AddOnTemplate,
	deployment *appsv1.Deployment, _ orderedValues, _, _ Values) error {

	volumeMounts := []corev1.VolumeMount{}
	volumes := []corev1.Volume{}

	for _, registration := range template.Spec.Registration {
		if registration.Type == addonapiv1alpha1.RegistrationTypeKubeClient {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "hub-kubeconfig",
				MountPath: a.hubKubeconfigSecretMountPath(),
			})
			volumes = append(volumes, corev1.Volume{
				Name: "hub-kubeconfig",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: a.hubKubeconfigSecretName(),
					},
				},
			})
		}

		if registration.Type == addonapiv1alpha1.RegistrationTypeCustomSigner {
			if registration.CustomSigner == nil {
				return fmt.Errorf("custom signer is nil")
			}
			name := fmt.Sprintf("cert-%s", strings.ReplaceAll(
				strings.ReplaceAll(registration.CustomSigner.SignerName, "/", "-"),
				".", "-"))
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      name,
				MountPath: a.customSignedSecretMountPath(registration.CustomSigner.SignerName),
			})
			volumes = append(volumes, corev1.Volume{
				Name: name,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: a.getCustomSignedSecretName(a.addonName, registration.CustomSigner.SignerName),
					},
				},
			})
		}
	}

	if len(volumeMounts) == 0 || len(volumes) == 0 {
		return nil
	}

	for j := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[j].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[j].VolumeMounts, volumeMounts...)
	}

	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, volumes...)

	return nil
}

func (a *CRDTemplateAgentAddon) injectNodePlacement(_ *addonapiv1alpha1.AddOnTemplate,
	deployment *appsv1.Deployment, _ orderedValues, _, privateValues Values) error {

	nodePlacement, ok := privateValues[NodePlacementPrivateValueKey]
	if !ok {
		return nil
	}

	np, ok := nodePlacement.(*addonapiv1alpha1.NodePlacement)
	if !ok {
		return fmt.Errorf("node placement value is invalid")
	}

	if np.NodeSelector != nil {
		deployment.Spec.Template.Spec.NodeSelector = np.NodeSelector
	}

	if np.NodeSelector != nil {
		deployment.Spec.Template.Spec.Tolerations = np.Tolerations
	}

	return nil
}

func (a *CRDTemplateAgentAddon) overrideImages(_ *addonapiv1alpha1.AddOnTemplate,
	deployment *appsv1.Deployment, _ orderedValues, _, privateValues Values) error {

	registries, ok := privateValues[RegistriesPrivateValueKey]
	if !ok {
		return nil
	}

	ims, ok := registries.([]addonapiv1alpha1.ImageMirror)
	if !ok {
		return fmt.Errorf("registries value is invalid")
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].Image = OverrideImage(
			ims, deployment.Spec.Template.Spec.Containers[i].Image)
	}

	return nil
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

type keyValuePair struct {
	name  string
	value string
}

type orderedValues []keyValuePair

func (a *CRDTemplateAgentAddon) getValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate,
) (orderedValues, map[string]interface{}, map[string]interface{}, error) {

	presetValues := make([]keyValuePair, 0)
	overrideValues := map[string]interface{}{}
	privateValues := map[string]interface{}{}

	defaultSortedKeys, defaultValues, err := a.getDefaultValues(cluster, addon, template)
	if err != nil {
		return presetValues, overrideValues, privateValues, nil
	}
	overrideValues = MergeValues(overrideValues, defaultValues)

	privateValuesKeys := map[string]struct{}{
		NodePlacementPrivateValueKey: {},
		RegistriesPrivateValueKey:    {},
	}

	for i := 0; i < len(a.getValuesFuncs); i++ {
		if a.getValuesFuncs[i] != nil {
			userValues, err := a.getValuesFuncs[i](cluster, addon)
			if err != nil {
				return nil, nil, nil, err
			}

			publicValues := map[string]interface{}{}
			for k, v := range userValues {
				if _, ok := privateValuesKeys[k]; ok {
					privateValues[k] = v
					continue
				}
				publicValues[k] = v
			}

			overrideValues = MergeValues(overrideValues, publicValues)
		}
	}
	builtinSortedKeys, builtinValues, err := a.getBuiltinValues(cluster, addon)
	if err != nil {
		return presetValues, overrideValues, privateValues, nil
	}
	overrideValues = MergeValues(overrideValues, builtinValues)

	for k, v := range overrideValues {
		_, ok := v.(string)
		if !ok {
			return nil, nil, nil, fmt.Errorf("only support string type for variables, invalid key %s", k)
		}
	}

	keys := append(defaultSortedKeys, builtinSortedKeys...)

	for _, key := range keys {
		presetValues = append(presetValues, keyValuePair{
			name:  key,
			value: overrideValues[key].(string),
		})
	}
	return presetValues, overrideValues, privateValues, nil
}

func (a *CRDTemplateAgentAddon) getBuiltinValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]string, Values, error) {
	builtinValues := templateCRDBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	builtinValues.AddonInstallNamespace = installNamespace

	value, err := JsonStructToValues(builtinValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) getDefaultValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate) ([]string, Values, error) {
	defaultValues := templateCRDDefaultValues{}

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if template.Spec.Registration != nil {
		defaultValues.HubKubeConfigPath = a.hubKubeconfigPath()
	}

	value, err := JsonStructToValues(defaultValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) sortValueKeys(value Values) []string {
	keys := make([]string, 0)
	for k := range value {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	return keys
}

func (a *CRDTemplateAgentAddon) hubKubeconfigPath() string {
	return "/managed/hub-kubeconfig/kubeconfig"
}

func (a *CRDTemplateAgentAddon) hubKubeconfigSecretMountPath() string {
	return "/managed/hub-kubeconfig"
}

func (a *CRDTemplateAgentAddon) hubKubeconfigSecretName() string {
	return fmt.Sprintf("%s-hub-kubeconfig", a.addonName)
}

func (a *CRDTemplateAgentAddon) getCustomSignedSecretName(addonName, signerName string) string {
	return fmt.Sprintf("%s-%s-client-cert", addonName, strings.ReplaceAll(signerName, "/", "-"))
}

func (a *CRDTemplateAgentAddon) customSignedSecretMountPath(signerName string) string {
	return fmt.Sprintf("/managed/%s", strings.ReplaceAll(signerName, "/", "-"))
}
