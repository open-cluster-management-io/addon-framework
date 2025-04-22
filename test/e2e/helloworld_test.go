package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	eventuallyTimeout  = 300 // seconds
	eventuallyInterval = 1   // seconds
)

const (
	addonName                             = "helloworld"
	deployConfigName                      = "deploy-config"
	deployImageOverrideConfigName         = "image-override-deploy-config"
	deployProxyConfigName                 = "proxy-deploy-config"
	deployResourceRequirementConfigName   = "resources-deploy-config"
	deployAgentInstallNamespaceConfigName = "agent-install-namespace-deploy-config"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
	registries   = []addonapiv1alpha1.ImageMirror{
		{
			Source: "quay.io/open-cluster-management/addon-examples",
			Mirror: "quay.io/ocm/addon-examples",
		},
	}

	proxyConfig = addonapiv1alpha1.ProxyConfig{
		// settings of http proxy will not impact the communicaiton between
		// hub kube-apiserver and the add-on agent running on managed cluster.
		HTTPProxy: "http://127.0.0.1:3128",
		NoProxy:   "example.com",
	}

	agentInstallNamespaceConfig = "test-ns"
)

var _ = ginkgo.Describe("install/uninstall helloworld addons", func() {
	ginkgo.BeforeEach(func() {
		gomega.Eventually(func() error {
			_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubKubeClient.CoreV1().Namespaces().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			testNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: agentInstallNamespaceConfig,
				},
			}
			_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), testNs, metav1.CreateOptions{})
			if err != nil {
				if errors.IsAlreadyExists(err) {
					return nil
				}
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Clean up the customized agent install namespace after each case.")
		gomega.Eventually(func() error {
			_, err := hubKubeClient.CoreV1().Namespaces().Get(context.Background(),
				agentInstallNamespaceConfig, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}

			if err == nil {
				errd := hubKubeClient.CoreV1().Namespaces().Delete(context.Background(),
					agentInstallNamespaceConfig, metav1.DeleteOptions{})
				if errd != nil {
					return errd
				}
				return fmt.Errorf("ns is deleting, need re-check if namespace is not found")
			}

			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Clean up CSRs for addon: %s", addonName))
		_ = cleanupCSR(hubKubeClient, addonName)
	})

	ginkgo.It("addon should be worked", func() {
		ginkgo.By("Make sure addon is available")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is functioning")
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

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Prepare a AddOnDeploymentConfig for addon nodeSelector and tolerations")
		gomega.Eventually(func() error {
			return prepareAddOnDeploymentConfig(managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: managedClusterName,
						Name:      deployConfigName,
					},
				},
			}
			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(context.Background(), "helloworld-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.NodeSelector, nodeSelector) {
				return fmt.Errorf("unexpected nodeSeletcor %v", agentDeploy.Spec.Template.Spec.NodeSelector)
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.Tolerations, tolerations) {
				return fmt.Errorf("unexpected tolerations %v", agentDeploy.Spec.Template.Spec.Tolerations)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Remove addon")
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("addon should be worked with proxy settings", func() {
		ginkgo.By("Make sure addon is available")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is functioning")
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

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Prepare a AddOnDeploymentConfig for proxy settings")
		gomega.Eventually(func() error {
			return prepareProxyConfigAddOnDeploymentConfig(managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: managedClusterName,
						Name:      deployProxyConfigName,
					},
				},
			}
			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(context.Background(), "helloworld-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			containers := agentDeploy.Spec.Template.Spec.Containers
			if len(containers) != 1 {
				return fmt.Errorf("expect one container, but %v", containers)
			}

			// check the proxy settings
			deployProxyConfig := addonapiv1alpha1.ProxyConfig{}
			for _, envVar := range containers[0].Env {
				if envVar.Name == "HTTP_PROXY" {
					deployProxyConfig.HTTPProxy = envVar.Value
				}

				if envVar.Name == "HTTPS_PROXY" {
					deployProxyConfig.HTTPSProxy = envVar.Value
				}

				if envVar.Name == "NO_PROXY" {
					deployProxyConfig.NoProxy = envVar.Value
				}
			}

			if !equality.Semantic.DeepEqual(proxyConfig, deployProxyConfig) {
				return fmt.Errorf("expected proxy settings %v, but got %v", proxyConfig, deployProxyConfig)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Remove addon")
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("addon should be worked with resource requirements settings", func() {
		ginkgo.By("Make sure addon is available")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is functioning")
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

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Prepare a AddOnDeploymentConfig for resource requirements settings")
		gomega.Eventually(func() error {
			return prepareResourceRequirementsAddOnDeploymentConfig(managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: managedClusterName,
						Name:      deployResourceRequirementConfigName,
					},
				},
			}
			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(context.Background(), "helloworld-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			containers := agentDeploy.Spec.Template.Spec.Containers
			if len(containers) != 1 {
				return fmt.Errorf("expect one container, but %v", containers)
			}

			// check the resource requirements
			required := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
			}

			if !equality.Semantic.DeepEqual(containers[0].Resources, required) {
				return fmt.Errorf("expected resource requirement %v, but got %v", required, containers[0].Resources)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Remove addon")
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("addon registraion agent install namespace should work", func() {
		ginkgo.By("Make sure addon is available")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is functioning")
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

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Prepare a AddOnDeploymentConfig for addon agent install namespace")
		gomega.Eventually(func() error {
			return prepareAgentInstallNamespaceAddOnDeploymentConfig(managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: managedClusterName,
						Name:      deployAgentInstallNamespaceConfigName,
					},
				},
			}
			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			_, err := hubKubeClient.CoreV1().Secrets(agentInstallNamespaceConfig).Get(
				context.Background(), "helloworld-hub-kubeconfig", metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Remove addon")
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})

func prepareAddOnDeploymentConfig(namespace string) error {
	_, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(context.Background(), deployConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					NodePlacement: &addonapiv1alpha1.NodePlacement{
						NodeSelector: nodeSelector,
						Tolerations:  tolerations,
					},
					AgentInstallNamespace: addonInstallNamespace,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareImageOverrideAddOnDeploymentConfig(namespace string) error {
	_, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), deployImageOverrideConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployImageOverrideConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries:            registries,
					AgentInstallNamespace: addonInstallNamespace,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareProxyConfigAddOnDeploymentConfig(namespace string) error {
	_, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(context.Background(), deployProxyConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployProxyConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					ProxyConfig:           proxyConfig,
					AgentInstallNamespace: addonInstallNamespace,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareResourceRequirementsAddOnDeploymentConfig(namespace string) error {
	_, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(context.Background(), deployResourceRequirementConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployResourceRequirementConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: addonInstallNamespace,
					ResourceRequirements: []addonapiv1alpha1.ContainerResourceRequirements{
						{
							ContainerID: "*:*:*",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
						},
						{
							ContainerID: "deployments:*:*",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
							},
						},
					},
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareAgentInstallNamespaceAddOnDeploymentConfig(namespace string) error {
	_, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), deployAgentInstallNamespaceConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hubAddOnClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployAgentInstallNamespaceConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: agentInstallNamespaceConfig,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func cleanupCSR(hubKubeClient kubernetes.Interface, addonName string) error {
	csrs, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().List(
		context.Background(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", addonapiv1alpha1.AddonLabelKey, addonName),
		})
	if err != nil {
		return err
	}

	for _, csr := range csrs.Items {

		if csr.Status.Certificate == nil {
			continue
		}
		err := hubKubeClient.CertificatesV1().CertificateSigningRequests().Delete(
			context.Background(), csr.Name, *&metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}
