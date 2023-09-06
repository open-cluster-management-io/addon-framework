package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

type testadcGetter struct {
	adc *addonapiv1alpha1.AddOnDeploymentConfig
}

func (g *testadcGetter) Get(ctx context.Context,
	namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error) {
	return g.adc, nil
}

// newTestAddOnDeploymentConfigGetter returns a AddOnDeploymentConfigGetter for testing
func newTestAddOnDeploymentConfigGetter(adc *addonapiv1alpha1.AddOnDeploymentConfig) AddOnDeploymentConfigGetter {
	return &testadcGetter{adc: adc}
}

func TestAgentInstallNamespaceFromDeploymentConfigFunc(t *testing.T) {

	cases := []struct {
		name     string
		getter   AddOnDeploymentConfigGetter
		mca      *addonapiv1alpha1.ManagedClusterAddOn
		expected string
	}{
		{
			name: "addon is nil",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{},
				}),
			mca:      nil,
			expected: "",
		},
		{
			name: "addon no deployment config reference",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{},
				}),
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{},
				},
			},
			expected: "",
		},
		{
			name: "addon deployment config reference spec hash empty",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{},
				}),
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test1",
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "addon deployment config reference spec hash not match",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						AgentInstallNamespace: "testns",
					},
				}),
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test1",
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								SpecHash: "wronghash",
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "addon deployment config reference spec hash match",
			getter: newTestAddOnDeploymentConfigGetter(
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						AgentInstallNamespace: "testns",
					},
				}),
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test1",
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
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
			ns := nsFunc(c.mca)
			assert.Equal(t, c.expected, ns, "should be equal")
		})
	}
}
