package integration

import (
	"context"
	"fmt"
	"time"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	deploymentJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default"
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`

	deploymentZeroReplicasJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment-no-replica",
			"namespace": "default"
		},
		"spec": {
			"replicas": 0,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`
)

var _ = ginkgo.Describe("Agent deploy", func() {
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

	ginkgo.It("Should deploy agent when cma is managed by self successfully", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		objZeroReplicaDeployment := &unstructured.Unstructured{}
		err = objZeroReplicaDeployment.UnmarshalJSON([]byte(deploymentZeroReplicasJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj, objZeroReplicaDeployment}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
			// Type: agent.HealthProberTypeDeploymentAvailability,
		}

		// Create ManagedClusterAddOn
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		// wait for the agent to deploy the addon
		gomega.Eventually(func() error {
			deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx-deployment", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get deployment: %v", err)
			}

			if apiequality.Semantic.DeepEqual(deploy, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", deploy)
			}

			deployNoReplica, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx-deployment-no-replica", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get deployment: %v", err)
			}

			if apiequality.Semantic.DeepEqual(deployNoReplica, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", deploy)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// check the addon status
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

		// do nothing if cluster is deleting and addon is not deleted
		cluster, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		cluster.SetFinalizers([]string{"cluster.open-cluster-management.io/api-resource-cleanup"})
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(context.Background(), cluster, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		time.Sleep(5 * time.Second) // wait 5 seconds to sync
		gomega.Eventually(func() error {
			appliedWorkList, err := spokeWorkClient.WorkV1().AppliedManifestWorks().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("failed to list applied manifest works: %v", err)
			}

			if len(appliedWorkList.Items) != 1 {
				return fmt.Errorf("Unexpected number of applied manifest works")
			}

			deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx-deployment", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get deployment: %v", err)
			}

			if apiequality.Semantic.DeepEqual(deploy, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", deploy)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})

// The addon owner controller exist in general addon manager.
// This is for integration testing to assume that addon manager has already added the OwnerReferences.
func createManagedClusterAddOnwithOwnerRefs(namespace string, addon *addonapiv1alpha1.ManagedClusterAddOn, cma *addonapiv1alpha1.ClusterManagementAddOn) {
	addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Create(context.Background(), addon, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	addonCopy := addon.DeepCopy()

	// This is to assume that addon-manager has already added the OwnerReferences.
	owner := metav1.NewControllerRef(cma, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
	modified := utils.MergeOwnerRefs(&addonCopy.OwnerReferences, *owner, false)
	if modified {
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(addonCopy.Namespace).Update(context.Background(), addonCopy, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}
}
