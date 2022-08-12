package addonconfig

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
)

var (
	scheme  = runtime.NewScheme()
	fakeGVR = schema.GroupVersionResource{
		Group:    "config.test",
		Version:  "v1",
		Resource: "configtests",
	}
	fakeGVK = schema.GroupVersionKind{
		Group:   fakeGVR.Group,
		Version: fakeGVR.Version,
		Kind:    "ConfigTest",
	}
)

func init() {
	scheme.AddKnownTypeWithName(fakeGVK, &unstructured.Unstructured{})
}

func TestConfigReconcile(t *testing.T) {
	cases := []struct {
		name                string
		syncKey             string
		managedClusteraddon []runtime.Object
		configs             []*unstructured.Unstructured
		validateActions     func(*testing.T, []clienttesting.Action)
	}{
		{
			name:    "no configs",
			syncKey: "test/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			configs: []*unstructured.Unstructured{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:                "no addons",
			syncKey:             "cluster1/test",
			managedClusteraddon: []runtime.Object{},
			configs: []*unstructured.Unstructured{
				newTestConfing("test", "cluster1", 1),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:    "has generation",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			configs: []*unstructured.Unstructured{
				newTestConfing("test", "cluster1", 2),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.ConfigReference.LastObservedGeneration != 2 {
					t.Errorf("Expect addon config generation is 2, but got %v", addOn.Status.ConfigReference.LastObservedGeneration)
				}
			},
		},
		{
			name:    "no generation",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			configs: []*unstructured.Unstructured{
				newTestConfing("test", "cluster1", 0),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.ConfigReference.LastObservedGeneration != 1 {
					t.Errorf("Expect addon config generation is 1, but got %v", addOn.Status.ConfigReference.LastObservedGeneration)
				}
			},
		},
		{
			name:    "cluster scope config",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			configs: []*unstructured.Unstructured{
				newTestConfing("test", "", 3),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.ConfigReference.LastObservedGeneration != 3 {
					t.Errorf("Expect addon config generation is 3, but got %v", addOn.Status.ConfigReference.LastObservedGeneration)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			addonStore := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			for _, addon := range c.managedClusteraddon {
				if err := addonStore.Add(addon); err != nil {
					t.Fatal(err)
				}
			}

			fakeDynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
			configInformer := dynamicinformer.NewDynamicSharedInformerFactory(fakeDynamicClient, 0).ForResource(fakeGVR)
			configStore := configInformer.Informer().GetStore()
			for _, config := range c.configs {
				if err := configStore.Add(config); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &configController{
				addonName:      "test",
				addonNamespace: "cluster1",
				addonClient:    fakeAddonClient,
				configLister:   configInformer.Lister(),
				addonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				eventRecorder:  eventstesting.NewTestingEventRecorder(t),
			}

			syncContext := addontesting.NewFakeSyncContext(t, c.syncKey)
			err := ctrl.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}

			c.validateActions(t, fakeAddonClient.Actions())
		})
	}

}

func newTestConfing(name, namespace string, generation int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.test/v1",
			"kind":       "ConfigTest",
			"metadata": map[string]interface{}{
				"name":       name,
				"namespace":  namespace,
				"generation": generation,
			},
		},
	}
}
