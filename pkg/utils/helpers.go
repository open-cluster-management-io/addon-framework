package utils

import addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

func MergeRelatedObjects(modified *bool, objs *[]addonapiv1alpha1.ObjectReference, obj addonapiv1alpha1.ObjectReference) {
	if *objs == nil {
		*objs = []addonapiv1alpha1.ObjectReference{}
	}

	for _, o := range *objs {
		if o.Group == obj.Group && o.Resource == obj.Resource && o.Name == obj.Name && o.Namespace == obj.Namespace {
			return
		}
	}

	*objs = append(*objs, obj)
	*modified = true
}

// GetAddonInstallMode returns addon installation mode, mode could be `Default` or `Hosted`
// TODO: Consider changing the ManagedClusterAddon API to identify the hosted mode installation
func GetAddonInstallMode(addOn *addonapiv1alpha1.ManagedClusterAddOn) string {
	if mode, ok := addOn.Annotations["addon.open-cluster-management.io/agent-deploy-mode"]; ok {
		if mode == "Default" || mode == "Hosted" {
			return mode
		}
	}
	return "Default"
}

// GetHostingCluster returns addon hosting cluster name, it is only used in Hosed mode.
// TODO: Consider changing the ManagedClusterAddon API to identify the hosting cluster name
func GetHostingCluster(addOn *addonapiv1alpha1.ManagedClusterAddOn) string {
	return addOn.Annotations["addon.open-cluster-management.io/hosting-cluster-name"]
}
