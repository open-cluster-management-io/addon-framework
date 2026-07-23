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

func TestGetAddOnConfigRef(t *testing.T) {
	cases := []struct {
		name             string
		configReferences []addonapiv1beta1.ConfigReference
		group            string
		resource         string
		expectedFound    bool
		expectedName     string
	}{
		{
			name:             "no config references",
			configReferences: []addonapiv1beta1.ConfigReference{},
			group:            "addon.open-cluster-management.io",
			resource:         "addondeploymentconfigs",
			expectedFound:    false,
			expectedName:     "",
		},
		{
			name: "no matching group+resource",
			configReferences: []addonapiv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addontemplates",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "my-template"},
						SpecHash:       "hash1",
					},
				},
			},
			group:         "addon.open-cluster-management.io",
			resource:      "addondeploymentconfigs",
			expectedFound: false,
			expectedName:  "",
		},
		{
			name: "single match",
			configReferences: []addonapiv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "only-one"},
						SpecHash:       "hash1",
					},
				},
			},
			group:         "addon.open-cluster-management.io",
			resource:      "addondeploymentconfigs",
			expectedFound: true,
			expectedName:  "only-one",
		},
		{
			name: "multiple matches returns last",
			configReferences: []addonapiv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "first-adc"},
						SpecHash:       "hash1",
					},
				},
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "second-adc"},
						SpecHash:       "hash2",
					},
				},
			},
			group:         "addon.open-cluster-management.io",
			resource:      "addondeploymentconfigs",
			expectedFound: true,
			expectedName:  "second-adc",
		},
		{
			name: "multiple matches with interleaved types returns last of target type",
			configReferences: []addonapiv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "global-defaults"},
						SpecHash:       "hash1",
					},
				},
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addontemplates",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "my-template"},
						SpecHash:       "hash2",
					},
				},
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group: "addon.open-cluster-management.io", Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: "per-cluster-override"},
						SpecHash:       "hash3",
					},
				},
			},
			group:         "addon.open-cluster-management.io",
			resource:      "addondeploymentconfigs",
			expectedFound: true,
			expectedName:  "per-cluster-override",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, ref := GetAddOnConfigRef(c.configReferences, c.group, c.resource)
			assert.Equal(t, c.expectedFound, found, "found mismatch")
			if c.expectedFound {
				assert.Equal(t, c.expectedName, ref.DesiredConfig.ConfigReferent.Name,
					"should return the last matching config reference")
			}
		})
	}
}

func TestAgentInstallNamespaceFromDeploymentConfigFunc(t *testing.T) {

	cases := []struct {
		name     string
		getter   AddOnDeploymentConfigGetter
		mca      *addonapiv1beta1.ManagedClusterAddOn
		expected string
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
			mca:      nil,
			expected: "",
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
			expected: "",
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
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nsFunc := AgentInstallNamespaceFromDeploymentConfigFunc(c.getter)
			ns, _ := nsFunc(context.TODO(), c.mca)
			assert.Equal(t, c.expected, ns, "should be equal")
		})
	}
}
