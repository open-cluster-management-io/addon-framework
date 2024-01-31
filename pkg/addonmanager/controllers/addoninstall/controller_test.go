package addoninstall

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

type testAgent struct {
	name     string
	strategy *agent.InstallStrategy
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:       t.name,
		InstallStrategy: t.strategy,
	}
}

func newManagedClusterWithLabel(name, key, value string) *clusterv1.ManagedCluster {
	cluster := addontesting.NewManagedCluster(name)
	cluster.Labels = map[string]string{key: value}

	return cluster
}

func newManagedClusterWithAnnotation(name, key, value string) *clusterv1.ManagedCluster {
	cluster := addontesting.NewManagedCluster(name)
	cluster.Annotations = map[string]string{key: value}
	return cluster
}

func newClusterManagementAddonWithAnnotation(name string, annotations map[string]string) *addonapiv1alpha1.ClusterManagementAddOn {
	cma := addontesting.NewClusterManagementAddon(name, "", "").Build()
	cma.Annotations = annotations
	return cma
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		cma                  []runtime.Object
		addon                []runtime.Object
		testaddons           map[string]agent.AgentAddon
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                 "no install strategy",
			addon:                []runtime.Object{},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: nil},
			},
		},
		{
			name:    "all install strategy",
			addon:   []runtime.Object{},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Spec.InstallNamespace != "test" {
					t.Errorf("Install namespace is not correct, expected test but got %s", addOn.Spec.InstallNamespace)
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
			},
		},
		{
			name:    "install addon when cluster is deleting",
			addon:   []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster: []runtime.Object{addontesting.DeleteManagedCluster(addontesting.NewManagedCluster("cluster1"))},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Should not install addon when controller is deleting")
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
			},
		},
		{
			name:  "cluster has addon disable automatic installation annotation",
			addon: []runtime.Object{},
			cluster: []runtime.Object{addontesting.SetManagedClusterAnnotation(
				addontesting.NewManagedCluster("cluster1"),
				map[string]string{addonapiv1alpha1.DisableAddonAutomaticInstallationAnnotationKey: "true"})},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Should not install addon when cluster has disable automatic installation annotation")
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
			},
		},
		{
			name:                 "selector install strategy with unmatched cluster",
			addon:                []runtime.Object{},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallByLabelStrategy("test", metav1.LabelSelector{
					MatchLabels: map[string]string{"mode": "dev"},
				})},
			},
		},
		{
			name:    "selector install strategy with matched cluster",
			addon:   []runtime.Object{},
			cluster: []runtime.Object{newManagedClusterWithLabel("cluster1", "mode", "dev")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Spec.InstallNamespace != "test" {
					t.Errorf("Install namespace is not correct, expected test but got %s", addOn.Spec.InstallNamespace)
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallByLabelStrategy("test", metav1.LabelSelector{
					MatchLabels: map[string]string{"mode": "dev"},
				})},
			},
		},
		{
			name:    "multi addons on a cluster",
			addon:   []runtime.Object{},
			cluster: []runtime.Object{newManagedClusterWithLabel("cluster1", "mode", "dev")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// expect 2 create actions for each addons
				addontesting.AssertActions(t, actions, "create", "create")

				for i := 0; i < 2; i++ {
					actual := actions[i].(clienttesting.CreateActionImpl).Object
					addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
					switch addOn.Name {
					case "test1":
						if addOn.Spec.InstallNamespace != "test1" {
							t.Errorf("Install namespace is not correct, expected test1 but got %s", addOn.Spec.InstallNamespace)
						}
					case "test2":
						if addOn.Spec.InstallNamespace != "test2" {
							t.Errorf("Install namespace is not correct, expected test2 but got %s", addOn.Spec.InstallNamespace)
						}
					default:
						t.Errorf("invalid addon %v", addOn.Name)
					}
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test1": &testAgent{name: "test1", strategy: agent.InstallAllStrategy("test1")},
				"test2": &testAgent{name: "test2", strategy: agent.InstallAllStrategy("test2")},
			},
		},
		{
			name:  "managed cluster filter install strategy",
			addon: []runtime.Object{},
			cluster: []runtime.Object{
				newManagedClusterWithAnnotation("hosted-1", "mode", "hosted"),
				newManagedClusterWithAnnotation("hosted-2", "mode", "hosted"),
				addontesting.NewManagedCluster("default"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// expect one create on trigger by cluster "default"
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Spec.InstallNamespace != "test" {
					t.Errorf("Install namespace is not correct, expected test but got %s", addOn.Spec.InstallNamespace)
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallByFilterFunctionStrategy("test", func(cluster *clusterv1.ManagedCluster) bool {
					if v, ok := cluster.Annotations["mode"]; ok && v == "hosted" {
						return false
					}
					return true
				})},
			},
		},
		{
			name: "add annotation when uses install strategy",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
			})},
			addon:   []runtime.Object{},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "create")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Annotations) != 1 || cma.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey] != addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue {
					t.Errorf("cma annotation is not correct, expected self but got %s", cma.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey])
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
			},
		},
		{
			name: "override annotation when uses install strategy",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonapiv1alpha1.AddonLifecycleAnnotationKey: addonapiv1alpha1.AddonLifecycleAddonManagerAnnotationValue,
			})},
			addon:   []runtime.Object{},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "create")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Annotations) != 1 || cma.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey] != addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue {
					t.Errorf("cma annotation is not correct, expected self but got %s", cma.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey])
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
			},
		},
		{
			name: "no patch annotation if managed by self",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonapiv1alpha1.AddonLifecycleAnnotationKey: addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue,
			})},
			addon:   []runtime.Object{},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
			},
		},
		{
			name: "no patch annotation if no install strategy",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonapiv1alpha1.AddonLifecycleAnnotationKey: addonapiv1alpha1.AddonLifecycleAddonManagerAnnotationValue,
			})},
			addon:                []runtime.Object{},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append(c.addon, c.cma...)
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

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
			for _, obj := range c.cma {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonInstallController{
				addonClient:                  fakeAddonClient,
				managedClusterLister:         clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				agentAddons:                  c.testaddons,
				addonPatcher: patcher.NewPatcher[*addonapiv1alpha1.ClusterManagementAddOn,
					addonapiv1alpha1.ClusterManagementAddOnSpec,
					addonapiv1alpha1.ClusterManagementAddOnStatus](fakeAddonClient.AddonV1alpha1().ClusterManagementAddOns()),
			}

			for _, obj := range c.cluster {
				mc := obj.(*clusterv1.ManagedCluster)
				syncContext := addontesting.NewFakeSyncContext(t)
				err := controller.sync(context.TODO(), syncContext, mc.Name)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
