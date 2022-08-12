package clustermanagement

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/restmapper"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type testAgent struct {
	name string
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return []runtime.Object{}, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: t.name,
	}
}

func newClusterManagementOwner(name string) metav1.OwnerReference {
	clusterManagementAddon := addontesting.NewClusterManagementAddon(name, "testcrd", "testcr")
	return *metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		cluster                []runtime.Object
		testaddon              *testAgent
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
		queueLen               int
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			cluster:                []runtime.Object{},
			testaddon:              &testAgent{name: "test"},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                "no cluster",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster:              []runtime.Object{},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                   "no managedclusteraddon",
			syncKey:                "test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster:                []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon:              &testAgent{name: "test"},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "queue managedclusteraddon",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test")),
				addontesting.NewAddon("test", "cluster2", newClusterManagementOwner("test")),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
				addontesting.NewManagedCluster("cluster3"),
			},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
			queueLen:             2,
		},
		{
			name:                   "no managedclusteraddon to sync",
			syncKey:                "cluster1/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "update managedclusteraddon owner",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if len(addOn.OwnerReferences) != 1 {
					t.Errorf("Owner not updated")
				}
			},
		},
		{
			name:    "update managedclusteraddon",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test")),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.AddOnConfiguration.CRDName != "testcrd" || addOn.Status.AddOnConfiguration.CRName != "testcr" {
					t.Errorf("Config coordinate is not updated")
				}
			},
		},
		{
			name:    "no need to update managedclusteraddon",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					addon.Status.RelatedObjects = []addonapiv1alpha1.ObjectReference{
						{
							Name:     "test",
							Group:    "addon.open-cluster-management.io",
							Resource: "clustermanagementaddons",
						},
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "update related objects",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					addon.Status.RelatedObjects = []addonapiv1alpha1.ObjectReference{
						{
							Name:     "other",
							Group:    "otherGroupd",
							Resource: "otherresources",
						},
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if len(addOn.Status.RelatedObjects) != 2 {
					t.Errorf("Related object is not updated, %v", addOn.Status.RelatedObjects)
				}
			},
		},
		{
			name:    "update config reference",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					addon.Spec.Config.Namespace = "cluster1"
					addon.Spec.Config.Name = "test"
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					clusterManagementAddon := addontesting.NewClusterManagementAddon("test", "", "")
					clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource = addonapiv1alpha1.ConfigGroupResource{
						Group:    "configs.test",
						Resource: "testconfigs",
					}
					return clusterManagementAddon
				}(),
			},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.ConfigReference.Version != "v1" {
					t.Errorf("Expect addon config version is v1, but got %v", addOn.Status.ConfigReference.Version)
				}
			},
		},
		{
			name:    "no config name",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					clusterManagementAddon := addontesting.NewClusterManagementAddon("test", "", "")
					clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource = addonapiv1alpha1.ConfigGroupResource{
						Group:    "configs.test",
						Resource: "testconfigs",
					}
					return clusterManagementAddon
				}(),
			},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addontesting.AssertCondition(t, addOn.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnCondtionConfigured,
					Status:  metav1.ConditionFalse,
					Reason:  "ConfigurationWrong",
					Message: "the configuration name is required",
				})
			},
		},
		{
			name:    "no config discovered",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					clusterManagementAddon := addontesting.NewClusterManagementAddon("test", "", "")
					clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource = addonapiv1alpha1.ConfigGroupResource{
						Group:    "unknown.test",
						Resource: "unknowns",
					}
					return clusterManagementAddon
				}(),
			},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addontesting.AssertCondition(t, addOn.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnCondtionConfigured,
					Status:  metav1.ConditionFalse,
					Reason:  "ConfigurationWrong",
					Message: "the configuration resource type is not found, no matches for unknown.test/, Resource=unknowns",
				})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)

			obj := append(c.clusterManagementAddon, c.managedClusteraddon...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			for _, obj := range c.cluster {
				if err := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := clusterManagementController{
				addonClient:                  fakeAddonClient,
				mapper:                       newTestConfigRestMapper(),
				managedClusterLister:         clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:                  map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:                eventstesting.NewTestingEventRecorder(t),
			}

			syncContext := addontesting.NewFakeSyncContext(t, c.syncKey)
			err := controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())

			if c.queueLen != syncContext.Queue().Len() {
				t.Errorf("Expect queue size is %d, but got %d", c.queueLen, syncContext.Queue().Len())
			}
		})
	}
}

func newTestConfigRestMapper() meta.RESTMapper {
	return restmapper.NewDiscoveryRESTMapper([]*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Name: "configs.test",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "testconfigs", Namespaced: true, Kind: "TestConfig"},
				},
			},
		},
	})
}
