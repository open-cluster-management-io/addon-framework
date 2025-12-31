package constants

import (
	"fmt"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// InstallModeBuiltinValueKey is the key of the build in value to represent the addon install mode, addon developers
	// can use this built in value in manifests.
	InstallModeBuiltinValueKey = "InstallMode"
	InstallModeHosted          = "Hosted"
	InstallModeDefault         = "Default"

	// AddonLifecycleAnnotationKey is an annotation key on ClusterManagementAddon to indicate the installation
	// and upgrade of addon should be handled by the general addon manager or addon itself.
	// This constant was in v1alpha1 API but removed in v1beta1, keeping it here for compatibility.
	AddonLifecycleAnnotationKey = "addon.open-cluster-management.io/lifecycle"

	// AddonLifecycleAddonManagerAnnotationValue indicates that the addon installation and upgrade
	// is handled by the general addon manager.
	AddonLifecycleAddonManagerAnnotationValue = "addon-manager"

	// AddonLifecycleSelfManageAnnotationValue indicates that the addon installation and upgrade
	// is handled by the addon itself.
	AddonLifecycleSelfManageAnnotationValue = "self"
)

// DeployWorkNamePrefix returns the prefix of the work name for the addon
func DeployWorkNamePrefix(addonName string) string {
	return fmt.Sprintf("addon-%s-deploy", addonName)
}

// DeployHostingWorkNamePrefix returns the prefix of the work name on hosting cluster for the addon
func DeployHostingWorkNamePrefix(addonNamespace, addonName string) string {
	return fmt.Sprintf("%s-hosting-%s", DeployWorkNamePrefix(addonName), addonNamespace)
}

// PreDeleteHookWorkName return the name of pre-delete work for the addon
func PreDeleteHookWorkName(addonName string) string {
	return fmt.Sprintf("addon-%s-pre-delete", addonName)
}

// PreDeleteHookHostingWorkName return the name of pre-delete work on hosting cluster for the addon
func PreDeleteHookHostingWorkName(addonNamespace, addonName string) string {
	return fmt.Sprintf("%s-hosting-%s", PreDeleteHookWorkName(addonName), addonNamespace)
}

// GetHostedModeInfo returns addon installation mode and hosting cluster name.
func GetHostedModeInfo(addon *addonv1beta1.ManagedClusterAddOn, _ *clusterv1.ManagedCluster) (string, string) {
	if len(addon.Annotations) == 0 {
		return InstallModeDefault, ""
	}
	hostingClusterName, ok := addon.Annotations[addonv1beta1.HostingClusterNameAnnotationKey]
	if !ok {
		return InstallModeDefault, ""
	}

	return InstallModeHosted, hostingClusterName
}

// GetHostedManifestLocation returns the location of the manifest in Hosted mode, if it is invalid will return error
func GetHostedManifestLocation(labels, annotations map[string]string) (string, bool, error) {
	manifestLocation := annotations[addonv1beta1.HostedManifestLocationAnnotationKey]

	// In v1beta1, HostedManifestLocationLabelKey was removed in favor of annotation only
	// For backward compatibility, check labels if annotation is not set
	if manifestLocation == "" {
		// Use the hardcoded label key for backward compatibility
		manifestLocation = labels["addon.open-cluster-management.io/hosted-manifest-location"]
	}

	switch manifestLocation {
	case addonv1beta1.HostedManifestLocationManagedValue,
		addonv1beta1.HostedManifestLocationHostingValue,
		addonv1beta1.HostedManifestLocationNoneValue:
		return manifestLocation, true, nil
	case "":
		return "", false, nil
	default:
		return "", true, fmt.Errorf("not supported manifest location: %s", manifestLocation)
	}
}
