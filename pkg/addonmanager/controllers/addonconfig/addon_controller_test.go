package addonconfig

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
)

func TestAddOnReconcile(t *testing.T) {
	now := metav1.Now()
	_, cancel := context.WithCancel(context.Background())

	cases := []struct {
		name                      string
		syncKey                   string
		managedClusteraddon       []runtime.Object
		currentConfigControlllers map[string]configSyncContext
		validateControlllers      func(*testing.T, map[string]configSyncContext)
	}{
		{
			name:                "no addon",
			syncKey:             "test/test",
			managedClusteraddon: []runtime.Object{},
			currentConfigControlllers: map[string]configSyncContext{
				"test/test": {cancelFunc: cancel},
			},
			validateControlllers: func(t *testing.T, controllers map[string]configSyncContext) {
				if _, ok := controllers["test/test"]; ok {
					t.Errorf("unexpected config controllers")
				}
			},
		},
		{
			name:    "addon is deleting",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.DeletionTimestamp = &now
					return addon
				}(),
			},
			currentConfigControlllers: map[string]configSyncContext{
				"cluster1/test": {cancelFunc: cancel},
			},
			validateControlllers: func(t *testing.T, controllers map[string]configSyncContext) {
				if _, ok := controllers["cluster1/test"]; ok {
					t.Errorf("unexpected config controllers")
				}
			},
		},
		{
			name:    "config is not discovered",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			currentConfigControlllers: map[string]configSyncContext{
				"cluster1/test": {cancelFunc: cancel},
			},
			validateControlllers: func(t *testing.T, controllers map[string]configSyncContext) {
				if _, ok := controllers["cluster1/test"]; ok {
					t.Errorf("unexpected config controllers")
				}
			},
		},
		{
			name:    "addon config controller is already started",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReference.Group = "test"
					addon.Status.ConfigReference.Resource = "tests"
					addon.Status.ConfigReference.Version = "v1"
					addon.Status.ConfigReference.Namespace = "cluster1"
					addon.Status.ConfigReference.Name = "test"
					return addon
				}(),
			},
			currentConfigControlllers: map[string]configSyncContext{
				"cluster1/test": {
					configMeta: "test/v1/tests/cluster1/test",
					cancelFunc: cancel,
				},
			},
			validateControlllers: func(t *testing.T, controllers map[string]configSyncContext) {
				if _, ok := controllers["cluster1/test"]; !ok {
					t.Errorf("expected config controller, but failed")
				}
			},
		},
		{
			name:    "addon config is changed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReference.Group = "test"
					addon.Status.ConfigReference.Resource = "tests"
					addon.Status.ConfigReference.Version = "v1"
					addon.Status.ConfigReference.Namespace = "cluster1"
					addon.Status.ConfigReference.Name = "test"
					return addon
				}(),
			},
			currentConfigControlllers: map[string]configSyncContext{
				"cluster1/test": {
					configMeta: "test/v1beta1/tests/cluster1/test",
					cancelFunc: cancel,
				},
			},
			validateControlllers: func(t *testing.T, controllers map[string]configSyncContext) {
				ctrlCtx, ok := controllers["cluster1/test"]
				if !ok {
					t.Errorf("expected config controller, but failed")
				}

				ctrlCtx.cancelFunc()
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

			ctrl := &addonConfigController{
				dynamicClient:      dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
				addonClient:        fakeAddonClient,
				addonLister:        addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				configControlllers: c.currentConfigControlllers,
				eventRecorder:      eventstesting.NewTestingEventRecorder(t),
			}

			syncContext := addontesting.NewFakeSyncContext(t, c.syncKey)
			err := ctrl.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}

			c.validateControlllers(t, ctrl.configControlllers)
		})
	}
}
