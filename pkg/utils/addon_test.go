package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

func TestAllowAllAddOns(t *testing.T) {
	tests := []struct {
		name     string
		mca      *addonapiv1alpha1.ManagedClusterAddOn
		expected bool
	}{
		{
			name:     "nil addon",
			mca:      nil,
			expected: true,
		},
		{
			name: "empty addon",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
			},
			expected: true,
		},
		{
			name: "addon with config references",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addontemplates",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test-template",
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AllowAllAddOns(tt.mca)
			if result != tt.expected {
				t.Errorf("AllowAllAddOns() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFilterTemplateBasedAddOns(t *testing.T) {
	tests := []struct {
		name     string
		mca      *addonapiv1alpha1.ManagedClusterAddOn
		expected bool
	}{
		{
			name:     "nil addon",
			mca:      nil,
			expected: false,
		},
		{
			name: "addon without config references",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
			},
			expected: false,
		},
		{
			name: "addon with empty config references",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{},
				},
			},
			expected: false,
		},
		{
			name: "addon with non-template config reference",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test-config",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "addon with template config reference",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addontemplates",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test-template",
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								SpecHash: "test-hash",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "addon with mixed config references including template",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test-config",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addontemplates",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test-template",
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								SpecHash: "test-hash",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "addon with template reference but different group",
			mca: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "other.group",
								Resource: "addontemplates",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test-template",
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterTemplateBasedAddOns(tt.mca)
			if result != tt.expected {
				t.Errorf("FilterTemplateBasedAddOns() = %v, want %v", result, tt.expected)
			}
		})
	}
}
