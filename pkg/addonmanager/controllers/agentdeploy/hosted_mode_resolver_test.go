package agentdeploy

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/index"
)

func TestHostedModeResolver(t *testing.T) {
	tests := []struct {
		name           string
		addon          *addonapiv1beta1.ManagedClusterAddOn
		target         *clusterv1.ManagedCluster
		hosting        *clusterv1.ManagedCluster
		cma            *addonapiv1beta1.ClusterManagementAddOn
		clusterSets    []*clusterv1beta2.ManagedClusterSet
		hostedEnabled  bool
		wantStop       bool
		wantUpdate     bool
		wantHost       string
		wantPending    bool
		wantNoValidity bool
		wantError      bool
	}{
		{
			name:          "resolves through exclusive cluster set",
			addon:         autoDiscoveryAddon(),
			target:        claimedCluster("target", "hosting", map[string]string{clusterv1beta2.ClusterSetLabel: "production"}),
			hosting:       claimedCluster("hosting", "", map[string]string{clusterv1beta2.ClusterSetLabel: "production"}),
			cma:           autoDiscoveryCMA(true),
			clusterSets:   []*clusterv1beta2.ManagedClusterSet{exclusiveClusterSet("production")},
			hostedEnabled: true,
			wantStop:      true,
			wantUpdate:    true,
			wantHost:      "hosting",
		},
		{
			name:          "resolves through label selector cluster set",
			addon:         autoDiscoveryAddon(),
			target:        claimedCluster("target", "hosting", map[string]string{"environment": "production"}),
			hosting:       claimedCluster("hosting", "", map[string]string{"environment": "production"}),
			cma:           autoDiscoveryCMA(true),
			clusterSets:   []*clusterv1beta2.ManagedClusterSet{selectorClusterSet("production", "environment", "production")},
			hostedEnabled: true,
			wantStop:      true,
			wantUpdate:    true,
			wantHost:      "hosting",
		},
		{
			name:          "waits for target claim",
			addon:         autoDiscoveryAddon(),
			target:        claimedCluster("target", "", nil),
			cma:           autoDiscoveryCMA(true),
			hostedEnabled: true,
			wantStop:      true,
			wantPending:   true,
		},
		{
			name:          "waits for hosting managed cluster",
			addon:         autoDiscoveryAddon(),
			target:        claimedCluster("target", "missing", nil),
			cma:           autoDiscoveryCMA(true),
			hostedEnabled: true,
			wantStop:      true,
			wantPending:   true,
		},
		{
			name:          "global and default cluster sets do not authorize discovery",
			addon:         autoDiscoveryAddon(),
			target:        claimedCluster("target", "hosting", map[string]string{clusterv1beta2.ClusterSetLabel: "global"}),
			hosting:       claimedCluster("hosting", "", map[string]string{clusterv1beta2.ClusterSetLabel: "global"}),
			cma:           autoDiscoveryCMA(true),
			clusterSets:   []*clusterv1beta2.ManagedClusterSet{exclusiveClusterSet("global"), exclusiveClusterSet("default")},
			hostedEnabled: true,
			wantStop:      true,
			wantPending:   true,
		},
		{
			name:           "disabled addon type keeps default behavior",
			addon:          autoDiscoveryAddon(),
			target:         claimedCluster("target", "hosting", nil),
			hosting:        claimedCluster("hosting", "", nil),
			cma:            autoDiscoveryCMA(false),
			hostedEnabled:  true,
			wantNoValidity: true,
		},
		{
			name:           "agent without hosted support keeps default behavior",
			addon:          autoDiscoveryAddon(),
			target:         claimedCluster("target", "hosting", nil),
			hosting:        claimedCluster("hosting", "", nil),
			cma:            autoDiscoveryCMA(true),
			wantNoValidity: true,
		},
		{
			name: "does not overwrite a human hosting cluster",
			addon: func() *addonapiv1beta1.ManagedClusterAddOn {
				addon := autoDiscoveryAddon()
				addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] = "chosen-by-user"
				return addon
			}(),
			target:        claimedCluster("target", "hosting", nil),
			cma:           autoDiscoveryCMA(true),
			hostedEnabled: true,
		},
		{
			name: "does not re-resolve a discovered hosting cluster",
			addon: func() *addonapiv1beta1.ManagedClusterAddOn {
				addon := autoDiscoveryAddon()
				addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] = "original"
				addon.Annotations[addonapiv1beta1.HostingClusterNameManagedByAnnotationKey] =
					addonapiv1beta1.HostingClusterNameManagedByAutoDiscoveryValue
				return addon
			}(),
			target:        claimedCluster("target", "new-host", nil),
			cma:           autoDiscoveryCMA(true),
			hostedEnabled: true,
		},
		{
			name: "removes resolver-owned annotations when hosted mode is removed",
			addon: &addonapiv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
				Name: "test", Namespace: "target", Annotations: map[string]string{
					addonapiv1beta1.HostingClusterNameAnnotationKey:          "hosting",
					addonapiv1beta1.HostingClusterNameManagedByAnnotationKey: addonapiv1beta1.HostingClusterNameManagedByAutoDiscoveryValue,
				},
			}},
			target:        claimedCluster("target", "hosting", nil),
			hostedEnabled: true,
			wantStop:      true,
			wantUpdate:    true,
		},
		{
			name: "deletion never waits for unresolved discovery",
			addon: func() *addonapiv1beta1.ManagedClusterAddOn {
				addon := autoDiscoveryAddon()
				now := metav1.Now()
				addon.DeletionTimestamp = &now
				return addon
			}(),
			target:         claimedCluster("target", "", nil),
			cma:            autoDiscoveryCMA(true),
			hostedEnabled:  true,
			wantNoValidity: true,
		},
		{
			name:          "invalid label selector is skipped and fails closed",
			addon:         autoDiscoveryAddon(),
			target:        claimedCluster("target", "hosting", map[string]string{"environment": "production"}),
			hosting:       claimedCluster("hosting", "", map[string]string{"environment": "production"}),
			cma:           autoDiscoveryCMA(true),
			clusterSets:   []*clusterv1beta2.ManagedClusterSet{selectorClusterSet("broken", "environment", "bad,value")},
			hostedEnabled: true,
			wantStop:      true,
			wantPending:   true,
		},
		{
			name:    "invalid selector does not hide a valid matching cluster set",
			addon:   autoDiscoveryAddon(),
			target:  claimedCluster("target", "hosting", map[string]string{"environment": "production"}),
			hosting: claimedCluster("hosting", "", map[string]string{"environment": "production"}),
			cma:     autoDiscoveryCMA(true),
			clusterSets: []*clusterv1beta2.ManagedClusterSet{
				selectorClusterSet("broken", "environment", "bad,value"),
				selectorClusterSet("production", "environment", "production"),
			},
			hostedEnabled: true,
			wantStop:      true,
			wantUpdate:    true,
			wantHost:      "hosting",
		},
		{
			name:    "missing label selector is skipped and never authorizes",
			addon:   autoDiscoveryAddon(),
			target:  claimedCluster("target", "hosting", map[string]string{"environment": "production"}),
			hosting: claimedCluster("hosting", "", map[string]string{"environment": "production"}),
			cma:     autoDiscoveryCMA(true),
			clusterSets: []*clusterv1beta2.ManagedClusterSet{{
				ObjectMeta: metav1.ObjectMeta{Name: "broken"},
				Spec: clusterv1beta2.ManagedClusterSetSpec{ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.LabelSelector,
				}},
			}},
			hostedEnabled: true,
			wantStop:      true,
			wantPending:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, addonClient := newTestHostedModeResolver(t, test.target, test.hosting, test.cma, test.clusterSets...)
			if err := addonClient.Tracker().Add(test.addon.DeepCopy()); err != nil {
				t.Fatal(err)
			}
			resolved, stop, err := resolver.resolve(context.Background(),
				&resolverTestAgent{name: "test", hostedModeEnabled: test.hostedEnabled}, test.target, test.addon)
			if (err != nil) != test.wantError {
				t.Fatalf("resolve error = %v, wantError %v", err, test.wantError)
			}
			if stop != test.wantStop {
				t.Fatalf("stop = %v, want %v", stop, test.wantStop)
			}

			updates := updateActions(addonClient.Actions())
			if (len(updates) == 1) != test.wantUpdate {
				t.Fatalf("update actions = %v, wantUpdate %v", addonClient.Actions(), test.wantUpdate)
			}
			if len(updates) == 1 {
				updated := updates[0].GetObject().(*addonapiv1beta1.ManagedClusterAddOn)
				if updated.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] != test.wantHost {
					t.Errorf("hosting cluster = %q, want %q", updated.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey], test.wantHost)
				}
				if test.wantHost != "" && updated.Annotations[addonapiv1beta1.HostingClusterNameManagedByAnnotationKey] !=
					addonapiv1beta1.HostingClusterNameManagedByAutoDiscoveryValue {
					t.Errorf("managed-by annotation was not set")
				}
			}

			condition := meta.FindStatusCondition(resolved.Status.Conditions,
				addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity)
			if test.wantPending && (condition == nil ||
				condition.Reason != addonapiv1beta1.HostingClusterValidityReasonAutoDiscoveryPending) {
				t.Fatalf("pending condition = %#v", condition)
			}
			if test.wantNoValidity && condition != nil {
				t.Fatalf("unexpected validity condition %#v", condition)
			}
		})
	}
}

