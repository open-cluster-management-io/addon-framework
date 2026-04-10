package kube

import (
	"context"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

// The addon owner controller exist in general addon manager.
// This is for integration testing to assume that addon manager has already added the OwnerReferences.
func createManagedClusterAddOnwithOwnerRefs(namespace string, addon *addonapiv1beta1.ManagedClusterAddOn, cma *addonapiv1beta1.ClusterManagementAddOn) {
	addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(namespace).Create(context.Background(), addon, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	addonCopy := addon.DeepCopy()

	// This is to assume that addon-manager has already added the OwnerReferences.
	owner := metav1.NewControllerRef(cma, schema.GroupVersionKind{
		Group:   addonapiv1beta1.GroupName,
		Version: addonapiv1beta1.GroupVersion.Version,
		Kind:    "ClusterManagementAddOn",
	})
	modified := utils.MergeOwnerRefs(&addonCopy.OwnerReferences, *owner, false)
	if modified {
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(addonCopy.Namespace).Update(context.Background(), addonCopy, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}
}

func setKubeClientDriver(namespace, addonName, driver string) {
	gomega.Eventually(func() error {
		addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(namespace).Get(context.Background(), addonName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Wait for registration controller to set registrations first
		if len(addon.Status.Registrations) == 0 {
			addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: driver,
					},
				},
			}
		} else {
			for i := range addon.Status.Registrations {
				if addon.Status.Registrations[i].Type == addonapiv1beta1.KubeClient {
					if addon.Status.Registrations[i].KubeClient == nil {
						addon.Status.Registrations[i].KubeClient = &addonapiv1beta1.KubeClientConfig{}
					}
					addon.Status.Registrations[i].KubeClient.Driver = driver
					break
				}
			}
		}

		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(namespace).UpdateStatus(context.Background(), addon, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}
