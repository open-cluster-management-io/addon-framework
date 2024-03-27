package integration

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	hookJobJson = `
{
    "apiVersion": "batch/v1",
    "kind": "Job",
    "metadata": {
        "name": "test",
        "namespace": "default",
		"annotations": {
			"addon.open-cluster-management.io/addon-pre-delete":""
		}
    },
    "spec": {
        "manualSelector": true,
        "selector": {
            "matchLabels": {
                "job": "test"
            }
        },
        "template": {
            "metadata": {
                "labels": {
                    "job": "test"
                },
                "name": "test"
            },
            "spec": {
                "restartPolicy": "Never",
                "containers": [
                    {
                        "args": [
                            "/helloworld_helm",
                            "cleanup",
                            "--addon-namespace=default"
                        ],
                        "image": "quay.io/open-cluster-management/addon-examples:latest",
                        "imagePullPolicy": "Always",
                        "name": "helloworld-cleanup-agent"
                    }
                ]
            }
        }
    }
}
`
)

var _ = ginkgo.Describe("Agent hook deploy", func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var managedClusterName string
	var cma *addonapiv1alpha1.ClusterManagementAddOn
	var err error
	ginkgo.BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
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

		cma = newClusterManagementAddon(testAddonImpl.name)
		cma, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(),
			cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// start work agent for the managed cluster
		ginkgo.By("Start agent for managed cluster")
		startAgent(ctx, managedClusterName)
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cancel()
	})

	ginkgo.It("Should install and uninstall agent successfully", func() {
		deployObj := &unstructured.Unstructured{}
		err := deployObj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		hookObj := &unstructured.Unstructured{}
		err = hookObj.UnmarshalJSON([]byte(hookJobJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{deployObj, hookObj}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
			// Type: agent.HealthProberTypeDeploymentAvailability,
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		// check deploy manifest is deployed
		gomega.Eventually(func() error {
			deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx-deployment", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get deployment: %v", err)
			}

			if apiequality.Semantic.DeepEqual(deploy, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", deploy)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// addon has a finalizer
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			finalizers := addon.GetFinalizers()
			for _, f := range finalizers {
				if f == addonapiv1alpha1.AddonPreDeleteHookFinalizer {
					return nil
				}
			}
			return fmt.Errorf("these is no hook finalizer in addon")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// addon condition is applied and available
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied) {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionAvailable) {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			if cond := meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing); cond != nil {
				return fmt.Errorf("expected no addon progressing condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// delete addon, hook manifestwork will be applied, addon is deleting and addon status will be updated.
		err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			job, err := spokeKubeClient.BatchV1().Jobs("default").Get(context.Background(), "test", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get pre-delete job: %v", err)
			}

			if apiequality.Semantic.DeepEqual(job, []byte(hookJobJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", job)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// check the addon status for hook manifestwork
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "HookManifestCompleted") {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// addon will be deleted after hook manifestwork is completed
		// gomega.Eventually(func() error {
		// 	_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
		// 	if err != nil {
		// 		if errors.IsNotFound(err) {
		// 			return nil
		// 		}
		// 		return err
		// 	}
		// 	return fmt.Errorf("addon is expceted to be deleted")
		// }, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