func TestHostedModeResolverStartsManagedClusterSetInformerLazily(t *testing.T) {
	tests := []struct {
		name  string
		addon *addonapiv1beta1.ManagedClusterAddOn
		cma   *addonapiv1beta1.ClusterManagementAddOn
	}{
		{
			name:  "disabled addon type",
			addon: autoDiscoveryAddon(),
			cma:   autoDiscoveryCMA(false),
		},
		{
			name: "explicit hosting cluster",
			addon: func() *addonapiv1beta1.ManagedClusterAddOn {
				addon := autoDiscoveryAddon()
				addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey] = "hosting"
				return addon
			}(),
			cma: autoDiscoveryCMA(true),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			target := claimedCluster("target", "hosting",
				map[string]string{clusterv1beta2.ClusterSetLabel: "production"})
			hosting := claimedCluster("hosting", "",
				map[string]string{clusterv1beta2.ClusterSetLabel: "production"})
			resolver, addonClient := newTestHostedModeResolver(t, target, hosting, test.cma)
			clusterClient := clusterfake.NewSimpleClientset(exclusiveClusterSet("production"))
			informer := clusterinformerv1beta2.NewManagedClusterSetInformer(
				clusterClient, 10*time.Minute, cache.Indexers{})
			resolver.managedClusterSetInformer = informer
			resolver.managedClusterSetLister = clusterlisterv1beta2.NewManagedClusterSetLister(informer.GetIndexer())

			if err := addonClient.Tracker().Add(test.addon.DeepCopy()); err != nil {
				t.Fatal(err)
			}
			if _, _, err := resolver.resolve(ctx,
				&resolverTestAgent{name: "test", hostedModeEnabled: true}, target, test.addon); err != nil {
				t.Fatal(err)
			}
			if actions := clusterClient.Actions(); len(actions) != 0 {
				t.Fatalf("ManagedClusterSet informer started without an unresolved request: %v", actions)
			}
		})
	}
}

