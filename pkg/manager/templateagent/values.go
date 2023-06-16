package templateagent

import (
	"fmt"
	"sort"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
)

// ToAddOnNodePlacementPrivateValues only transform the AddOnDeploymentConfig NodePlacement part into Values object
// with a specific key, this value would be used by the addon template controller
func ToAddOnNodePlacementPrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if config.Spec.NodePlacement == nil {
		return nil, nil
	}

	return addonfactory.Values{
		NodePlacementPrivateValueKey: config.Spec.NodePlacement,
	}, nil
}

// ToAddOnRegistriesPrivateValues only transform the AddOnDeploymentConfig Registries part into Values object
// with a specific key, this value would be used by the addon template controller
func ToAddOnRegistriesPrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if config.Spec.Registries == nil {
		return nil, nil
	}

	return addonfactory.Values{
		RegistriesPrivateValueKey: config.Spec.Registries,
	}, nil
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
	overrideValues = addonfactory.MergeValues(overrideValues, defaultValues)

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

			overrideValues = addonfactory.MergeValues(overrideValues, publicValues)
		}
	}
	builtinSortedKeys, builtinValues, err := a.getBuiltinValues(cluster, addon)
	if err != nil {
		return presetValues, overrideValues, privateValues, nil
	}
	overrideValues = addonfactory.MergeValues(overrideValues, builtinValues)

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
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]string, addonfactory.Values, error) {
	builtinValues := templateCRDBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = addonfactory.AddonDefaultInstallNamespace
	}
	builtinValues.AddonInstallNamespace = installNamespace

	value, err := addonfactory.JsonStructToValues(builtinValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) getDefaultValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate) ([]string, addonfactory.Values, error) {
	defaultValues := templateCRDDefaultValues{}

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if template.Spec.Registration != nil {
		defaultValues.HubKubeConfigPath = hubKubeconfigPath()
	}

	value, err := addonfactory.JsonStructToValues(defaultValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) sortValueKeys(value addonfactory.Values) []string {
	keys := make([]string, 0)
	for k := range value {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	return keys
}

func hubKubeconfigPath() string {
	return "/managed/hub-kubeconfig/kubeconfig"
}
