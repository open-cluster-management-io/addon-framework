package index

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
)

func TestIndexManagedClusterAddonByNamespace(t *testing.T) {
	if _, err := IndexManagedClusterAddonByNamespace(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	mca := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Namespace: "cluster1"}}
	keys, err := IndexManagedClusterAddonByNamespace(mca)
	if err != nil || len(keys) != 1 || keys[0] != "cluster1" {
		t.Errorf("expected [cluster1], got %v, err %v", keys, err)
	}
}

func TestIndexManagedClusterAddonByName(t *testing.T) {
	if _, err := IndexManagedClusterAddonByName(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	mca := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "helloworld"}}
	keys, err := IndexManagedClusterAddonByName(mca)
	if err != nil || len(keys) != 1 || keys[0] != "helloworld" {
		t.Errorf("expected [helloworld], got %v, err %v", keys, err)
	}
}

func TestIndexManagedClusterAddonByHostedMode(t *testing.T) {
	if _, err := IndexManagedClusterAddonByHostedMode(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	cases := []struct {
		name        string
		annotations map[string]string
		wantKeys    []string
	}{
		{name: "no annotation", annotations: nil, wantKeys: nil},
		{name: "default mode", annotations: map[string]string{addonv1beta1.InstallModeAnnotationKey: constants.InstallModeDefault}, wantKeys: nil},
		{name: "hosted mode", annotations: map[string]string{addonv1beta1.InstallModeAnnotationKey: constants.InstallModeHosted}, wantKeys: []string{HostedModeIndexKey}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mca := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Annotations: c.annotations}}
			keys, err := IndexManagedClusterAddonByHostedMode(mca)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertStringSlice(t, keys, c.wantKeys)
		})
	}
}