func TestHostedModeResolverEnqueuesAfterManagedClusterSetCacheSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := claimedCluster("target", "hosting",
		map[string]string{clusterv1beta2.ClusterSetLabel: "production"})
	hosting := claimedCluster("hosting", "",
		map[string]string{clusterv1beta2.ClusterSetLabel: "production"})
	addon := autoDiscoveryAddon()
	resolver, addonClient := newTestHostedModeResolver(t, target, hosting, autoDiscoveryCMA(true))
	if err := addonClient.Tracker().Add(addon.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	clusterClient := clusterfake.NewSimpleClientset(exclusiveClusterSet("production"))
	informer := clusterinformerv1beta2.NewManagedClusterSetInformer(
		clusterClient, 10*time.Minute, cache.Indexers{})
	resolver.managedClusterSetInformer = informer
	resolver.managedClusterSetLister = clusterlisterv1beta2.NewManagedClusterSetLister(informer.GetIndexer())
	resolver.controller.managedClusterAddonIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		index.ManagedClusterAddonByHostedMode: index.IndexManagedClusterAddonByHostedMode,
	})
	resolver.controller.queue = workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string]())
	defer resolver.controller.queue.ShutDown()
	if err := resolver.controller.managedClusterAddonIndexer.Add(addon); err != nil {
		t.Fatal(err)
	}

	resolved, stop, err := resolver.resolve(ctx,
		&resolverTestAgent{name: "test", hostedModeEnabled: true}, target, addon)
	if err != nil {
		t.Fatal(err)
	}
	if !stop {
		t.Fatal("resolver did not wait for the ManagedClusterSet cache")
	}
	condition := meta.FindStatusCondition(resolved.Status.Conditions,
		addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity)
	if condition == nil || condition.Reason != addonapiv1beta1.HostingClusterValidityReasonAutoDiscoveryPending {
		t.Fatalf("pending condition = %#v", condition)
	}
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		t.Fatal("ManagedClusterSet informer did not sync")
	}
	deadline := time.Now().Add(time.Second)
	for resolver.controller.queue.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if resolver.controller.queue.Len() == 0 {
		t.Fatal("hosted addon was not enqueued after ManagedClusterSet cache sync")
	}

	_, stop, err = resolver.resolve(ctx,
		&resolverTestAgent{name: "test", hostedModeEnabled: true}, target, addon)
	if err != nil {
		t.Fatal(err)
	}
	if !stop || len(updateActions(addonClient.Actions())) != 1 {
		t.Fatalf("resolved stop=%v, addon actions=%v", stop, addonClient.Actions())
	}
	if len(clusterClient.Actions()) == 0 {
		t.Fatal("ManagedClusterSet informer did not access the API after an unresolved request")
	}
}

