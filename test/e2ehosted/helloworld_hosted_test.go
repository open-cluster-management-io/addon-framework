package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
)

const (
	helloWorldHostedAddonName = "helloworldhosted"
	hostedE2EClusterSetName   = "hosted-addon-e2e"

	eventuallyTimeout  = 300 // seconds
	eventuallyInterval = 1   // seconds
)

// addonAgentNamespace is the namespace the hosted addon agent is installed into on the hosting
// cluster.
func addonAgentNamespace() string {
	return fmt.Sprintf("klusterlet-%s-agent-addon", hostingClusterName)
}

var _ = ginkgo.Describe("install/uninstall helloworld hosted addons in Hosted mode", func() {
	ginkgo.BeforeEach(func() {
		for _, clusterName := range []string{hostedManagedClusterName, hostingClusterName} {
			_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), clusterName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err = hubKubeClient.CoreV1().Namespaces().Get(
				context.Background(), clusterName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		deleteManagedClusterAddon()
		deleteConsumerHostingClusterClaim()
		cleanupClusterSetGuard()
		enableAutoDiscovery()
	})

	ginkgo.AfterEach(func() {
		deleteManagedClusterAddon()
		deleteConsumerHostingClusterClaim()
		cleanupClusterSetGuard()
	})

	ginkgo.It("discovers the host only after the ClusterSet guard and runs the addon", func() {
		setConsumerHostingClusterClaim(hostingClusterName)
		setClusterSetLabels(hostedE2EClusterSetName)
		createManagedClusterAddon("")

		waitForValidityReason(addonapiv1beta1.HostingClusterValidityReasonAutoDiscoveryPending,
			metav1.ConditionFalse)
		assertHostingWorkAbsent(hostingClusterName)

		createClusterSetGuard()
		waitForDiscoveredHostingCluster()
		provideManagedClusterKubeconfig()
		waitForAddonAvailable()

		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: hostedManagedClusterName,
			},
			Data: map[string]string{"key1": rand.String(6), "key2": rand.String(6)},
		}
		_, err := hubKubeClient.CoreV1().ConfigMaps(hostedManagedClusterName).Create(
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copiedConfig, err := hostedManagedKubeClient.CoreV1().ConfigMaps(addonAgentNamespace()).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !apiequality.Semantic.DeepEqual(copiedConfig.Data, configmap.Data) {
				return fmt.Errorf("copied configmap data is %v", copiedConfig.Data)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		deleteManagedClusterAddon()
		gomega.Eventually(func() bool {
			_, err := hostedManagedKubeClient.CoreV1().ConfigMaps(addonAgentNamespace()).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		waitForDeployWorksDeleted()

		err = hubKubeClient.CoreV1().ConfigMaps(hostedManagedClusterName).Delete(
			context.Background(), configmap.Name, metav1.DeleteOptions{})
		gomega.Expect(err == nil || errors.IsNotFound(err)).To(gomega.BeTrue())
	})

	ginkgo.It("holds back a new deployment when the declared host disagrees with the target", func() {
		setConsumerHostingClusterClaim(hostingClusterName)
		createManagedClusterAddon(hostedManagedClusterName)

		waitForValidityReason(addonapiv1beta1.HostingClusterValidityReasonMismatch,
			metav1.ConditionFalse)
		gomega.Consistently(func() bool {
			_, err := hubWorkClient.WorkV1().ManifestWorks(hostedManagedClusterName).Get(
				context.Background(), hostingDeployWorkName(), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, 10, eventuallyInterval).Should(gomega.BeTrue())
		assertHostingWorkAbsent(hostingClusterName)

		deleteManagedClusterAddon()
		assertHostingWorkAbsent(hostedManagedClusterName)
	})

	ginkgo.It("keeps an existing deployment in place when the reported host changes", func() {
		setConsumerHostingClusterClaim(hostingClusterName)
		setClusterSetLabels(hostedE2EClusterSetName)
		createClusterSetGuard()
		createManagedClusterAddon("")
		waitForDiscoveredHostingCluster()
		provideManagedClusterKubeconfig()
		waitForAddonAvailable()

		work, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(
			context.Background(), hostingDeployWorkName(), metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		originalWorkUID := work.UID

		setConsumerHostingClusterClaim(hostedManagedClusterName)
		waitForValidityReason(addonapiv1beta1.HostingClusterValidityReasonMismatch,
			metav1.ConditionFalse)
		gomega.Eventually(func() error {
			addon, err := getManagedClusterAddon()
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon is not available: %v", addon.Status.Conditions)
			}
			work, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(
				context.Background(), hostingDeployWorkName(), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if work.UID != originalWorkUID {
				return fmt.Errorf("hosting work was relocated or recreated: got UID %s, want %s",
					work.UID, originalWorkUID)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		assertHostingWorkAbsent(hostedManagedClusterName)

		deleteManagedClusterAddon()
		waitForDeployWorksDeleted()
	})
})

func enableAutoDiscovery() {
	gomega.Eventually(func() error {
		cma, err := hubAddOnClient.AddonV1beta1().ClusterManagementAddOns().Get(
			context.Background(), helloWorldHostedAddonName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		cma.Spec.HostedModeAutoDiscovery = &addonapiv1beta1.HostedModeAutoDiscoveryConfig{
			Mode: addonapiv1beta1.HostedModeAutoDiscoveryModeEnable,
		}
		_, err = hubAddOnClient.AddonV1beta1().ClusterManagementAddOns().Update(
			context.Background(), cma, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func createManagedClusterAddon(hostingCluster string) {
	annotations := map[string]string{
		addonapiv1beta1.InstallModeAnnotationKey:   constants.InstallModeHosted,
		addonapiv1beta1.InstallNamespaceAnnotation: addonAgentNamespace(),
	}
	if hostingCluster != "" {
		annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] = hostingCluster
	}
	_, err := hubAddOnClient.AddonV1beta1().ManagedClusterAddOns(hostedManagedClusterName).Create(
		context.Background(), &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   hostedManagedClusterName,
				Name:        helloWorldHostedAddonName,
				Annotations: annotations,
			},
		}, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func getManagedClusterAddon() (*addonapiv1beta1.ManagedClusterAddOn, error) {
	return hubAddOnClient.AddonV1beta1().ManagedClusterAddOns(hostedManagedClusterName).Get(
		context.Background(), helloWorldHostedAddonName, metav1.GetOptions{})
}

func deleteManagedClusterAddon() {
	err := hubAddOnClient.AddonV1beta1().ManagedClusterAddOns(hostedManagedClusterName).Delete(
		context.Background(), helloWorldHostedAddonName, metav1.DeleteOptions{})
	gomega.Expect(err == nil || errors.IsNotFound(err)).To(gomega.BeTrue())
	if errors.IsNotFound(err) {
		return
	}
	gomega.Eventually(func() bool {
		_, err := getManagedClusterAddon()
		return errors.IsNotFound(err)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// setConsumerHostingClusterClaim seeds the producer output for this framework-owned consumer
// suite. The Klusterlet fixture intentionally does not enable reportHostingCluster, so no producer
// races this test for ownership of the reserved claim.
func setConsumerHostingClusterClaim(value string) {
	gomega.Eventually(func() error {
		claims := hostedManagedClusterClient.ClusterV1alpha1().ClusterClaims()
		claim, err := claims.Get(context.Background(), constants.HostingClusterClaimName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			_, err = claims.Create(context.Background(), &clusterv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{Name: constants.HostingClusterClaimName},
				Spec:       clusterv1alpha1.ClusterClaimSpec{Value: value},
			}, metav1.CreateOptions{})
			return err
		}
		if err != nil {
			return err
		}
		claim.Spec.Value = value
		_, err = claims.Update(context.Background(), claim, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	waitForReportedHostingCluster(value)
}

func deleteConsumerHostingClusterClaim() {
	err := hostedManagedClusterClient.ClusterV1alpha1().ClusterClaims().Delete(
		context.Background(), constants.HostingClusterClaimName, metav1.DeleteOptions{})
	gomega.Expect(err == nil || errors.IsNotFound(err)).To(gomega.BeTrue())
	if err == nil {
		waitForReportedHostingCluster("")
	}
}

func waitForReportedHostingCluster(expected string) {
	gomega.Eventually(func() string {
		cluster, err := hubClusterClient.ClusterV1().ManagedClusters().Get(
			context.Background(), hostedManagedClusterName, metav1.GetOptions{})
		if err != nil {
			return ""
		}
		for _, claim := range cluster.Status.ClusterClaims {
			if claim.Name == constants.HostingClusterClaimName {
				return claim.Value
			}
		}
		return ""
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Equal(expected))
}

func setClusterSetLabels(value string) {
	for _, clusterName := range []string{hostedManagedClusterName, hostingClusterName} {
		clusterName := clusterName
		gomega.Eventually(func() error {
			cluster, err := hubClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), clusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if cluster.Labels == nil {
				cluster.Labels = map[string]string{}
			}
			if value == "" {
				delete(cluster.Labels, clusterv1beta2.ClusterSetLabel)
			} else {
				cluster.Labels[clusterv1beta2.ClusterSetLabel] = value
			}
			_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(
				context.Background(), cluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	}
}

func createClusterSetGuard() {
	_, err := hubClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(),
		&clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{Name: hostedE2EClusterSetName},
			Spec: clusterv1beta2.ManagedClusterSetSpec{ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
			}},
		}, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func cleanupClusterSetGuard() {
	setClusterSetLabels("")
	err := hubClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(
		context.Background(), hostedE2EClusterSetName, metav1.DeleteOptions{})
	gomega.Expect(err == nil || errors.IsNotFound(err)).To(gomega.BeTrue())
	if err == nil {
		gomega.Eventually(func() bool {
			_, err := hubClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
				context.Background(), hostedE2EClusterSetName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}
}

func waitForValidityReason(reason string, status metav1.ConditionStatus) {
	gomega.Eventually(func() string {
		addon, err := getManagedClusterAddon()
		if err != nil {
			return err.Error()
		}
		condition := meta.FindStatusCondition(addon.Status.Conditions,
			addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity)
		if condition == nil || condition.Status != status {
			return ""
		}
		return condition.Reason
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Equal(reason))
}

func waitForDiscoveredHostingCluster() {
	gomega.Eventually(func() error {
		addon, err := getManagedClusterAddon()
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(addon.Status.Conditions,
			addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity) {
			return fmt.Errorf("hosting cluster is not valid: %v", addon.Status.Conditions)
		}
		if addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] != hostingClusterName ||
			addon.Annotations[addonapiv1beta1.HostingClusterNameManagedByAnnotationKey] !=
				addonapiv1beta1.HostingClusterNameManagedByAutoDiscoveryValue {
			return fmt.Errorf("hosting cluster was not auto-discovered: %v", addon.Annotations)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func provideManagedClusterKubeconfig() {
	gomega.Eventually(func() error {
		klusterletSecret, err := hostingKubeClient.CoreV1().Secrets(hostedKlusterletName).Get(
			context.Background(), "external-managed-kubeconfig", metav1.GetOptions{})
		if err != nil {
			return err
		}
		namespace := addonAgentNamespace()
		secrets := hostingKubeClient.CoreV1().Secrets(namespace)
		secretName := fmt.Sprintf("%s-managed-kubeconfig", helloWorldHostedAddonName)
		secret, err := secrets.Get(context.Background(), secretName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			_, err = secrets.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: secretName},
				Data:       klusterletSecret.Data,
			}, metav1.CreateOptions{})
			return err
		}
		if err != nil {
			return err
		}
		secret.Data = klusterletSecret.Data
		_, err = secrets.Update(context.Background(), secret, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func waitForAddonAvailable() {
	gomega.Eventually(func() error {
		addon, err := getManagedClusterAddon()
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(addon.Status.Conditions,
			addonapiv1beta1.ManagedClusterAddOnHostingManifestApplied) {
			return fmt.Errorf("hosting manifest is not applied: %v", addon.Status.Conditions)
		}
		if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
			return fmt.Errorf("addon is not available: %v", addon.Status.Conditions)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func hostingDeployWorkName() string {
	return fmt.Sprintf("%s-0",
		constants.DeployHostingWorkNamePrefix(hostedManagedClusterName, helloWorldHostedAddonName))
}

func assertHostingWorkAbsent(namespace string) {
	_, err := hubWorkClient.WorkV1().ManifestWorks(namespace).Get(
		context.Background(), hostingDeployWorkName(), metav1.GetOptions{})
	gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
}

func waitForDeployWorksDeleted() {
	works := []types.NamespacedName{
		{Namespace: hostedManagedClusterName,
			Name: fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(helloWorldHostedAddonName))},
		{Namespace: hostingClusterName, Name: hostingDeployWorkName()},
	}
	for _, work := range works {
		work := work
		gomega.Eventually(func() bool {
			_, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(
				context.Background(), work.Name, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}
}
