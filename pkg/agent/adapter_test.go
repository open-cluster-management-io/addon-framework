package agent

import (
	"context"
	"errors"
	"testing"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	agentv1alpha1 "open-cluster-management.io/addon-framework/pkg/agent/v1alpha1"
)

// fakeV1alpha1Addon is a configurable stub that satisfies agentv1alpha1.AgentAddon.
type fakeV1alpha1Addon struct {
	opts agentv1alpha1.AgentAddonOptions
}

func (f *fakeV1alpha1Addon) Manifests(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (f *fakeV1alpha1Addon) GetAgentAddonOptions() agentv1alpha1.AgentAddonOptions {
	return f.opts
}

// -------------------------------------------------------------------------
// ToV1alpha1Addon / ToV1beta1Addon
// -------------------------------------------------------------------------

func TestToV1alpha1Addon_Nil(t *testing.T) {
	if got := ToV1alpha1Addon(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestToV1alpha1Addon(t *testing.T) {
	in := &addonv1beta1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-ns"},
	}
	out := ToV1alpha1Addon(in)
	if out == nil {
		t.Fatal("expected non-nil result")
	}
	if out.Name != in.Name || out.Namespace != in.Namespace {
		t.Errorf("ObjectMeta not copied: got %v/%v", out.Namespace, out.Name)
	}
}

func TestToV1beta1Addon_Nil(t *testing.T) {
	if got := ToV1beta1Addon(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestToV1beta1Addon(t *testing.T) {
	in := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-ns"},
	}
	out := ToV1beta1Addon(in)
	if out == nil {
		t.Fatal("expected non-nil result")
	}
	if out.Name != in.Name || out.Namespace != in.Namespace {
		t.Errorf("ObjectMeta not copied: got %v/%v", out.Namespace, out.Name)
	}
	if _, ok := out.Annotations[v1alpha1InstallNamespaceAnnotation]; ok {
		t.Error("annotation should not be set when InstallNamespace is empty")
	}
}

//nolint:staticcheck
func TestToV1beta1Addon_InstallNamespace(t *testing.T) {
	in := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test-addon"},
	}
	in.Spec.InstallNamespace = "custom-ns"

	out := ToV1beta1Addon(in)
	if out == nil {
		t.Fatal("expected non-nil result")
	}
	if got := out.Annotations[v1alpha1InstallNamespaceAnnotation]; got != "custom-ns" {
		t.Errorf("expected annotation %q, got %q", "custom-ns", got)
	}
}

// Roundtrip: v1alpha1 → v1beta1 → v1alpha1 preserves ObjectMeta.
func TestAddonConversionRoundtrip(t *testing.T) {
	orig := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"},
	}
	got := ToV1alpha1Addon(ToV1beta1Addon(orig))
	if got.Name != orig.Name || got.Namespace != orig.Namespace {
		t.Errorf("roundtrip mismatch: got %v/%v", got.Namespace, got.Name)
	}
}

// -------------------------------------------------------------------------
// toV1beta1RegistrationConfigs
// -------------------------------------------------------------------------

func TestToV1beta1RegistrationConfigs_KubeClient(t *testing.T) {
	in := []addonv1alpha1.RegistrationConfig{
		{
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
			Subject: addonv1alpha1.Subject{
				User:   "system:foo",
				Groups: []string{"g1"},
			},
		},
	}
	out := toV1beta1RegistrationConfigs(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %d", len(out))
	}
	kc, ok := out[0].(*KubeClientRegistration)
	if !ok {
		t.Fatalf("expected *KubeClientRegistration, got %T", out[0])
	}
	if kc.User != "system:foo" {
		t.Errorf("user mismatch: %s", kc.User)
	}
	if len(kc.Groups) != 1 || kc.Groups[0] != "g1" {
		t.Errorf("groups mismatch: %v", kc.Groups)
	}
}

func TestToV1beta1RegistrationConfigs_CustomSigner(t *testing.T) {
	in := []addonv1alpha1.RegistrationConfig{
		{
			SignerName: "example.io/my-signer",
			Subject: addonv1alpha1.Subject{
				User:              "system:bar",
				Groups:            []string{"g2"},
				OrganizationUnits: []string{"ou1"},
			},
		},
	}
	out := toV1beta1RegistrationConfigs(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %d", len(out))
	}
	cs, ok := out[0].(*CustomSignerRegistration)
	if !ok {
		t.Fatalf("expected *CustomSignerRegistration, got %T", out[0])
	}
	if cs.SignerName != "example.io/my-signer" {
		t.Errorf("signer name mismatch: %s", cs.SignerName)
	}
	if len(cs.OrganizationUnits) != 1 || cs.OrganizationUnits[0] != "ou1" {
		t.Errorf("ou mismatch: %v", cs.OrganizationUnits)
	}
}

// -------------------------------------------------------------------------
// toV1beta1HealthProber
// -------------------------------------------------------------------------

func TestToV1beta1HealthProber_Nil(t *testing.T) {
	if got := toV1beta1HealthProber(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestToV1beta1HealthProber_NoWorkProber(t *testing.T) {
	in := &agentv1alpha1.HealthProber{Type: agentv1alpha1.HealthProberTypeLease}
	out := toV1beta1HealthProber(in)
	if out == nil {
		t.Fatal("expected non-nil")
	}
	if out.Type != HealthProberTypeLease {
		t.Errorf("type mismatch: %s", out.Type)
	}
	if out.WorkProber != nil {
		t.Error("expected nil WorkProber")
	}
}

func TestToV1beta1HealthProber_WithHealthChecker(t *testing.T) {
	called := false
	in := &agentv1alpha1.HealthProber{
		Type: agentv1alpha1.HealthProberTypeWork,
		WorkProber: &agentv1alpha1.WorkHealthProber{
			ProbeFields: []agentv1alpha1.ProbeField{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: "default",
						Name:      "agent",
					},
				},
			},
			HealthChecker: func(_ []agentv1alpha1.FieldResult, _ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn) error {
				called = true
				return nil
			},
		},
	}
	out := toV1beta1HealthProber(in)
	if out == nil || out.WorkProber == nil {
		t.Fatal("expected non-nil WorkProber")
	}
	if len(out.WorkProber.ProbeFields) != 1 {
		t.Errorf("expected 1 probe field, got %d", len(out.WorkProber.ProbeFields))
	}
	// Invoke the wrapped checker to verify delegation.
	_ = out.WorkProber.HealthChecker(nil, nil, nil)
	if !called {
		t.Error("HealthChecker was not called")
	}
}

// -------------------------------------------------------------------------
// v1alpha1Adapter.Manifests
// -------------------------------------------------------------------------

func TestAdapter_Manifests(t *testing.T) {
	inner := &fakeV1alpha1Addon{opts: agentv1alpha1.AgentAddonOptions{AddonName: "test"}}
	adapter := WrapV1alpha1(inner)
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}}
	addon := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	objs, err := adapter.Manifests(context.Background(), cluster, addon)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if objs != nil {
		t.Errorf("expected nil objects, got %v", objs)
	}
}

