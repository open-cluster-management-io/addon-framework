package agentdeploy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/index"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"
	workbuilder "open-cluster-management.io/sdk-go/pkg/apis/work/v1/builder"
)

type testHostedAgent struct {
	name               string
	objects            []runtime.Object
	err                error
	ConfigCheckEnabled bool
}

func (t *testHostedAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (
	[]runtime.Object, error) {
	return t.objects, t.err
}

func (t *testHostedAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:          t.name,
		HostedModeEnabled:  true,
		HostedModeInfoFunc: constants.GetHostedModeInfo,
		ConfigCheckEnabled: t.ConfigCheckEnabled,
	}
}

func TestHostingReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		key                  string
		existingWork         []runtime.Object
		addon                []runtime.Object
		testaddon            *testHostedAgent
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		validateWorkActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "no cluster",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster:              []runtime.Object{},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name: "no managed cluster",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster2")},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name: "no hosting cluster",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster:      []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			existingWork: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// Update addon condition
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingClusterValidity)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != addonapiv1alpha1.HostingClusterValidityReasonInvalid {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}
			},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// default sync creates a manifestWork in managed cluster
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				deployWork := actual.(*workapiv1.ManifestWork)
				if deployWork.Namespace != "cluster1" || deployWork.Name != fmt.Sprintf("%s-%v", constants.DeployWorkNamePrefix("test"), 0) {
					t.Errorf("the manifestWork %v/%v is not in managed cluster ns.", deployWork.Namespace, deployWork.Name)
				}
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name: "add finalizer",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},

			existingWork: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// Update finalizer
				addontesting.AssertActions(t, actions, "update")
				update := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := update.(*addonapiv1alpha1.ManagedClusterAddOn)
				if len(addOn.Finalizers) != 1 {
					t.Errorf("expected 1 finalizer, but got %v", len(addOn.Finalizers))
				}
				if !addonHasFinalizer(addOn, addonapiv1alpha1.AddonHostingManifestFinalizer) {
					t.Errorf("expected hosting manifest finalizer")
				}
			},
			validateWorkActions: addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name: "deploy manifests for an addon",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
			}},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingClusterValidity)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != addonapiv1alpha1.HostingClusterValidityReasonValid {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}
			},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// default sync deletes the deploy work since there is no manifests needed deploy in the managed cluster
				// hosted sync creates the deploy work in the hosting cluster ns
				addontesting.AssertActions(t, actions, "create")
			},
		},
		{
			name: "update manifest for an addon",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
			}},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkNamePrefix("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// hosted sync updates the deploy work in the hosting cluster ns
				addontesting.AssertActions(t, actions, "patch")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if meta.IsStatusConditionFalse(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied) {
					t.Errorf("Condition Reason is not correct: %v", addOn.Status.Conditions)
				}

				manifestAppliyedCondition := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied)
				if manifestAppliyedCondition == nil {
					t.Fatal("manifestapplied condition should not be nil")
				}
				if manifestAppliyedCondition.Reason != addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied {
					t.Errorf("Condition Reason is not correct: %v", manifestAppliyedCondition.Reason)
				}
				if manifestAppliyedCondition.Message != "no manifest need to apply" {
					t.Errorf("Condition Message is not correct: %v", manifestAppliyedCondition.Message)
				}
				if manifestAppliyedCondition.Status != metav1.ConditionTrue {
					t.Errorf("Condition Status is not correct: %v", manifestAppliyedCondition.Status)
				}
			},
		},
		{
			name: "do not update manifest for an addon",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
			}},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkNamePrefix("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if meta.IsStatusConditionFalse(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied) {
					t.Errorf("Condition Reason is not correct: %v", addOn.Status.Conditions)
				}
			},
		},
		{
			name: "get error when run manifest from agent",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{
				name: "test",
				objects: []runtime.Object{
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				},
				err: fmt.Errorf("run manifest failed"),
			},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkNamePrefix("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
				)
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied) {
					t.Errorf("Condition Reason is not correct: %v", addOn.Status.Conditions)
				}
			},
		},
		{
			name: "delete finalizer",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.SetAddonDeletionTimestamp(
				addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
					registrationAppliedCondition),
				time.Now(),
			)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
			}},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkNamePrefix("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
				)
				work.Labels = map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				}
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// hosted sync deletes the deploy work in the hosting cluster ns
				addontesting.AssertActions(t, actions, "delete")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				update := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := update.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addonHasFinalizer(addOn, addonapiv1alpha1.AddonHostingManifestFinalizer) {
					t.Errorf("expected hosting manifest finalizer")
				}
			},
		},
		{
			name: "deploy manifests for an addon when ConfigCheckEnabled is true",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
				registrationAppliedCondition, configuredCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
			}, ConfigCheckEnabled: true},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingClusterValidity)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != addonapiv1alpha1.HostingClusterValidityReasonValid {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}
			},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
			},
		},
		{
			name: "not deploy manifests for an addon when ConfigCheckEnabled is true",
			key:  "cluster1/test",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
				registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
			}, ConfigCheckEnabled: true},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingClusterValidity)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != addonapiv1alpha1.HostingClusterValidityReasonValid {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}
			},
			validateWorkActions: addontesting.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeWorkClient := fakework.NewSimpleClientset(c.existingWork...)
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)

			workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			err := workInformerFactory.Work().V1().ManifestWorks().Informer().AddIndexers(
				cache.Indexers{
					index.ManifestWorkByAddon:           index.IndexManifestWorkByAddon,
					index.ManifestWorkByHostedAddon:     index.IndexManifestWorkByHostedAddon,
					index.ManifestWorkHookByHostedAddon: index.IndexManifestWorkHookByHostedAddon,
				},
			)

			if err != nil {
				t.Fatal(err)
			}

			for _, obj := range c.cluster {
				if err := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.addon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.existingWork {
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonDeployController{
				workApplier:               workapplier.NewWorkApplierWithTypedClient(fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister()),
				workBuilder:               workbuilder.NewWorkBuilder(),
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				workIndexer:               workInformerFactory.Work().V1().ManifestWorks().Informer().GetIndexer(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err = controller.sync(context.TODO(), syncContext, c.key)
			if (err == nil && c.testaddon.err != nil) || (err != nil && c.testaddon.err == nil) {
				t.Errorf("expected error %v when sync got %v", c.testaddon.err, err)
			}
			if err != nil && c.testaddon.err != nil && err.Error() != c.testaddon.err.Error() {
				t.Errorf("expected error %v when sync got %v", c.testaddon.err, err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
			c.validateWorkActions(t, fakeWorkClient.Actions())
		})
	}
}

func assertHostingClusterValid(t *testing.T, actions clienttesting.Action) {
	patch := actions.(clienttesting.PatchActionImpl).Patch
	addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
	err := json.Unmarshal(patch, addOn)
	if err != nil {
		t.Fatal(err)
	}
	addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingClusterValidity)
	if addOnCond == nil {
		t.Fatal("condition should not be nil")
	}
	if addOnCond.Reason != addonapiv1alpha1.HostingClusterValidityReasonValid {
		t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
	}
}
