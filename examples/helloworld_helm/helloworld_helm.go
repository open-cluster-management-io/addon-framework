package helloworld_helm

import (
	"context"
	"embed"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	imageName              = "helloWorldHelm"
	defaultImage           = "quay.io/open-cluster-management/addon-examples:latest"
	defaultImagePullPolicy = "IfNotPresent"
)

//go:embed manifests
//go:embed manifests/charts/helloworld
//go:embed manifests/charts/helloworld/templates/_helpers.tpl
var FS embed.FS

const (
	AddonName = "helloworldhelm"
)

type global struct {
	ImagePullPolicy string            `json:"imagePullPolicy,omitempty"`
	ImagePullSecret string            `json:"imagePullSecret,omitempty"`
	ImageOverrides  map[string]string `json:"imageOverrides,omitempty"`
}
type userValues struct {
	ClusterNamespace string `json:"clusterNamespace,omitempty"`
	Global           global `json:"global"`
}

func GetDefaultValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (addonfactory.Values, error) {
	image := os.Getenv("EXAMPLE_IMAGE_NAME")
	if len(image) == 0 {
		image = defaultImage
	}

	userJsonValues := userValues{
		ClusterNamespace: cluster.GetName(),
		Global: global{
			ImagePullPolicy: defaultImagePullPolicy,
			ImageOverrides: map[string]string{
				imageName: image,
			},
		},
	}
	values, err := addonfactory.JsonStructToValues(userJsonValues)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func GetImageValues(kubeClient kubernetes.Interface) addonfactory.GetValuesFunc {
	return func(
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1beta1.ManagedClusterAddOn,
	) (addonfactory.Values, error) {
		overrideValues := addonfactory.Values{}
		for _, config := range addon.Status.ConfigReferences {
			if config.ConfigGroupResource.Group != "" ||
				config.ConfigGroupResource.Resource != "configmaps" {
				continue
			}

			if config.DesiredConfig == nil {
				continue
			}

			configMap, err := kubeClient.CoreV1().
				ConfigMaps(config.DesiredConfig.Namespace).
				Get(context.Background(), config.DesiredConfig.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}

			image, ok := configMap.Data["image"]
			if !ok {
				return nil, fmt.Errorf("no image in configmap %s/%s", config.DesiredConfig.Namespace, config.DesiredConfig.Name)
			}

			imagePullPolicy, ok := configMap.Data["imagePullPolicy"]
			if !ok {
				return nil, fmt.Errorf("no imagePullPolicy in configmap %s/%s", config.DesiredConfig.Namespace, config.DesiredConfig.Name)
			}

			userJsonValues := userValues{
				Global: global{
					ImagePullPolicy: imagePullPolicy,
					ImageOverrides: map[string]string{
						imageName: image,
					},
				},
			}
			values, err := addonfactory.JsonStructToValues(userJsonValues)
			if err != nil {
				return nil, err
			}
			overrideValues = addonfactory.MergeValues(overrideValues, values)
		}

		return overrideValues, nil
	}
}

func AgentHealthProber() *agent.HealthProber {
	return &agent.HealthProber{
		Type: agent.HealthProberTypeWork,
		WorkProber: &agent.WorkHealthProber{
			ProbeFields: []agent.ProbeField{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "*",
						Namespace: "*",
					},
					ProbeRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			},
			HealthChecker: func(fields []agent.FieldResult, cluster *clusterv1.ManagedCluster,
				addon *addonapiv1beta1.ManagedClusterAddOn) error {
				if len(fields) == 0 {
					return fmt.Errorf("no fields found in health checker")
				}
				for _, field := range fields {
					if len(field.FeedbackResult.Values) == 0 {
						continue
					}
					switch field.ResourceIdentifier.Name {
					case "helloworldhelm-agent":
						for _, value := range field.FeedbackResult.Values {
							if value.Name == "AvailableReplicas" && *value.Value.Integer == 1 {
								return nil
							}
						}
					}
				}
				return fmt.Errorf("helloworldhelmhm agent is not ready")
			},
		},
	}
}
