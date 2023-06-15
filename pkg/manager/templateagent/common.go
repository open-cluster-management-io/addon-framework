package templateagent

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/utils"
)

// GetDesiredAddOnTemplate returns the desired template of the addon
func GetDesiredAddOnTemplate(
	atLister addonlisterv1alpha1.AddOnTemplateLister,
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

	template, err := atLister.Get(desiredTemplate.Name)
	if err != nil {
		return nil, err
	}

	return template.DeepCopy(), nil
}

// AddonTemplateConfigRef return the first addon template config
func AddonTemplateConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference) (bool, addonapiv1alpha1.ConfigReference) {
	for _, config := range configReferences {
		if config.Group == utils.AddOnTemplateGVR.Group && config.Resource == utils.AddOnTemplateGVR.Resource {
			return true, config
		}
	}
	return false, addonapiv1alpha1.ConfigReference{}
}

// GetTemplateSpecHash returns the sha256 hash of the spec field of the addon template
func GetTemplateSpecHash(template *addonapiv1alpha1.AddOnTemplate) (string, error) {
	unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		return "", err
	}
	specHash, err := utils.GetSpecHash(&unstructured.Unstructured{
		Object: unstructuredTemplate,
	})
	if err != nil {
		return specHash, err
	}
	return specHash, nil
}

// SupportAddOnTemplate return true if the given ClusterManagementAddOn supports the AddOnTemplate
func SupportAddOnTemplate(cma *addonapiv1alpha1.ClusterManagementAddOn) bool {
	if cma == nil {
		return false
	}

	for _, config := range cma.Spec.SupportedConfigs {
		if config.Group == utils.AddOnTemplateGVR.Group && config.Resource == utils.AddOnTemplateGVR.Resource {
			return true
		}
	}
	return false
}
