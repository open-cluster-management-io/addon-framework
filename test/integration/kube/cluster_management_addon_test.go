package kube

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
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

		testAddonImpl.registrations[managedClusterName] = []addonapiv1alpha1.RegistrationConfig{
			{
				SignerName: certificatesv1.KubeAPIServerClientSignerName,
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
		cma := &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1alpha1.InstallStrategy{
					Type: addonapiv1alpha1.AddonInstallStrategyManual,
				},
			},
		}
		cma, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// Create managed cluster addon
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(actual.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied) {
				return fmt.Errorf("expected RegistrationApplied condition to be true")
			}
			if actual.Status.Namespace != "test" {
				return fmt.Errorf("expected namespace in status is not correct, actual: %s", actual.Status.Namespace)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(), testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			_, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().
				Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return true
			}

			return false
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue(), "ClusterManagementAddOns should be deleted")
	})
	ginkgo.It("Should wait until managedclusteraddon cleaned successfully", func() {
		const cmaFinalizer = "cma.open-cluster-management.io/cma-pre-delete"
		// Create clustermanagement addon
		cma := &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1alpha1.InstallStrategy{
					Type: addonapiv1alpha1.AddonInstallStrategyManual,
				},
			},
		}
		cma, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// Create managed cluster addon
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		cma, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(controllerutil.ContainsFinalizer(cma, addonapiv1alpha1.AddonPreDeleteHookFinalizer)).
			Should(gomega.BeTrue(), "The ClusterManagementAddOns should have the AddonPreDeleteHook Finalizer")

		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().
			Delete(context.Background(), testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "Deleting ClusterManagementAddOns should be successful")

		gomega.Eventually(func() bool {
			_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return true
			}

			return false
		}, eventuallyTimeout*2, eventuallyInterval).Should(gomega.BeTrue(), "ManagedClusterAddOns should be deleted")

		gomega.Eventually(func() bool {
			_, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().
				Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return true
			}

			return false
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue(), "ClusterManagementAddOns should be deleted")
	})
})
