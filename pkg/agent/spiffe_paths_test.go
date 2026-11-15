package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func podTemplate() corev1.PodTemplateSpec { return deployment().Spec.Template }

func toU(t *testing.T, obj runtime.Object) *unstructured.Unstructured {
	t.Helper()
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatal(err)
	}
	return &unstructured.Unstructured{Object: content}
}

// Every workload kind and the namespace are adapted, typed or unstructured (as template add-ons
// render them); other objects pass unchanged.
func TestApplySPIFFEKinds(t *testing.T) {
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1",
		Annotations: map[string]string{KlusterletNameAnnotation: "klusterlet"}}}
	ds := &appsv1.DaemonSet{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DaemonSet"},
		ObjectMeta: metav1.ObjectMeta{Name: "ds"}, Spec: appsv1.DaemonSetSpec{Template: podTemplate()}}
	sts := &appsv1.StatefulSet{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{Name: "sts"}, Spec: appsv1.StatefulSetSpec{Template: podTemplate()}}
	ns := &corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: "ns"}}
	cm := &corev1.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "cm"}}

	objects := []runtime.Object{ds, sts, toU(t, deployment()), toU(t, ds), toU(t, sts), toU(t, ns), toU(t, cm), cm}
	out, err := ApplySPIFFE(objects, cluster, "my-addon")
	if err != nil {
		t.Fatal(err)
	}
	checkPod(t, out[0].(*appsv1.DaemonSet).Spec.Template)
	checkPod(t, out[1].(*appsv1.StatefulSet).Spec.Template)
	for i, want := range []runtime.Object{&appsv1.Deployment{}, &appsv1.DaemonSet{}, &appsv1.StatefulSet{}} {
		u, ok := out[2+i].(*unstructured.Unstructured)
		if !ok {
			t.Fatalf("object %d: %T, want unstructured", 2+i, out[2+i])
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, want); err != nil {
			t.Fatal(err)
		}
		switch w := want.(type) {
		case *appsv1.Deployment:
			checkPod(t, w.Spec.Template)
		case *appsv1.DaemonSet:
			checkPod(t, w.Spec.Template)
		case *appsv1.StatefulSet:
			checkPod(t, w.Spec.Template)
		}
	}
	if got := out[5].(*unstructured.Unstructured).GetLabels()[SPIFFEAddOnNamespaceLabel]; got != "klusterlet" {
		t.Errorf("unstructured namespace label %q", got)
	}
	if out[6] != objects[6] || out[7] != objects[7] {
		t.Error("a ConfigMap was changed")
	}
}

func TestApplySPIFFEErrors(t *testing.T) {
	// An unstructured Deployment whose spec isn't a Deployment spec.
	bad := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "bad"}, "spec": "not a spec"}}
	if _, err := ApplySPIFFE([]runtime.Object{bad}, nil, "my-addon"); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Errorf("converting a broken Deployment: %v", err)
	}

	// Converting the adapted object back fails.
	defer func() { toUnstructured = runtime.DefaultUnstructuredConverter.ToUnstructured }()
	toUnstructured = func(any) (map[string]any, error) { return nil, errors.New("conversion failed") }
	if _, err := ApplySPIFFE([]runtime.Object{toU(t, deployment())}, nil, "my-addon"); err == nil ||
		!strings.Contains(err.Error(), "conversion failed") {
		t.Errorf("converting back: %v", err)
	}
}

type failingAgentAddon struct{ registeringAgentAddon }

func (failingAgentAddon) Manifests(context.Context, *clusterv1.ManagedCluster, *addonapiv1beta1.ManagedClusterAddOn) (
	[]runtime.Object, error) {
	return nil, errors.New("render failed")
}

type failingConfigurations struct{ fakeAgentAddon }

func (failingConfigurations) GetAgentAddonOptions() AgentAddonOptions {
	return AgentAddonOptions{AddonName: "my-addon", Registration: &RegistrationOption{
		Configurations: func(context.Context, *clusterv1.ManagedCluster, *addonapiv1beta1.ManagedClusterAddOn) (
			[]RegistrationConfig, error) {
			return nil, errors.New("configurations failed")
		}}}
}

type customSignerOnly struct{ fakeAgentAddon }

func (customSignerOnly) GetAgentAddonOptions() AgentAddonOptions {
	return AgentAddonOptions{AddonName: "my-addon", Registration: &RegistrationOption{
		Configurations: func(context.Context, *clusterv1.ManagedCluster, *addonapiv1beta1.ManagedClusterAddOn) (
			[]RegistrationConfig, error) {
			return []RegistrationConfig{&CustomSignerRegistration{SignerName: "example.com/signer"}}, nil
		}}}
}

func TestWithSPIFFEManifestsErrors(t *testing.T) {
	spiffeCluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1",
		Annotations: map[string]string{KlusterletNameAnnotation: "klusterlet"}}}
	noStatus := &addonapiv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: "cluster1"}}

	if _, err := WithSPIFFE(failingAgentAddon{}).Manifests(context.TODO(), spiffeCluster, noStatus); err == nil ||
		!strings.Contains(err.Error(), "render failed") {
		t.Errorf("the add-on's own error: %v", err)
	}
	if _, err := WithSPIFFE(failingConfigurations{}).Manifests(context.TODO(), spiffeCluster, noStatus); err == nil ||
		!strings.Contains(err.Error(), "configurations failed") {
		t.Errorf("the registration configurations' error: %v", err)
	}
	// An add-on that only registers a custom signer has no kubeClient driver to wait for.
	objs, err := WithSPIFFE(customSignerOnly{fakeAgentAddon{[]runtime.Object{deployment()}}}).Manifests(context.TODO(), spiffeCluster, noStatus)
	if err != nil || len(objs) != 1 {
		t.Errorf("custom signer only: %d objects, %v", len(objs), err)
	}
	// Wrapping twice doesn't wrap again.
	once := WithSPIFFE(fakeAgentAddon{})
	if WithSPIFFE(once) != once {
		t.Error("WithSPIFFE wrapped an already wrapped add-on")
	}
}