func TestManagedClusterDeleteEnqueuesOwnedAndHostedAddons(t *testing.T) {
	addonIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		index.ManagedClusterAddonByNamespace:              index.IndexManagedClusterAddonByNamespace,
		index.ManagedClusterAddonByHostedMode:             index.IndexManagedClusterAddonByHostedMode,
		index.ManagedClusterAddonByDeclaredHostingCluster: index.IndexManagedClusterAddonByDeclaredHostingCluster,
	})
	for _, addon := range []*addonapiv1beta1.ManagedClusterAddOn{
		{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "target"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "dependent"}},
		{ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "moved-dependent",
			Annotations: map[string]string{addonapiv1beta1.InstallModeAnnotationKey: constants.InstallModeHosted},
		}},
		{ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "declared-dependent",
			Annotations: map[string]string{addonapiv1beta1.HostingClusterNameAnnotationKey: "target"},
		}},
	} {
		if err := addonIndexer.Add(addon); err != nil {
			t.Fatal(err)
		}
	}
	clusterIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		index.ManagedClusterByHostingCluster: index.IndexManagedClusterByHostingCluster,
	})
	if err := clusterIndexer.Add(claimedCluster("dependent", "target", nil)); err != nil {
		t.Fatal(err)
	}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	defer queue.ShutDown()
	controller := &addonDeployController{
		managedClusterAddonIndexer: addonIndexer,
		managedClusterIndexer:      clusterIndexer,
		queue:                      queue,
	}

	controller.handleHostedModeDiscoveryManagedClusterDelete(cache.DeletedFinalStateUnknown{
		Key: "target", Obj: &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "target"}},
	})
	got := map[string]bool{}
	for queue.Len() > 0 {
		key, shutdown := queue.Get()
		if shutdown {
			t.Fatal("queue shut down unexpectedly")
		}
		got[key] = true
		queue.Done(key)
	}
	for _, key := range []string{"target/test", "dependent/test", "moved-dependent/test", "declared-dependent/test"} {
		if !got[key] {
			t.Errorf("addon %s was not enqueued: %v", key, got)
		}
	}
}