func TestIndexManagedClusterAddonByDeclaredHostingCluster(t *testing.T) {
	if _, err := IndexManagedClusterAddonByDeclaredHostingCluster(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	cases := []struct {
		name        string
		annotations map[string]string
		wantKeys    []string
	}{
		{name: "no annotation", annotations: nil, wantKeys: nil},
		{name: "empty annotation", annotations: map[string]string{addonv1beta1.HostingClusterNameAnnotationKey: ""}, wantKeys: nil},
		{name: "declared hosting cluster", annotations: map[string]string{addonv1beta1.HostingClusterNameAnnotationKey: "cluster1"}, wantKeys: []string{"cluster1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mca := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Annotations: c.annotations}}
			keys, err := IndexManagedClusterAddonByDeclaredHostingCluster(mca)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertStringSlice(t, keys, c.wantKeys)
		})
	}
}

func TestIndexManagedClusterByHostingCluster(t *testing.T) {
	if _, err := IndexManagedClusterByHostingCluster(&addonv1beta1.ManagedClusterAddOn{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	cases := []struct {
		name     string
		claims   []clusterv1.ManagedClusterClaim
		wantKeys []string
	}{
		{name: "no claims", claims: nil, wantKeys: nil},
		{name: "unrelated claim", claims: []clusterv1.ManagedClusterClaim{{Name: "region", Value: "us-east-1"}}, wantKeys: nil},
		{name: "hosting claim with empty value", claims: []clusterv1.ManagedClusterClaim{{Name: constants.HostingClusterClaimName, Value: ""}}, wantKeys: nil},
		{
			name: "hosting claim present",
			claims: []clusterv1.ManagedClusterClaim{
				{Name: "region", Value: "us-east-1"},
				{Name: constants.HostingClusterClaimName, Value: "hub1"},
			},
			wantKeys: []string{"hub1"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cluster := &clusterv1.ManagedCluster{Status: clusterv1.ManagedClusterStatus{ClusterClaims: c.claims}}
			keys, err := IndexManagedClusterByHostingCluster(cluster)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertStringSlice(t, keys, c.wantKeys)
		})
	}
}

func TestManifestWorkIndexers(t *testing.T) {
	newWork := func(name string, labels map[string]string) *workapiv1.ManifestWork {
		return &workapiv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "hub-ns", Labels: labels}}
	}
	cases := []struct {
		name         string
		work         *workapiv1.ManifestWork
		wantByAddon  []string
		wantByHosted []string
		wantByHook   []string
	}{
		{
			name:         "no labels",
			work:         newWork("addon-helloworld-deploy", nil),
			wantByAddon:  nil,
			wantByHosted: nil,
			wantByHook:   nil,
		},
		{
			name:         "no addon label",
			work:         newWork("addon-helloworld-deploy", map[string]string{"foo": "bar"}),
			wantByAddon:  nil,
			wantByHosted: nil,
			wantByHook:   nil,
		},
		{
			name: "default mode deploy",
			work: newWork("addon-helloworld-deploy", map[string]string{
				addonv1beta1.AddonLabelKey: "helloworld",
			}),
			wantByAddon:  []string{"hub-ns/helloworld"},
			wantByHosted: nil,
			wantByHook:   nil,
		},
		{
			name: "hosted mode deploy",
			work: newWork("addon-helloworld-hosting-cluster1", map[string]string{
				addonv1beta1.AddonLabelKey:          "helloworld",
				addonv1beta1.AddonNamespaceLabelKey: "cluster1",
			}),
			wantByAddon:  nil,
			wantByHosted: []string{"cluster1/helloworld"},
			wantByHook:   nil,
		},
		{
			name: "default mode hook",
			work: newWork(constants.PreDeleteHookWorkName("helloworld"), map[string]string{
				addonv1beta1.AddonLabelKey: "helloworld",
			}),
			wantByAddon:  nil,
			wantByHosted: nil,
			wantByHook:   nil,
		},
		{
			name: "hosted mode hook",
			work: newWork(constants.PreDeleteHookWorkName("helloworld")+"-hosting-cluster1", map[string]string{
				addonv1beta1.AddonLabelKey:          "helloworld",
				addonv1beta1.AddonNamespaceLabelKey: "cluster1",
			}),
			wantByAddon:  nil,
			wantByHosted: nil,
			wantByHook:   []string{"cluster1/helloworld"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			byAddon, err := IndexManifestWorkByAddon(c.work)
			if err != nil {
				t.Fatalf("IndexManifestWorkByAddon: unexpected error: %v", err)
			}
			assertStringSlice(t, byAddon, c.wantByAddon)

			byHosted, err := IndexManifestWorkByHostedAddon(c.work)
			if err != nil {
				t.Fatalf("IndexManifestWorkByHostedAddon: unexpected error: %v", err)
			}
			assertStringSlice(t, byHosted, c.wantByHosted)

			byHook, err := IndexManifestWorkHookByHostedAddon(c.work)
			if err != nil {
				t.Fatalf("IndexManifestWorkHookByHostedAddon: unexpected error: %v", err)
			}
			assertStringSlice(t, byHook, c.wantByHook)
		})
	}

	if _, err := IndexManifestWorkByAddon(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("IndexManifestWorkByAddon: expected error for wrong type")
	}
	if _, err := IndexManifestWorkByHostedAddon(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("IndexManifestWorkByHostedAddon: expected error for wrong type")
	}
	if _, err := IndexManifestWorkHookByHostedAddon(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("IndexManifestWorkHookByHostedAddon: expected error for wrong type")
	}
}

func TestIndexAddonByConfig(t *testing.T) {
	if _, err := IndexAddonByConfig(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	mca := &addonv1beta1.ManagedClusterAddOn{
		Status: addonv1beta1.ManagedClusterAddOnStatus{
			ConfigReferences: []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
					DesiredConfig:       &addonv1beta1.ConfigSpecHash{ConfigReferent: addonv1beta1.ConfigReferent{Namespace: "ns1", Name: "cfg1"}},
				},
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
					DesiredConfig:       &addonv1beta1.ConfigSpecHash{ConfigReferent: addonv1beta1.ConfigReferent{Name: "cluster-scoped"}},
				},
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
					DesiredConfig:       nil,
				},
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
					DesiredConfig:       &addonv1beta1.ConfigSpecHash{},
				},
			},
		},
	}
	keys, err := IndexAddonByConfig(mca)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertStringSlice(t, keys, []string{"core/configmaps/ns1/cfg1", "core/configmaps/cluster-scoped"})
}

func TestIndexClusterManagementAddonByConfig(t *testing.T) {
	if _, err := IndexClusterManagementAddonByConfig(&clusterv1.ManagedCluster{}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	cma := &addonv1beta1.ClusterManagementAddOn{
		Status: addonv1beta1.ClusterManagementAddOnStatus{
			DefaultConfigReferences: []addonv1beta1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
					DesiredConfig:       &addonv1beta1.ConfigSpecHash{ConfigReferent: addonv1beta1.ConfigReferent{Name: "default1"}},
				},
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
					DesiredConfig:       nil,
				},
			},
			InstallProgressions: []addonv1beta1.InstallProgression{
				{
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
							DesiredConfig:       &addonv1beta1.ConfigSpecHash{ConfigReferent: addonv1beta1.ConfigReferent{Name: "default1"}},
						},
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
							DesiredConfig:       &addonv1beta1.ConfigSpecHash{ConfigReferent: addonv1beta1.ConfigReferent{Namespace: "ns1", Name: "override1"}},
						},
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "configmaps"},
							DesiredConfig:       &addonv1beta1.ConfigSpecHash{ConfigReferent: addonv1beta1.ConfigReferent{Name: ""}},
						},
					},
				},
			},
		},
	}
	keys, err := IndexClusterManagementAddonByConfig(cma)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{"core/configmaps/default1": true, "core/configmaps/ns1/override1": true}
	if len(keys) != len(want) {
		t.Fatalf("expected %d keys, got %d: %v", len(want), len(keys), keys)
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected key %q", k)
		}
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