// -------------------------------------------------------------------------
// v1alpha1Adapter.GetAgentAddonOptions
// -------------------------------------------------------------------------

func TestAdapter_GetAgentAddonOptions_UpdatersMerge(t *testing.T) {
	ri1 := workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "a"}
	ri2 := workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "b"}
	updateStrategy := workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeServerSideApply}

	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Updaters: []agentv1alpha1.Updater{
				{ResourceIdentifier: ri1, UpdateStrategy: updateStrategy},
			},
			ManifestConfigs: []workapiv1.ManifestConfigOption{
				{ResourceIdentifier: ri2},
			},
		},
	}
	adapter := WrapV1alpha1(inner)
	opts := adapter.GetAgentAddonOptions()

	// Updaters come first, then ManifestConfigs entries.
	if len(opts.ManifestConfigs) != 2 {
		t.Fatalf("expected 2 manifest configs, got %d", len(opts.ManifestConfigs))
	}
	if opts.ManifestConfigs[0].ResourceIdentifier != ri1 {
		t.Errorf("first entry should be from Updater, got %v", opts.ManifestConfigs[0].ResourceIdentifier)
	}
	if opts.ManifestConfigs[1].ResourceIdentifier != ri2 {
		t.Errorf("second entry should be from ManifestConfigs, got %v", opts.ManifestConfigs[1].ResourceIdentifier)
	}
}

