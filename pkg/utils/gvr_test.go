package utils

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestContainGR(t *testing.T) {
	cases := []struct {
		name     string
		gvrs     map[schema.GroupVersionResource]bool
		group    string
		resource string
		expected bool
	}{
		{
			name:     "empty map",
			gvrs:     map[schema.GroupVersionResource]bool{},
			group:    "addon.open-cluster-management.io",
			resource: "addondeploymentconfigs",
			expected: false,
		},
		{
			name:     "contain",
			gvrs:     BuiltInAddOnConfigGVRs,
			group:    "addon.open-cluster-management.io",
			resource: "addondeploymentconfigs",
			expected: true,
		},
		{
			name:     "not contain",
			gvrs:     BuiltInAddOnConfigGVRs,
			group:    "addon.open-cluster-management.io",
			resource: "fakeresource",
			expected: false,
		},
	}

	for _, c := range cases {
		r := ContainGR(c.gvrs, c.group, c.resource)
		if r != c.expected {
			t.Errorf("Name %s : expected %t, but got %t", c.name, c.expected, r)
		}
	}
}

func TestFilterOutTheBuiltInAddOnConfigGVRs(t *testing.T) {
	cases := []struct {
		name     string
		gvrs     map[schema.GroupVersionResource]bool
		expected map[schema.GroupVersionResource]bool
	}{
		{
			name:     "empty map",
			gvrs:     map[schema.GroupVersionResource]bool{},
			expected: map[schema.GroupVersionResource]bool{},
		},
		{
			name:     "only built-in GVRs",
			gvrs:     BuiltInAddOnConfigGVRs,
			expected: map[schema.GroupVersionResource]bool{},
		},
		{
			name: "contain built-in GVRs",
			gvrs: map[schema.GroupVersionResource]bool{
				AddOnDeploymentConfigGVR: true,
				AddOnTemplateGVR:         true,
				{
					Group:    "addon.open-cluster-management.io",
					Version:  "v1alpha1",
					Resource: "fakeresource",
				}: true,
			},
			expected: map[schema.GroupVersionResource]bool{
				{
					Group:    "addon.open-cluster-management.io",
					Version:  "v1alpha1",
					Resource: "fakeresource",
				}: true,
			},
		},
	}

	for _, c := range cases {
		r := FilterOutTheBuiltInAddOnConfigGVRs(c.gvrs)
		if len(r) != len(c.expected) {
			t.Errorf("Name %s : expected %d, but got %d", c.name, len(c.expected), len(r))
		}
		for gvr := range r {
			if c.expected[gvr] != true {
				t.Errorf("Name %s : expected %t, but got %t for gvr: %v", c.name, true, c.expected[gvr], gvr)
			}
		}
	}
}
