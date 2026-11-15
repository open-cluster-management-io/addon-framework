package helloworld_spiffe //nolint:revive

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// TestManifestsOnSPIFFECluster renders the add-on as the add-on manager does for a cluster whose
// add-ons use the spiffe driver: the namespace is labeled as the Klusterlet's dedicated add-on
// namespace, and the agent gets the <addon>-spiffe ConfigMap instead of a hub kubeconfig Secret,
// the SPIRE Workload API and the spiffe-role addon label.
func TestManifestsOnSPIFFECluster(t *testing.T) {
	agentAddon, err := NewAgentAddon(fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	if prober := agentAddon.GetAgentAddonOptions().HealthProber; prober == nil ||
		prober.Type != agent.HealthProberTypeDeploymentAvailability {
		t.Errorf("health prober %+v, want deployment availability", prober)
	}
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster2",
		Annotations: map[string]string{agent.KlusterletNameAnnotation: "klusterlet"}}}
	addon := &addonapiv1beta1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: AddonName, Namespace: "cluster2"},
		Status: addonapiv1beta1.ManagedClusterAddOnStatus{Registrations: []addonapiv1beta1.RegistrationConfig{{
			Type:       addonapiv1beta1.KubeClient,
			KubeClient: &addonapiv1beta1.KubeClientConfig{Driver: agent.SPIFFEDriver},
		}}},
	}

	objects, err := agent.WithSPIFFE(agentAddon).Manifests(context.Background(), cluster, addon)
	if err != nil {
		t.Fatal(err)
	}
	var ns *corev1.Namespace
	var deploy *appsv1.Deployment
	for _, o := range objects {
		switch o := o.(type) {
		case *corev1.Namespace:
			ns = o
		case *appsv1.Deployment:
			deploy = o
		}
	}
	if ns == nil || ns.Name != InstallNamespace || ns.Labels[agent.SPIFFEAddOnNamespaceLabel] != "klusterlet" {
		t.Fatalf("namespace not labeled as the Klusterlet's dedicated add-on namespace: %+v", ns)
	}
	if deploy == nil {
		t.Fatal("no Deployment")
	}
	pod := deploy.Spec.Template
	if pod.Labels[agent.SPIFFERoleLabel] != "addon" {
		t.Errorf("pod labels %v, want %s=addon", pod.Labels, agent.SPIFFERoleLabel)
	}
	var hubConfig, workloadAPI bool
	for _, v := range pod.Spec.Volumes {
		if v.Secret != nil {
			t.Errorf("volume %s still mounts Secret %s", v.Name, v.Secret.SecretName)
		}
		hubConfig = hubConfig || (v.Name == "hub-config" && v.ConfigMap != nil && v.ConfigMap.Name == agent.SPIFFEConfigMapName(AddonName))
		workloadAPI = workloadAPI || (v.CSI != nil && v.CSI.Driver == "csi.spiffe.io")
	}
	if !hubConfig || !workloadAPI {
		t.Errorf("volumes %+v: want hub-config from ConfigMap %s and the SPIRE Workload API",
			pod.Spec.Volumes, agent.SPIFFEConfigMapName(AddonName))
	}
}