func TestAdapter_GetAgentAddonOptions_AgentInstallNamespaceFunc(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				AgentInstallNamespace: func(_ *addonv1alpha1.ManagedClusterAddOn) (string, error) {
					return "dynamic-ns", nil
				},
			},
		},
	}
	adapter := WrapV1alpha1(inner)
	opts := adapter.GetAgentAddonOptions()
	if opts.AgentInstallNamespace == nil {
		t.Fatal("expected AgentInstallNamespace to be set")
	}
	ns, err := opts.AgentInstallNamespace(context.Background(), &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "dynamic-ns" {
		t.Errorf("expected 'dynamic-ns', got %q", ns)
	}
}

func TestAdapter_GetAgentAddonOptions_StaticNamespace(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				Namespace: "static-ns",
			},
		},
	}
	adapter := WrapV1alpha1(inner)
	opts := adapter.GetAgentAddonOptions()
	if opts.AgentInstallNamespace == nil {
		t.Fatal("expected AgentInstallNamespace to be set")
	}
	// Without annotation: falls back to static namespace.
	ns, err := opts.AgentInstallNamespace(context.Background(), &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "static-ns" {
		t.Errorf("expected 'static-ns', got %q", ns)
	}
}

func TestAdapter_GetAgentAddonOptions_StaticNamespace_AnnotationOverride(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				Namespace: "static-ns",
			},
		},
	}
	adapter := WrapV1alpha1(inner)
	opts := adapter.GetAgentAddonOptions()

	// Addon carrying the v1alpha1 InstallNamespace annotation should take priority.
	addon := &addonv1beta1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				v1alpha1InstallNamespaceAnnotation: "annotation-ns",
			},
		},
	}
	ns, err := opts.AgentInstallNamespace(context.Background(), addon)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "annotation-ns" {
		t.Errorf("expected 'annotation-ns', got %q", ns)
	}
}

func TestAdapter_GetAgentAddonOptions_NoRegistration(t *testing.T) {
	inner := &fakeV1alpha1Addon{opts: agentv1alpha1.AgentAddonOptions{AddonName: "test"}}
	adapter := WrapV1alpha1(inner)
	opts := adapter.GetAgentAddonOptions()
	if opts.AgentInstallNamespace != nil {
		t.Error("expected AgentInstallNamespace to be nil when no registration is set")
	}
}

func TestAdapter_GetAgentAddonOptions_HostedModeInfoFunc(t *testing.T) {
	called := false
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			HostedModeInfoFunc: func(_ *addonv1alpha1.ManagedClusterAddOn, _ *clusterv1.ManagedCluster) (string, string) {
				called = true
				return "hosting-cluster", "hosted"
			},
		},
	}
	adapter := WrapV1alpha1(inner)
	opts := adapter.GetAgentAddonOptions()
	if opts.HostedModeInfoFunc == nil {
		t.Fatal("expected HostedModeInfoFunc to be set")
	}
	h, mode := opts.HostedModeInfoFunc(&addonv1beta1.ManagedClusterAddOn{}, &clusterv1.ManagedCluster{})
	if !called {
		t.Error("HostedModeInfoFunc not called")
	}
	if h != "hosting-cluster" || mode != "hosted" {
		t.Errorf("unexpected return values: %q, %q", h, mode)
	}
}

// -------------------------------------------------------------------------
// v1alpha1Adapter.RegistrationConfigs
// -------------------------------------------------------------------------

