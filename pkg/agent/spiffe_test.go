package agent

import (
	"context"
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

type fakeAgentAddon struct{ objects []runtime.Object }

func (f fakeAgentAddon) Manifests(context.Context, *clusterv1.ManagedCluster, *addonapiv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return f.objects, nil
}
func (f fakeAgentAddon) GetAgentAddonOptions() AgentAddonOptions {
	return AgentAddonOptions{AddonName: "my-addon"}
}

func deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "my-addon-ns"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "agent", VolumeMounts: []corev1.VolumeMount{{Name: "hub-config", MountPath: "/var/run/hub"}}}},
			Volumes: []corev1.Volume{{Name: "hub-config", VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "my-addon-hub-kubeconfig"}}}},
		}}},
	}
}

func spiffeAddOn() *addonapiv1beta1.ManagedClusterAddOn {
	return &addonapiv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: "cluster1"},
		Status: addonapiv1beta1.ManagedClusterAddOnStatus{Registrations: []addonapiv1beta1.RegistrationConfig{{
			Type: addonapiv1beta1.KubeClient, KubeClient: &addonapiv1beta1.KubeClientConfig{Driver: SPIFFEDriver}}}}}
}

func checkPod(t *testing.T, tpl corev1.PodTemplateSpec) {
	t.Helper()
	if tpl.Labels[SPIFFERoleLabel] != "addon" {
		t.Errorf("pod labels %v, want the addon role", tpl.Labels)
	}
	var hub, csi bool
	for _, v := range tpl.Spec.Volumes {
		if v.Secret != nil {
			t.Errorf("volume %s still mounts Secret %s", v.Name, v.Secret.SecretName)
		}
		hub = hub || (v.Name == "hub-config" && v.ConfigMap != nil && v.ConfigMap.Name == "my-addon-spiffe")
		csi = csi || (v.Name == SPIFFEWorkloadAPIVolume && v.CSI != nil && v.CSI.Driver == "csi.spiffe.io")
	}
	if !hub || !csi {
		t.Errorf("volumes %+v: want hub-config from the ConfigMap and the Workload API", tpl.Spec.Volumes)
	}
	mounts := 0
	for _, m := range tpl.Spec.Containers[0].VolumeMounts {
		if m.Name == SPIFFEWorkloadAPIVolume {
			mounts++
		}
	}
	if mounts != 1 {
		t.Errorf("Workload API mounted %d times", mounts)
	}
}

func TestWithSPIFFE(t *testing.T) {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment())
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: "my-addon-ns"}}
	inner := fakeAgentAddon{objects: []runtime.Object{deployment(), &unstructured.Unstructured{Object: u}, ns}}
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1",
		Annotations: map[string]string{KlusterletNameAnnotation: "klusterlet"}}}

	a := WithSPIFFE(inner)
	if WithSPIFFE(a) != a {
		t.Error("WithSPIFFE wraps twice")
	}
	objs, err := a.Manifests(context.TODO(), cluster, spiffeAddOn())
	if err != nil {
		t.Fatal(err)
	}
	checkPod(t, objs[0].(*appsv1.Deployment).Spec.Template)
	d := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(objs[1].(*unstructured.Unstructured).Object, d); err != nil {
		t.Fatal(err)
	}
	checkPod(t, d.Spec.Template)
	if l := objs[2].(*corev1.Namespace).Labels[SPIFFEAddOnNamespaceLabel]; l != "klusterlet" {
		t.Errorf("namespace label %q, want klusterlet", l)
	}
	if inner.objects[0].(*appsv1.Deployment).Spec.Template.Labels != nil || ns.Labels != nil {
		t.Error("the add-on's own objects were modified")
	}

	// Idempotent: applying twice changes nothing more.
	again, err := ApplySPIFFE(objs, cluster, "my-addon")
	if err != nil {
		t.Fatal(err)
	}
	checkPod(t, again[0].(*appsv1.Deployment).Spec.Template)

	// Without the spiffe driver, and on a cluster that doesn't report its Klusterlet, nothing is labeled.
	plain := spiffeAddOn()
	plain.Status.Registrations[0].KubeClient.Driver = "csr"
	objs, _ = a.Manifests(context.TODO(), cluster, plain)
	if objs[0].(*appsv1.Deployment).Spec.Template.Spec.Volumes[0].Secret == nil {
		t.Error("csr add-on was changed")
	}
	objs, _ = a.Manifests(context.TODO(), &clusterv1.ManagedCluster{}, spiffeAddOn())
	if objs[2].(*corev1.Namespace).Labels != nil {
		t.Error("labeled a namespace without the Klusterlet name")
	}
}

