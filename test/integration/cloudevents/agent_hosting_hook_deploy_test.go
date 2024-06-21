package cloudevents

import (
	"context"
	"encoding/json"
	"fmt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workv1listers "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	hookHostingJobJson = `
{
    "apiVersion": "batch/v1",
    "kind": "Job",
    "metadata": {
        "name": "test",
        "namespace": "default",
		"annotations": {
            "addon.open-cluster-management.io/addon-pre-delete": "",
			"addon.open-cluster-management.io/hosted-manifest-location": "hosting"
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
                            "/helloworld_hosted",
                            "cleanup",
                            "--addon-namespace=default"
                        ],
                        "image": "quay.io/open-cluster-management/helloworld-addon:latest",
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
	var err error
	var managedClusterName, hostingClusterName string
	var hostingJobCompleteValue = "True"
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
		hostingClusterName = fmt.Sprintf("hostingcluster-%s", suffix)

		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		hostingCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: hostingClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(
			context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(
			context.Background(), hostingCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: hostingClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cma = newClusterManagementAddon(testHostedAddonImpl.name)
		cma, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(),
			cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// start work agent for the hosting cluster
		ginkgo.By("Start agent for hosting cluster")
		clientHolder, agentWorkInformer, err = startWorkAgent(ctx, hostingClusterName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		agentWorkLister = agentWorkInformer.Lister().ManifestWorks(hostingClusterName)
		agentWorkClient = clientHolder.ManifestWorks(hostingClusterName)
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(
			context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(
			context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = hubKubeClient.CoreV1().Namespaces().Delete(
			context.Background(), hostingClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(
			context.Background(), hostingClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			testHostedAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// cancel the context to stop the work agent
		cancel()
	})

	ginkgo.It("Should install and uninstall agent successfully", func() {
		deployObj := &unstructured.Unstructured{}
		err := deployObj.UnmarshalJSON([]byte(deploymentHostingJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		hookObj := &unstructured.Unstructured{}
		err = hookObj.UnmarshalJSON([]byte(hookHostingJobJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testHostedAddonImpl.manifests[managedClusterName] = []runtime.Object{deployObj, hookObj}
		testHostedAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testHostedAddonImpl.name,
				Annotations: map[string]string{
					addonapiv1alpha1.HostingClusterNameAnnotationKey: hostingClusterName,
				},
				// this finalizer is to prevent the addon from being deleted for test, it will be deleted at the end.
				Finalizers: []string{"pending"},
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		// deploy manifest is deployed
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

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentHostingJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// addon has a finalizer
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			finalizers := addon.GetFinalizers()
			for _, f := range finalizers {
				if f == addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer {
					return nil
				}
			}
			return fmt.Errorf("these is no hook finalizer in addon")
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

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied) {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
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

		gomega.Eventually(func() error {
			addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// HealthProberTypeWork for hosting manifestwork is not supported by now
			// TODO: consider to support it
			// if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
			// 	return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			// }
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// delete addon, hook manifestwork will be applied, addon is deleting and addon status will be updated.
		err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
			Delete(context.Background(), testHostedAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// hook manifest is deployed
		var hookWork *workv1.ManifestWork
		gomega.Eventually(func() error {
			works, err := agentWorkLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list works: %v", err)
			}

			if len(works) != 2 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			for _, w := range works {
				if w.Name != work.Name {
					hookWork = w
					break
				}
			}

			if len(hookWork.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of hookWork manifests")
			}

			if apiequality.Semantic.DeepEqual(hookWork.Spec.Workload.Manifests[0].Raw, []byte(hookHostingJobJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", hookWork.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "HookManifestCompleted") {
				return fmt.Errorf("unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update hook manifest feedbackResult, addon status will be updated and finalizer and pre-delete manifestwork
		// will be deleted.
		newHookWork := hookWork.DeepCopy()
		meta.SetStatusCondition(&newHookWork.Status.Conditions, metav1.Condition{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		meta.SetStatusCondition(&newHookWork.Status.Conditions, metav1.Condition{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "ResourceAvailable"})
		newHookWork.Status.ResourceStatus = workv1.ManifestResourceStatus{
			Manifests: []workv1.ManifestCondition{
				{
					ResourceMeta: workv1.ManifestResourceMeta{
						Group:     "batch",
						Version:   "v1",
						Resource:  "jobs",
						Namespace: "default",
						Name:      "test",
					},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{
							{
								Name: "JobComplete",
								Value: workv1.FieldValue{
									Type:   workv1.String,
									String: &hostingJobCompleteValue,
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

		hookWorkBytes, err := json.Marshal(hookWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newHookWorkBytes, err := json.Marshal(newHookWork)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		hookWorkPatchBytes, err := jsonpatch.CreateMergePatch(hookWorkBytes, newHookWorkBytes)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = agentWorkClient.Patch(context.Background(), hookWork.Name, types.MergePatchType, hookWorkPatchBytes, metav1.PatchOptions{}, "status")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// addon only has pending finalizer, and status is updated
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if len(addon.Finalizers) != 1 {
				return fmt.Errorf("addon is expected to only 1 finalizer,but got %v", len(addon.Finalizers))
			}
			if addon.Finalizers[0] != "pending" {
				return fmt.Errorf("addon is expected to only pending finalizer,but got %v", len(addon.Finalizers))
			}
			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted) {
				return fmt.Errorf("addon HookManifestCompleted condition is expecte to true, but got false")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update addon to trigger reconcile 3 times, and per-delete manifestwork should be deleted and not be re-created
		// for i := 0; i < 3; i++ {
		// 	gomega.Eventually(func() error {
		// 		addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
		// 			Get(context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
		// 		if err != nil {
		// 			return err
		// 		}
		// 		addon.Labels = map[string]string{"test": fmt.Sprintf("%d", i)}
		// 		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
		// 			Update(context.Background(), addon, metav1.UpdateOptions{})
		// 		return err
		// 	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// 	time.Sleep(2 * time.Second)
		// 	hookWork, err = agentWorkClient.Get(context.Background(), hookWork.Name, metav1.GetOptions{})
		// 	gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		// }

		// remove pending finalizer to delete addon
		gomega.Eventually(func() error {
			addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			addon.SetFinalizers([]string{})
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Update(context.Background(), addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("addon is expceted to be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