func TestAdapter_RegistrationConfigs_NilRegistration(t *testing.T) {
	inner := &fakeV1alpha1Addon{opts: agentv1alpha1.AgentAddonOptions{}}
	adapter := &v1alpha1Adapter{inner: inner}

	configs, err := adapter.RegistrationConfigs(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configs != nil {
		t.Errorf("expected nil, got %v", configs)
	}
}

func TestAdapter_RegistrationConfigs_NilCSRConfigurations(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	configs, err := adapter.RegistrationConfigs(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configs != nil {
		t.Errorf("expected nil, got %v", configs)
	}
}

func TestAdapter_RegistrationConfigs_WithConfigs(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				CSRConfigurations: func(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn) ([]addonv1alpha1.RegistrationConfig, error) {
					return []addonv1alpha1.RegistrationConfig{
						{SignerName: certificatesv1.KubeAPIServerClientSignerName},
					}, nil
				},
			},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	configs, err := adapter.RegistrationConfigs(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
}

// -------------------------------------------------------------------------
// v1alpha1Adapter.ApproveCSR
// -------------------------------------------------------------------------

func TestAdapter_ApproveCSR_NilRegistration(t *testing.T) {
	inner := &fakeV1alpha1Addon{opts: agentv1alpha1.AgentAddonOptions{}}
	adapter := &v1alpha1Adapter{inner: inner}

	approved := adapter.ApproveCSR(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{},
		&certificatesv1.CertificateSigningRequest{})
	if approved {
		t.Error("expected false when no registration configured")
	}
}

func TestAdapter_ApproveCSR_NilApproveCheck(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	approved := adapter.ApproveCSR(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{},
		&certificatesv1.CertificateSigningRequest{})
	if approved {
		t.Error("expected false when CSRApproveCheck is nil")
	}
}

func TestAdapter_ApproveCSR_Approved(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				CSRApproveCheck: func(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn,
					_ *certificatesv1.CertificateSigningRequest) bool {
					return true
				},
			},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	approved := adapter.ApproveCSR(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{},
		&certificatesv1.CertificateSigningRequest{})
	if !approved {
		t.Error("expected true")
	}
}

// -------------------------------------------------------------------------
// v1alpha1Adapter.Sign
// -------------------------------------------------------------------------

func TestAdapter_Sign_NilRegistration(t *testing.T) {
	inner := &fakeV1alpha1Addon{opts: agentv1alpha1.AgentAddonOptions{}}
	adapter := &v1alpha1Adapter{inner: inner}

	cert, err := adapter.Sign(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{},
		&certificatesv1.CertificateSigningRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert != nil {
		t.Errorf("expected nil cert, got %v", cert)
	}
}

func TestAdapter_Sign_WithSigner(t *testing.T) {
	wantCert := []byte("fake-cert")
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				CSRSign: func(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn,
					_ *certificatesv1.CertificateSigningRequest) ([]byte, error) {
					return wantCert, nil
				},
			},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	cert, err := adapter.Sign(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{},
		&certificatesv1.CertificateSigningRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(cert) != string(wantCert) {
		t.Errorf("cert mismatch: got %q", cert)
	}
}

// -------------------------------------------------------------------------
// v1alpha1Adapter.SetHubPermissions
// -------------------------------------------------------------------------

func TestAdapter_SetHubPermissions_NilRegistration(t *testing.T) {
	inner := &fakeV1alpha1Addon{opts: agentv1alpha1.AgentAddonOptions{}}
	adapter := &v1alpha1Adapter{inner: inner}

	err := adapter.SetHubPermissions(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdapter_SetHubPermissions_NilPermissionConfig(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	err := adapter.SetHubPermissions(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdapter_SetHubPermissions_Success(t *testing.T) {
	called := false
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				PermissionConfig: func(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn) error {
					called = true
					return nil
				},
			},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	err := adapter.SetHubPermissions(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("PermissionConfig was not called")
	}
}

func TestAdapter_SetHubPermissions_SubjectNotReadyError(t *testing.T) {
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				PermissionConfig: func(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn) error {
					return &agentv1alpha1.SubjectNotReadyError{}
				},
			},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	err := adapter.SetHubPermissions(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if err == nil {
		t.Fatal("expected error")
	}
	var v1beta1Err *SubjectNotReadyError
	if !errors.As(err, &v1beta1Err) {
		t.Errorf("expected *SubjectNotReadyError, got %T: %v", err, err)
	}
}

func TestAdapter_SetHubPermissions_OtherError(t *testing.T) {
	wantErr := errors.New("some other error")
	inner := &fakeV1alpha1Addon{
		opts: agentv1alpha1.AgentAddonOptions{
			Registration: &agentv1alpha1.RegistrationOption{
				PermissionConfig: func(_ *clusterv1.ManagedCluster, _ *addonv1alpha1.ManagedClusterAddOn) error {
					return wantErr
				},
			},
		},
	}
	adapter := &v1alpha1Adapter{inner: inner}

	err := adapter.SetHubPermissions(context.Background(),
		&clusterv1.ManagedCluster{}, &addonv1beta1.ManagedClusterAddOn{})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
}
