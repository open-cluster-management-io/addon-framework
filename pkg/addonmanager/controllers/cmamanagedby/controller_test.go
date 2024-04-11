package cmamanagedby

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

type testAgent struct {
	name string
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: t.name,
	}
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
		testaddons           map[string]agent.AgentAddon
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                 "no patch annotation if nil",
			cma:                  []runtime.Object{newClusterManagementAddonWithAnnotation("test", nil)},
			validateAddonActions: validateWithFinalizerAction,
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test"},
			},
		},
		{
			name: "no patch annotation if managed by not exist",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
			})},
			validateAddonActions: validateWithFinalizerAction,
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test"},
			},
		},
		{
			name: "no patch annotation if managed by is not self",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonapiv1alpha1.AddonLifecycleAnnotationKey: "xxx",
			})},
			validateAddonActions: validateWithFinalizerAction,
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test"},
			},
		},
		{
			name: "patch annotation if managed by self",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonapiv1alpha1.AddonLifecycleAnnotationKey: addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue,
			})},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				// action[0] is setting finalizer. Ignore this action
				patch := actions[1].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Annotations) != 1 || cma.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey] != "" {
					t.Errorf("cma annotation is not correct, expected self but got %s", cma.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey])
				}
			},
			testaddons: map[string]agent.AgentAddon{
				"test": &testAgent{name: "test"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.cma...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.cma {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := cmaManagedByController{
				addonClient:                  fakeAddonClient,
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				agentAddons:                  c.testaddons,
				addonFilterFunc:              utils.FilterByAddonName(c.testaddons),
				addonPatcher: patcher.NewPatcher[*addonapiv1alpha1.ClusterManagementAddOn,
					addonapiv1alpha1.ClusterManagementAddOnSpec,
					addonapiv1alpha1.ClusterManagementAddOnStatus](fakeAddonClient.AddonV1alpha1().ClusterManagementAddOns()),
			}

			for _, obj := range c.cma {
				cma := obj.(*addonapiv1alpha1.ClusterManagementAddOn)
				syncContext := addontesting.NewFakeSyncContext(t)
				err := controller.sync(context.TODO(), syncContext, cma.Name)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}

// Expect that the incoming test case has only one action that is setting finalizer
func validateWithFinalizerAction(t *testing.T, actions []clienttesting.Action) {
	addontesting.AssertActions(t, actions, "patch")
}
