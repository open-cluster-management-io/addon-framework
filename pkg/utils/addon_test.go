package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

func TestAllowAllAddOns(t *testing.T) {
	tests := []struct {
		name     string
		mca      *addonapiv1beta1.ManagedClusterAddOn
		expected bool
	}{
		{
			name:     "nil addon",
			mca:      nil,
			expected: true,
		},
		{
			name: "empty addon",
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
			},
			expected: true,
		},
		{
			name: "addon with config references",
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addontemplates",
							},
							DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
								ConfigReferent: addonapiv1beta1.ConfigReferent{
									Name: "test-template",
								},
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
		mca      *addonapiv1beta1.ManagedClusterAddOn
		expected bool
	}{
		{
			name:     "nil addon",
			mca:      nil,
			expected: false,
		},
		{
			name: "addon without config references",
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
			},
			expected: false,
		},
		{
			name: "addon with empty config references",
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{},
				},
			},
			expected: false,
		},
		{
			name: "addon with non-template config reference",
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
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
									Name: "test-config",
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "addon with template config reference",
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addontemplates",
							},
							DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
								ConfigReferent: addonapiv1beta1.ConfigReferent{
									Name: "test-template",
								},
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
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
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
									Name: "test-config",
								},
							},
						},
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addontemplates",
							},
							DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
								ConfigReferent: addonapiv1beta1.ConfigReferent{
									Name: "test-template",
								},
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
			mca: &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "test-cluster",
				},
				Status: addonapiv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonapiv1beta1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    "other.group",
								Resource: "addontemplates",
							},
							DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
								ConfigReferent: addonapiv1beta1.ConfigReferent{
									Name: "test-template",
								},
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
