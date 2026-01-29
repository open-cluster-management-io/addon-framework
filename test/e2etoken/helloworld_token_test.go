package e2etoken

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	helloworldAddonName   = "helloworld"
	addonInstallNamespace = "open-cluster-management-agent-addon"
)

var _ = ginkgo.Describe("Token-based addon registration", func() {
	ginkgo.BeforeEach(func() {
		gomega.Eventually(func() error {
			_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubKubeClient.CoreV1().Namespaces().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should handle token-based authentication", func() {
		ginkgo.By("Wait for addon registrations to be populated")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(addon.Status.Registrations) == 0 {
				return fmt.Errorf("registrations not yet populated")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Wait for kubeClientDriver to be set to token by agent")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if addon.Status.KubeClientDriver != "token" {
				return fmt.Errorf("kubeClientDriver not set to token yet, current: %s", addon.Status.KubeClientDriver)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Verify RegistrationApplied condition becomes true")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied) {
				cond := meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied)
				return fmt.Errorf("RegistrationApplied condition not true: %v", cond)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Verify RBAC role is created")
		gomega.Eventually(func() error {
			roleName := fmt.Sprintf("open-cluster-management:%s:agent", helloworldAddonName)
			_, err := hubKubeClient.RbacV1().Roles(managedClusterName).Get(
				context.Background(), roleName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Verify RBAC role binding is created")
		gomega.Eventually(func() error {
			roleName := fmt.Sprintf("open-cluster-management:%s:agent", helloworldAddonName)
			_, err := hubKubeClient.RbacV1().RoleBindings(managedClusterName).Get(
				context.Background(), roleName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Verify addon becomes available")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon not available yet")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should have correct registration subject set by agent", func() {
		ginkgo.By("Verify registration contains subject with correct format (User only, no Groups)")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			var found bool
			for _, reg := range addon.Status.Registrations {
				if reg.SignerName == certificatesv1.KubeAPIServerClientSignerName {
					if reg.Subject.User == "" {
						return fmt.Errorf("subject user is empty")
					}
					// Verify subject format - should be a service account for token auth
					if !strings.HasPrefix(reg.Subject.User, "system:serviceaccount:") {
						return fmt.Errorf("unexpected user format: %s, expected system:serviceaccount:*", reg.Subject.User)
					}
					// For token driver, groups should NOT be in registration subject
					// Groups are implicit in the ServiceAccount token itself
					if len(reg.Subject.Groups) != 0 {
						return fmt.Errorf("subject groups should be empty for token driver, got: %v", reg.Subject.Groups)
					}
					found = true
					break
				}
			}

			if !found {
				return fmt.Errorf("kubeclient registration not found")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should create RBAC with subjects matching registration status", func() {
		var regSubject *addonapiv1alpha1.Subject

		ginkgo.By("Get registration subject from addon status")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			for _, reg := range addon.Status.Registrations {
				if reg.SignerName == certificatesv1.KubeAPIServerClientSignerName {
					if reg.Subject.User == "" {
						return fmt.Errorf("registration subject user not populated yet")
					}
					// Verify it's a ServiceAccount for token auth
					if !strings.HasPrefix(reg.Subject.User, "system:serviceaccount:") {
						return fmt.Errorf("expected ServiceAccount user, got: %s", reg.Subject.User)
					}
					// For token driver, groups should be empty in registration
					if len(reg.Subject.Groups) != 0 {
						return fmt.Errorf("registration subject should not have groups for token driver, got: %v", reg.Subject.Groups)
					}
					regSubject = &reg.Subject
					return nil
				}
			}
			return fmt.Errorf("kubeclient registration not found")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Verify RoleBinding contains ONLY User from registration (no Groups)")
		gomega.Eventually(func() error {
			roleName := fmt.Sprintf("open-cluster-management:%s:agent", helloworldAddonName)
			binding, err := hubKubeClient.RbacV1().RoleBindings(managedClusterName).Get(
				context.Background(), roleName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify User is in subjects
			hasUser := false
			for _, subject := range binding.Subjects {
				if subject.Kind == rbacv1.UserKind && subject.Name == regSubject.User {
					hasUser = true
					break
				}
			}

			if !hasUser {
				return fmt.Errorf("RoleBinding missing user subject %s, actual subjects: %v", regSubject.User, binding.Subjects)
			}

			// For token driver, RoleBinding should NOT have Groups
			// Groups are implicit in the ServiceAccount token when making API calls
			for _, subject := range binding.Subjects {
				if subject.Kind == rbacv1.GroupKind {
					return fmt.Errorf("RoleBinding should not have group subjects for token driver, found: %v", subject)
				}
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should function correctly with token authentication", func() {
		ginkgo.By("Verify ManifestApplied condition")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloworldAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				cond := meta.FindStatusCondition(addon.Status.Conditions, "ManifestApplied")
				return fmt.Errorf("ManifestApplied condition not true: %v", cond)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Test addon functionality - create a configmap on hub")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: managedClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Verify addon copies the configmap using token authentication")
		gomega.Eventually(func() error {
			copiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copiedConfig.Data, configmap.Data) {
				return fmt.Errorf("copied configmap data does not match, expected %v, got %v", configmap.Data, copiedConfig.Data)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Clean up test configmap")
		err = hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Delete(
			context.Background(), configmap.Name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})
