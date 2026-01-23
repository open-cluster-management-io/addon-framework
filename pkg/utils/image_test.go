package utils

import (
	"encoding/json"
	"testing"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// newImageRegistriesAnnotation creates a JSON annotation string for image registries
func newImageRegistriesAnnotation(registries []addonapiv1alpha1.ImageMirror) string {
	type ImageRegistries struct {
		Registries []addonapiv1alpha1.ImageMirror `json:"registries"`
	}
	data, _ := json.Marshal(ImageRegistries{Registries: registries})
	return string(data)
}

func TestOverrideImageByAnnotation(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		imageName   string
		expected    string
		expectError bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			imageName:   "docker.io/library/nginx:latest",
			expected:    "docker.io/library/nginx:latest",
			expectError: false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "docker.io/library/nginx:latest",
			expectError: false,
		},
		{
			name: "no image-registries key",
			annotations: map[string]string{
				"other-key": "other-value",
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "docker.io/library/nginx:latest",
			expectError: false,
		},
		{
			name: "invalid JSON annotation",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: "invalid-json",
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "docker.io/library/nginx:latest",
			expectError: true,
		},
		{
			name: "empty registries array",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[]}`,
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "docker.io/library/nginx:latest",
			expectError: false,
		},
		{
			name: "single registry with match",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "docker.io/library", Mirror: "quay.io/library"},
				}),
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "quay.io/library/nginx:latest",
			expectError: false,
		},
		{
			name: "single registry no match",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "docker.io/library", Mirror: "quay.io/library"},
				}),
			},
			imageName:   "gcr.io/project/image:tag",
			expected:    "gcr.io/project/image:tag",
			expectError: false,
		},
		{
			name: "multiple registries with different sources",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "docker.io/library", Mirror: "quay.io/library"},
					{Source: "gcr.io/project", Mirror: "quay.io/project"},
				}),
			},
			imageName:   "gcr.io/project/myapp:latest",
			expected:    "quay.io/project/myapp:latest",
			expectError: false,
		},
		{
			name: "multiple registries latter wins when both match",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "docker.io/library", Mirror: "quay.io/library"},
					{Source: "docker.io/library/nginx:latest", Mirror: "private.registry.io/nginx:v2"},
				}),
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "private.registry.io/nginx:v2",
			expectError: false,
		},
		{
			name: "empty source acts as wildcard",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "", Mirror: "private.registry.io/mirror"},
				}),
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "private.registry.io/mirror/nginx:latest",
			expectError: false,
		},
		{
			name: "trailing slashes trimmed",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "docker.io/library/", Mirror: "quay.io/library/"},
				}),
			},
			imageName:   "docker.io/library/nginx:latest",
			expected:    "quay.io/library/nginx:latest",
			expectError: false,
		},
		{
			name: "image with SHA digest",
			annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: newImageRegistriesAnnotation([]addonapiv1alpha1.ImageMirror{
					{Source: "docker.io/library", Mirror: "quay.io/library"},
				}),
			},
			imageName:   "docker.io/library/nginx@sha256:abc123def456",
			expected:    "quay.io/library/nginx@sha256:abc123def456",
			expectError: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := OverrideImageByAnnotation(c.annotations, c.imageName)
			if c.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
			if result != c.expected {
				t.Errorf("expected %s but got %s", c.expected, result)
			}
		})
	}
}

func TestImageOverride(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		mirror    string
		imageName string
		expected  string
	}{
		{
			name:      "both source and mirror empty",
			source:    "",
			mirror:    "",
			imageName: "docker.io/namespace/image:tag",
			expected:  "image:tag",
		},
		{
			name:      "source empty mirror provided",
			source:    "",
			mirror:    "quay.io/namespace",
			imageName: "docker.io/namespace/image:tag",
			expected:  "quay.io/namespace/image:tag",
		},
		{
			name:      "source provided no match",
			source:    "docker.io",
			mirror:    "quay.io",
			imageName: "gcr.io/namespace/image:tag",
			expected:  "gcr.io/namespace/image:tag",
		},
		{
			name:      "source provided with match",
			source:    "docker.io/namespace",
			mirror:    "quay.io/namespace",
			imageName: "docker.io/namespace/image:tag",
			expected:  "quay.io/namespace/image:tag",
		},
		{
			name:      "trailing slashes trimmed",
			source:    "docker.io/namespace/",
			mirror:    "quay.io/namespace/",
			imageName: "docker.io/namespace/image:tag",
			expected:  "quay.io/namespace/image:tag",
		},
		{
			name:      "source with partial path match",
			source:    "docker.io/project",
			mirror:    "quay.io/project",
			imageName: "docker.io/project/sub/image:tag",
			expected:  "quay.io/project/sub/image:tag",
		},
		{
			name:      "image with sha digest",
			source:    "docker.io/library",
			mirror:    "quay.io/library",
			imageName: "docker.io/library/nginx@sha256:abc123def456",
			expected:  "quay.io/library/nginx@sha256:abc123def456",
		},
		{
			name:      "simple image name only",
			source:    "",
			mirror:    "quay.io",
			imageName: "nginx:latest",
			expected:  "quay.io/nginx:latest",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := imageOverride(c.source, c.mirror, c.imageName)
			if result != c.expected {
				t.Errorf("expected %s but got %s", c.expected, result)
			}
		})
	}
}