func TestSubjectCarriedBySVID(t *testing.T) {
	sub := func(user string, groups ...string) addonapiv1beta1.KubeClientSubject {
		return addonapiv1beta1.KubeClientSubject{BaseSubject: addonapiv1beta1.BaseSubject{User: user, Groups: groups}}
	}
	cases := []struct {
		s    addonapiv1beta1.KubeClientSubject
		want bool
	}{
		{sub(DefaultUser("c1", "a", "a"), DefaultGroups("c1", "a")...), true},
		{sub(DefaultUser("c1", "a", "other-agent")), true},
		{sub(DefaultUser("c2", "a", "a")), false},
		{sub(DefaultUser("c1", "b", "a")), false},
		{sub("system:serviceaccount:ns:sa"), false},
		{sub(DefaultUser("c1", "a", "a"), "system:masters"), false},
		{sub(DefaultUser("c1", "a", "x:y")), false},
	}
	for _, c := range cases {
		if got := SubjectCarriedBySVID("c1", "a", c.s); got != c.want {
			t.Errorf("%+v: got %v, want %v", c.s, got, c.want)
		}
	}
}

type registeringAgentAddon struct{ fakeAgentAddon }

func (f registeringAgentAddon) GetAgentAddonOptions() AgentAddonOptions {
	return AgentAddonOptions{AddonName: "my-addon", Registration: &RegistrationOption{
		Configurations: KubeClientSignerConfigurations("my-addon", "agent")}}
}

// TestWithSPIFFEWaitsForTheDriver checks that on a cluster whose Klusterlet uses spiffe, an add-on
// with a kubeClient registration is not rendered before the registration agent reports the
// driver: rendered without the spiffe changes, its namespace could never get the dedicated label.
func TestWithSPIFFEWaitsForTheDriver(t *testing.T) {
	ns := &corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: "my-addon-ns"}}
	objects := []runtime.Object{deployment(), ns}
	spiffeCluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1",
		Annotations: map[string]string{KlusterletNameAnnotation: "klusterlet"}}}
	otherCluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster2"}}
	noStatus := &addonapiv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: "cluster1"}}
	noDriver := spiffeAddOn()
	noDriver.Status.Registrations[0].KubeClient.Driver = ""
	csr := spiffeAddOn()
	csr.Status.Registrations[0].KubeClient.Driver = "csr"

	cases := []struct {
		name    string
		addon   AgentAddon
		cluster *clusterv1.ManagedCluster
		mca     *addonapiv1beta1.ManagedClusterAddOn
		wait    bool
		adapted bool
	}{
		{"spiffe cluster, no registration in the status yet", registeringAgentAddon{fakeAgentAddon{objects}}, spiffeCluster, noStatus, true, false},
		{"spiffe cluster, driver not reported yet", registeringAgentAddon{fakeAgentAddon{objects}}, spiffeCluster, noDriver, true, false},
		{"spiffe cluster, spiffe driver reported", registeringAgentAddon{fakeAgentAddon{objects}}, spiffeCluster, spiffeAddOn(), false, true},
		{"spiffe cluster, csr driver reported", registeringAgentAddon{fakeAgentAddon{objects}}, spiffeCluster, csr, false, false},
		{"spiffe cluster, add-on without registration", fakeAgentAddon{objects}, spiffeCluster, noStatus, false, false},
		{"other cluster, no status yet", registeringAgentAddon{fakeAgentAddon{objects}}, otherCluster, noStatus, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objs, err := WithSPIFFE(c.addon).Manifests(context.TODO(), c.cluster, c.mca)
			if c.wait {
				if err == nil || !strings.Contains(err.Error(), "waiting for the registration agent") || objs != nil {
					t.Fatalf("want to wait, got %d objects, err %v", len(objs), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			labeled := objs[1].(*corev1.Namespace).Labels[SPIFFEAddOnNamespaceLabel] != ""
			if labeled != c.adapted {
				t.Errorf("namespace labeled %v, want %v", labeled, c.adapted)
			}
		})
	}
}
