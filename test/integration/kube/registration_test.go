package kube

import (
	"context"
	"fmt"
	"open-cluster-management.io/addon-framework/pkg/agent"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = ginkgo.Describe("Addon Registration", func() {
	var managedClusterName string
	var cma *addonapiv1beta1.ClusterManagementAddOn
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

		// Create clustermanagement addon
		cma = &addonapiv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1beta1.InstallStrategy{
					Type: addonapiv1beta1.AddonInstallStrategyManual,
				},
			},
		}
		cma, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		delete(testAddonImpl.registrations, managedClusterName)
		err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(context.Background(), testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should setup registration successfully", func() {
		testAddonImpl.registrations[managedClusterName] = []agent.RegistrationConfig{
			&agent.KubeClientRegistration{
				User: fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s:agent:%s",
					managedClusterName, testAddonImpl.name, testAddonImpl.name),
				Groups: []string{
					fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s",
						managedClusterName, testAddonImpl.name),
					fmt.Sprintf("system:open-cluster-management:addon:%s", testAddonImpl.name),
				},
			},
		}

		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
				Annotations: map[string]string{
					addonapiv1beta1.InstallNamespaceAnnotation: "test",
				},
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		// Set kubeClientDriver to "csr" for CSR-based authentication
		setKubeClientDriver(managedClusterName, testAddonImpl.name, "csr")

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			// Create expected with driver for comparison
			expected := make([]addonapiv1beta1.RegistrationConfig, len(testAddonImpl.registrations[managedClusterName]))
			for i, reg := range testAddonImpl.registrations[managedClusterName] {
				expected[i] = reg.RegistrationAPI()
				if expected[i].Type == addonapiv1beta1.KubeClient && expected[i].KubeClient != nil {
					expected[i].KubeClient.Driver = "csr"
				}
			}
			if !apiequality.Semantic.DeepEqual(expected, actual.Status.Registrations) {
				return fmt.Errorf("Expected registration is not correct\nExpected: %+v\nActual: %+v", expected, actual.Status.Registrations)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should update registration successfully", func() {
		testAddonImpl.registrations[managedClusterName] = []agent.RegistrationConfig{
			&agent.KubeClientRegistration{
				User: fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s:agent:%s",
					managedClusterName, testAddonImpl.name, testAddonImpl.name),
				Groups: []string{
					fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s",
						managedClusterName, testAddonImpl.name),
					fmt.Sprintf("system:open-cluster-management:addon:%s", testAddonImpl.name),
				},
			},
			&agent.CustomSignerRegistration{
				SignerName: "open-cluster-management.io/test-signer",
			},
		}

		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
				Annotations: map[string]string{
					addonapiv1beta1.InstallNamespaceAnnotation: "test",
				},
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		// Set kubeClientDriver to "csr" for CSR-based authentication
		setKubeClientDriver(managedClusterName, testAddonImpl.name, "csr")

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			actual.Status = addonapiv1beta1.ManagedClusterAddOnStatus{
				Registrations: []addonapiv1beta1.RegistrationConfig{
					{
						Type: addonapiv1beta1.KubeClient,
						KubeClient: &addonapiv1beta1.KubeClientConfig{
							Driver: "csr",
						},
					},
				},
				Conditions: []metav1.Condition{},
			}
			_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), actual, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			// Create expected with driver for comparison
			expected := make([]addonapiv1beta1.RegistrationConfig, len(testAddonImpl.registrations[managedClusterName]))
			for i, reg := range testAddonImpl.registrations[managedClusterName] {
				expected[i] = reg.RegistrationAPI()
				if expected[i].Type == addonapiv1beta1.KubeClient && expected[i].KubeClient != nil {
					expected[i].KubeClient.Driver = "csr"
				}
			}
			if !apiequality.Semantic.DeepEqual(expected, actual.Status.Registrations) {
				return fmt.Errorf("Expected registration is not correct\nExpected: %+v\nActual: %+v", expected, actual.Status.Registrations)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
