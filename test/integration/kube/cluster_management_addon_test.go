package kube

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = ginkgo.Describe("ClusterManagementAddon", func() {
	var managedClusterName string
	var err error

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)

		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		testAddonImpl.registrations[managedClusterName] = []addonapiv1beta1.RegistrationConfig{
			{
				Type:       addonapiv1beta1.KubeClient,
				KubeClient: &addonapiv1beta1.KubeClientConfig{},
			},
		}

	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		delete(testAddonImpl.registrations, managedClusterName)
	})

	ginkgo.It("Should update config related object successfully", func() {
		// Create clustermanagement addon
		cma := &addonapiv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1beta1.InstallStrategy{
					Type: addonapiv1beta1.AddonInstallStrategyManual,
				},
			},
		}
		cma, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// Create managed cluster addon
		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{
				Configs: []addonapiv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: managedClusterName,
							Name:      "test-deploy-config",
						},
					},
				},
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(actual.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied) {
				return fmt.Errorf("expected RegistrationApplied condition to be true")
			}
			if actual.Status.Namespace != "test" {
				return fmt.Errorf("expected namespace in status is not correct, actual: %s", actual.Status.Namespace)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(context.Background(), testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})
