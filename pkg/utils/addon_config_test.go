package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

type testadcGetter struct {
	adc *addonapiv1beta1.AddOnDeploymentConfig
}

func (g *testadcGetter) Get(ctx context.Context,
	namespace, name string) (*addonapiv1beta1.AddOnDeploymentConfig, error) {
	return g.adc, nil
}

// newTestAddOnDeploymentConfigGetter returns a AddOnDeploymentConfigGetter for testing
func newTestAddOnDeploymentConfigGetter(adc *addonapiv1beta1.AddOnDeploymentConfig) AddOnDeploymentConfigGetter {
	return &testadcGetter{adc: adc}
}

func TestAgentInstallNamespaceFromDeploymentConfigFunc(t *testing.T) {

	cases := []struct {
		name        string
		getter      AddOnDeploymentConfigGetter
		mca         *addonapiv1beta1.ManagedClusterAddOn
		expected    string
		expectError bool
	}{
		{
			name: "addon is nil",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{},
				}),
			mca:         nil,
			expected:    "",
			expectError: true,
		},
		{
			name: "addon no deployment config reference",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{},
				},
			},
			expected: "",
		},
		{
			name: "addon deployment config reference spec hash empty",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
								ConfigReferent: addonapiv1beta1.ConfigReferent{
									Name: "test1",
								},
							},
						},
					},
				},
			},
			expected:    "",
			expectError: true,
		},
		// {
		// 	name: "addon deployment config reference spec hash not match",
		// 	getter: newTestAddOnDeploymentConfigGetter(
		// 		&addonapiv1beta1.AddOnDeploymentConfig{
		// 			ObjectMeta: metav1.ObjectMeta{
		// 				Name: "test1",
		// 			},
		// 			Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
		// 				AgentInstallNamespace: "testns",
		// 			},
		// 		}),
		// 	mca: &addonapiv1beta1.ManagedClusterAddOn{
		// 		ObjectMeta: metav1.ObjectMeta{
		// 			Name:      "test1",
		// 			Namespace: "cluster1",
		// 		},
		// 		Status: addonapiv1beta1.ManagedClusterAddOnStatus{
		// 			ConfigReferences: []addonapiv1beta1.ConfigReference{
		// 				{
		// 					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
		// 						Group:    "addon.open-cluster-management.io",
		// 						Resource: "addondeploymentconfigs",
		// 					},
		// 					ConfigReferent: addonapiv1beta1.ConfigReferent{
		// 						Name: "test1",
		// 					},
		// 					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
		// 						SpecHash: "wronghash",
		// 					},
		// 				},
		// 			},
		// 		},
		// 	},
		// 	expected: "",
		// },
		{
			name: "addon deployment config reference spec hash match",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
						AgentInstallNamespace: "testns",
					},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
								ConfigReferent: addonapiv1beta1.ConfigReferent{
									Name: "test1",
								},
								SpecHash: "f97b3f6af1f786ec6f3273e2d6fc8717e45cb7bc9797ba7533663a7de84a5538",
							},
						},
					},
				},
			},
			expected: "testns",
		},
		{
			name: "addon supports deployment config but Configured condition absent - should requeue",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
						AgentInstallNamespace: "custom-ns",
					},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
						{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
					},
					ConfigReferences: []addonapiv1beta1.ConfigReference{},
				},
			},
			expected:    "",
			expectError: true,
		},
		{
			name: "addon supports deployment config but Configured condition is False - should requeue",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
						AgentInstallNamespace: "custom-ns",
					},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
						{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
					},
					ConfigReferences: []addonapiv1beta1.ConfigReference{},
					Conditions: []metav1.Condition{
						{
							Type:   addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
							Status: metav1.ConditionFalse,
							Reason: "ConfigurationsNotConfigured",
						},
					},
				},
			},
			expected:    "",
			expectError: true,
		},
		{
			name: "addon supports deployment config and Configured=True but no config exists - use default",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
						{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
					},
					ConfigReferences: []addonapiv1beta1.ConfigReference{},
					Conditions: []metav1.Condition{
						{
							Type:   addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
							Status: metav1.ConditionTrue,
							Reason: "ConfigurationsConfigured",
						},
					},
				},
			},
			expected:    "",
			expectError: false,
		},
		{
			name: "addon does not support deployment config and no config - use default",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1beta1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{},
				}),
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{},
					ConfigReferences: []addonapiv1beta1.ConfigReference{},
				},
			},
			expected:    "",
			expectError: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nsFunc := AgentInstallNamespaceFromDeploymentConfigFunc(c.getter)
			ns, err := nsFunc(context.TODO(), c.mca)
			assert.Equal(t, c.expected, ns, "namespace should be equal")
			if c.expectError {
				assert.Error(t, err, "should return error")
			} else {
				assert.NoError(t, err, "should not return error")
			}
		})
	}
}
