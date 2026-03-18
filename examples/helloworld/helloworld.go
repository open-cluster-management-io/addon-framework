package helloworld

import (
	"embed"
	"fmt"
	"os"

	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/examples/rbac"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	DefaultHelloWorldExampleImage = "quay.io/open-cluster-management/addon-examples:latest"
	AddonName                     = "helloworld"
	InstallationNamespace         = "open-cluster-management-agent-addon"
)

//go:embed manifests
//go:embed manifests/templates
var FS embed.FS

func NewRegistrationOption(kubeConfig *rest.Config, addonName, agentName string) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(addonName, agentName),
		CSRApproveCheck:   utils.DefaultCSRApprover(agentName),
		PermissionConfig:  rbac.AddonRBAC(kubeConfig),
	}
}

func GetDefaultValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (addonfactory.Values, error) {

	image := os.Getenv("EXAMPLE_IMAGE_NAME")
	if len(image) == 0 {
		image = DefaultHelloWorldExampleImage
	}

	manifestConfig := struct {
		KubeConfigSecret string
		ClusterName      string
		Image            string
	}{
		KubeConfigSecret: fmt.Sprintf("%s-hub-kubeconfig", addon.Name),
		ClusterName:      cluster.Name,
		Image:            image,
	}

	return addonfactory.StructToValues(manifestConfig), nil
}

func AgentHealthProber() *agent.HealthProber {
	return &agent.HealthProber{
		Type: agent.HealthProberTypeDeploymentAvailability,
	}
}
