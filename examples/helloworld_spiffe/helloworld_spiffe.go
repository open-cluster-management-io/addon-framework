// Package helloworld_spiffe is an example add-on for clusters whose add-ons use the spiffe driver.
// Its agent has no hub kubeconfig: it authenticates to the hub's SPIFFE listener with its own
// SPIRE-issued SVID, and can act only on its own ManagedClusterAddOn on its own cluster.
package helloworld_spiffe //nolint:revive

import (
	"context"
	"embed"
	"os"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	DefaultImage = "quay.io/open-cluster-management/addon-examples:latest"
	AddonName    = "helloworld-spiffe"
	// InstallNamespace is a namespace of the add-on's own: with the spiffe driver an add-on must
	// run in a dedicated namespace, which the add-on manager labels when it creates it.
	InstallNamespace = "helloworld-spiffe"
	// AgentName is fixed, so the agent's SPIFFE ID, and the SPIRE entry for it, stay the same:
	// spiffe://<trust domain>/ocm/cluster/<cluster>/addon/helloworld-spiffe/agent/agent.
	AgentName = "agent"
	// ConditionHello is the condition the agent sets on its ManagedClusterAddOn.
	ConditionHello = "HelloFromSPIFFEAgent"
)

//go:embed manifests
//go:embed manifests/templates
var FS embed.FS

// NewRegistrationOption registers one kubeClient agent. With the spiffe driver nothing is signed
// for it; the registration only names the agent, and the hub RBAC below is bound to its groups.
func NewRegistrationOption(kubeClient kubernetes.Interface) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		Configurations: agent.KubeClientSignerConfigurations(AddonName, AgentName),
		// The agent reads and updates the status of its own ManagedClusterAddOn through the hub's
		// gRPC service, which checks it with a SubjectAccessReview.
		PermissionConfig: utils.NewRBACPermissionConfigBuilder(kubeClient).
			BindKubeClientRole(&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "open-cluster-management:helloworld-spiffe:agent"},
				Rules: []rbacv1.PolicyRule{{
					APIGroups: []string{addonapiv1beta1.GroupName},
					Resources: []string{"managedclusteraddons", "managedclusteraddons/status"},
					Verbs:     []string{"get", "list", "watch", "update", "patch"},
				}},
			}).Build(),
	}
}

func GetDefaultValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (addonfactory.Values, error) {
	image := os.Getenv("EXAMPLE_IMAGE_NAME")
	if image == "" {
		image = DefaultImage
	}
	return addonfactory.StructToValues(struct {
		HubConfigSecret string
		ClusterName     string
		Image           string
	}{
		// The add-on is written as for any driver; on a spiffe cluster the add-on manager replaces
		// this Secret with the <addon>-spiffe ConfigMap, under the same volume and path.
		HubConfigSecret: addon.Name + "-hub-kubeconfig",
		ClusterName:     cluster.Name,
		Image:           image,
	}), nil
}

func AgentHealthProber() *agent.HealthProber {
	return &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability}
}

// NewAgentAddon builds the add-on: its manifests, values, registration and health prober.
func NewAgentAddon(kubeClient kubernetes.Interface) (agent.AgentAddon, error) {
	return addonfactory.NewAgentAddonFactory(AddonName, FS, "manifests/templates").
		WithGetValuesFuncs(GetDefaultValues).
		WithAgentRegistrationOption(NewRegistrationOption(kubeClient)).
		WithAgentInstallNamespace(func(context.Context, *addonapiv1beta1.ManagedClusterAddOn) (string, error) {
			return InstallNamespace, nil
		}).
		WithAgentHealthProber(AgentHealthProber()).
		BuildTemplateAgentAddon()
}
