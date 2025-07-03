package cloudevents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workv1listers "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work"
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

	daemonSetJson = `{
		"apiVersion": "apps/v1",
		"kind": "DaemonSet",
		"metadata": {
			"name": "nginx-ds",
			"namespace": "default"
		},
		"spec": {
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
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
	var err error
	var managedClusterName string
	var cma *addonapiv1alpha1.ClusterManagementAddOn
	// var agentWorkClient workclientset.Interface
	var agentWorkClient workv1client.ManifestWorkInterface
	var agentWorkInformer workv1informers.ManifestWorkInformer
	var agentWorkLister workv1listers.ManifestWorkNamespaceLister
	var clientHolder *work.ClientHolder

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
		clientHolder, agentWorkInformer, err = startWorkAgent(ctx, managedClusterName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		agentWorkLister = agentWorkInformer.Lister().ManifestWorks(managedClusterName)
		agentWorkClient = clientHolder.ManifestWorks(managedClusterName)
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// cancel the context to stop the work agent
		cancel()
	})

	ginkgo.It("Should deploy agent when cma is managed by self successfully", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
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

		// Check the work is created
		var work *workv1.ManifestWork
		gomega.Eventually(func() error {
			works, err := agentWorkLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list works: %v", err)
			}

			if len(works) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			work = works[0]
			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		newWork := work.DeepCopy()
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied", ObservedGeneration: work.Generation})
		workBytes, err := json.Marshal(work)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newWorkBytes, err := json.Marshal(newWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		patchBytes, err := jsonpatch.CreateMergePatch(workBytes, newWorkBytes)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = agentWorkClient.Patch(context.Background(), work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// wait for addon to be applied
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied) {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			if cond := meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing); cond != nil {
				return fmt.Errorf("expected no addon progressing condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work to available so addon becomes available
		work, err = agentWorkClient.Get(context.Background(), work.Name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		newWork = work.DeepCopy()
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable", ObservedGeneration: work.Generation})

		workBytes, err = json.Marshal(work)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newWorkBytes, err = json.Marshal(newWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		patchBytes, err = jsonpatch.CreateMergePatch(workBytes, newWorkBytes)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = agentWorkClient.Patch(context.Background(), work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// wait for addon to be available
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
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
			_, err = agentWorkClient.Get(context.Background(), work.Name, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent and get available with prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testAddonImpl.prober = utils.NewDeploymentProber(types.NamespacedName{Name: "nginx-deployment", Namespace: "default"})

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		// Check the work is created
		var work *workv1.ManifestWork
		gomega.Eventually(func() error {
			works, err := agentWorkLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list works: %v", err)
			}

			if len(works) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			work = works[0]
			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if len(work.Spec.ManifestConfigs) != 1 {
				return fmt.Errorf("Unexpected number of work manifests configuration")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		newWork := work.DeepCopy()
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)
		newWork.Status.ResourceStatus = workv1.ManifestResourceStatus{
			Manifests: []workv1.ManifestCondition{
				{
					ResourceMeta: workv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "ReadyReplicas",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}

		workBytes, err := json.Marshal(work)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newWorkBytes, err := json.Marshal(newWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		patchBytes, err := jsonpatch.CreateMergePatch(workBytes, newWorkBytes)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = agentWorkClient.Patch(context.Background(), work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// wait for addon to be available
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
	})

	ginkgo.It("Should deploy agent and get available with deployment availability prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		objZeroReplicaDeployment := &unstructured.Unstructured{}
		err = objZeroReplicaDeployment.UnmarshalJSON([]byte(deploymentZeroReplicasJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj, objZeroReplicaDeployment}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeDeploymentAvailability,
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

		var work *workv1.ManifestWork
		gomega.Eventually(func() error {
			works, err := agentWorkLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list works: %v", err)
			}

			if len(works) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			work = works[0]
			if len(work.Spec.Workload.Manifests) != 2 {
				return fmt.Errorf("Unexpected number of work manifests: %d", len(work.Spec.Workload.Manifests))
			}

			if len(work.Spec.ManifestConfigs) != 2 {
				return fmt.Errorf("Unexpected number of work manifests configuration: %d", len(work.Spec.ManifestConfigs))
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[1].Raw, []byte(deploymentZeroReplicasJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[1].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		newWork := work.DeepCopy()
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)
		newWork.Status.ResourceStatus = workv1.ManifestResourceStatus{
			Manifests: []workv1.ManifestCondition{
				{
					ResourceMeta: workv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "ReadyReplicas",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}

		workBytes, err := json.Marshal(work)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newWorkBytes, err := json.Marshal(newWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		patchBytes, err := jsonpatch.CreateMergePatch(workBytes, newWorkBytes)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = agentWorkClient.Patch(context.Background(), work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// wait for addon to be available
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
	})

	ginkgo.It("Should deploy agent and get available with workload availability prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		objDaemonSet := &unstructured.Unstructured{}
		err = objDaemonSet.UnmarshalJSON([]byte(daemonSetJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj, objDaemonSet}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWorkloadAvailability,
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

		var work *workv1.ManifestWork
		gomega.Eventually(func() error {
			works, err := agentWorkLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list works: %v", err)
			}

			if len(works) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			work = works[0]
			if len(work.Spec.Workload.Manifests) != 2 {
				return fmt.Errorf("Unexpected number of work manifests: %d", len(work.Spec.Workload.Manifests))
			}

			if len(work.Spec.ManifestConfigs) != 2 {
				return fmt.Errorf("Unexpected number of work manifests configuration: %d",
					len(work.Spec.ManifestConfigs))
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[1].Raw, []byte(daemonSetJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[1].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		newWork := work.DeepCopy()
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkApplied,
			Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		meta.SetStatusCondition(&newWork.Status.Conditions, metav1.Condition{Type: workv1.WorkAvailable,
			Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)
		newWork.Status.ResourceStatus = workv1.ManifestResourceStatus{
			Manifests: []workv1.ManifestCondition{
				{
					ResourceMeta: workv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "ReadyReplicas",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
				{
					ResourceMeta: workv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "daemonsets",
						Name:      "nginx-ds",
						Namespace: "default",
					},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{
							{
								Name: "NumberReady",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "DesiredNumberScheduled",
								Value: workv1.FieldValue{
									Type:    workv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
				},
			},
		}

		workBytes, err := json.Marshal(work)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newWorkBytes, err := json.Marshal(newWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		patchBytes, err := jsonpatch.CreateMergePatch(workBytes, newWorkBytes)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = agentWorkClient.Patch(context.Background(), work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// wait for addon to be available
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