type resolverTestAgent struct {
	name              string
	hostedModeEnabled bool
}

func (a *resolverTestAgent) Manifests(context.Context, *clusterv1.ManagedCluster,
	*addonapiv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (a *resolverTestAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{AddonName: a.name, HostedModeEnabled: a.hostedModeEnabled}
}

func newTestHostedModeResolver(
	t *testing.T,
	target, hosting *clusterv1.ManagedCluster,
	cma *addonapiv1beta1.ClusterManagementAddOn,
	clusterSets ...*clusterv1beta2.ManagedClusterSet,
) (*hostedModeResolver, *addonfake.Clientset) {
	t.Helper()
	clusterIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, cluster := range []*clusterv1.ManagedCluster{target, hosting} {
		if cluster != nil {
			if err := clusterIndexer.Add(cluster); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmaIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if cma != nil {
		if err := cmaIndexer.Add(cma); err != nil {
			t.Fatal(err)
		}
	}
	clusterSetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, clusterSet := range clusterSets {
		if err := clusterSetIndexer.Add(clusterSet); err != nil {
			t.Fatal(err)
		}
	}

	addonClient := addonfake.NewSimpleClientset()
	controller := &addonDeployController{
		addonClient:          addonClient,
		managedClusterLister: clusterlisterv1.NewManagedClusterLister(clusterIndexer),
	}
	return &hostedModeResolver{
		controller:                   controller,
		clusterManagementAddonLister: addonlisterv1beta1.NewClusterManagementAddOnLister(cmaIndexer),
		managedClusterSetLister:      clusterlisterv1beta2.NewManagedClusterSetLister(clusterSetIndexer),
	}, addonClient
}

func autoDiscoveryAddon() *addonapiv1beta1.ManagedClusterAddOn {
	return &addonapiv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
		Name: "test", Namespace: "target",
		Annotations: map[string]string{addonapiv1beta1.InstallModeAnnotationKey: constants.InstallModeHosted},
	}}
}

func autoDiscoveryCMA(enabled bool) *addonapiv1beta1.ClusterManagementAddOn {
	mode := addonapiv1beta1.HostedModeAutoDiscoveryModeDisable
	if enabled {
		mode = addonapiv1beta1.HostedModeAutoDiscoveryModeEnable
	}
	return &addonapiv1beta1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
			HostedModeAutoDiscovery: &addonapiv1beta1.HostedModeAutoDiscoveryConfig{Mode: mode},
		},
	}
}

func claimedCluster(name, hosting string, clusterLabels map[string]string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: clusterLabels}}
	if hosting != "" {
		cluster.Status.ClusterClaims = []clusterv1.ManagedClusterClaim{{
			Name: constants.HostingClusterClaimName, Value: hosting,
		}}
	}
	return cluster
}

func exclusiveClusterSet(name string) *clusterv1beta2.ManagedClusterSet {
	return &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: clusterv1beta2.ManagedClusterSetSpec{ClusterSelector: clusterv1beta2.ManagedClusterSelector{
			SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
		}},
	}
}

func selectorClusterSet(name, key, value string) *clusterv1beta2.ManagedClusterSet {
	return &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: clusterv1beta2.ManagedClusterSetSpec{ClusterSelector: clusterv1beta2.ManagedClusterSelector{
			SelectorType: clusterv1beta2.LabelSelector,
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: key, Operator: metav1.LabelSelectorOpIn, Values: []string{value},
			}}},
		}},
	}
}

func updateActions(actions []clienttesting.Action) []clienttesting.UpdateAction {
	var updates []clienttesting.UpdateAction
	for _, action := range actions {
		if update, ok := action.(clienttesting.UpdateAction); ok {
			updates = append(updates, update)
		}
	}
	return updates
}
