package cloudevents

import (
	"context"
	"encoding/json"
	"fmt"
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
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workv1listers "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
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

var jobCompleteValue = "True"

var _ = ginkgo.Describe("Agent hook deploy", func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var err error
	var managedClusterName string
	var cma *addonapiv1alpha1.ClusterManagementAddOn
	// var agentWorkClient workclientset.Interface
	var agentWorkClient workv1client.ManifestWorkInterface
	var agentWorkInformer workv1informers.ManifestWorkInformer
	var agentWorkLister workv1listers.ManifestWorkNamespaceLister

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
		clientHolder, err := startWorkAgent(ctx, managedClusterName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		agentWorkInformer = clientHolder.ManifestWorkInformer()
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

	ginkgo.It("Should install and uninstall agent successfully", func() {
		deployObj := &unstructured.Unstructured{}
		err := deployObj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		hookObj := &unstructured.Unstructured{}
		err = hookObj.UnmarshalJSON([]byte(hookJobJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{deployObj, hookObj}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
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

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
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

		// delete addon, hook manifestwork will be applied, addon is deleting and addon status will be updated.
		err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), testAddonImpl.name, metav1.DeleteOptions{})
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

			if apiequality.Semantic.DeepEqual(hookWork.Spec.Workload.Manifests[0].Raw, []byte(hookJobJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", hookWork.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

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

		// update hook manifest feedbackResult, addon will be deleted
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
									String: &jobCompleteValue,
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

		// wait for addon to be deleted
		gomega.Eventually(func() error {
			_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
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
