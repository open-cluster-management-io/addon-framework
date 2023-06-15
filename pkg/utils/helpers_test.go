package utils

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

func TestMergeRelatedObject(t *testing.T) {
	cases := []struct {
		name           string
		existingObject []addonapiv1alpha1.ObjectReference
		obj            addonapiv1alpha1.ObjectReference
		modified       bool
		expected       []addonapiv1alpha1.ObjectReference
	}{
		{
			name:           "existing is nil",
			existingObject: nil,
			obj:            relatedObject("test", "testns", "resources"),
			modified:       true,
			expected:       []addonapiv1alpha1.ObjectReference{relatedObject("test", "testns", "resources")},
		},
		{
			name:           "append to existing",
			existingObject: []addonapiv1alpha1.ObjectReference{relatedObject("test", "testns", "resources")},
			obj:            relatedObject("test", "testns", "resources1"),
			modified:       true,
			expected: []addonapiv1alpha1.ObjectReference{
				relatedObject("test", "testns", "resources"),
				relatedObject("test", "testns", "resources1"),
			},
		},
		{
			name: "no update",
			existingObject: []addonapiv1alpha1.ObjectReference{
				relatedObject("test", "testns", "resources"),
				relatedObject("test", "testns", "resources1"),
			},
			obj:      relatedObject("test", "testns", "resources1"),
			modified: false,
			expected: []addonapiv1alpha1.ObjectReference{
				relatedObject("test", "testns", "resources"),
				relatedObject("test", "testns", "resources1"),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			modified := false
			MergeRelatedObjects(&modified, &c.existingObject, c.obj)

			if !equality.Semantic.DeepEqual(c.existingObject, c.expected) {
				t.Errorf("Unexpected related object, expect %v, but got %v", c.expected, c.existingObject)
			}

			if modified != c.modified {
				t.Errorf("Unexpected modified value")
			}
		})
	}
}

func relatedObject(name, namespace, resource string) addonapiv1alpha1.ObjectReference {
	return addonapiv1alpha1.ObjectReference{
		Name:      name,
		Namespace: namespace,
		Resource:  resource,
	}
}

func TestGetSpecHash(t *testing.T) {
	cases := []struct {
		name         string
		obj          *unstructured.Unstructured
		expectedErr  error
		expectedHash string
	}{
		{
			name:        "nil object",
			obj:         nil,
			expectedErr: fmt.Errorf("object is nil"),
		},
		{
			name:        "no spec",
			obj:         &unstructured.Unstructured{},
			expectedErr: fmt.Errorf("object has no spec field"),
		},
		{
			name: "hash",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"test": 1,
					},
				},
			},
			expectedErr:  nil,
			expectedHash: "1da06016289bd76a5ada4f52fc805ae0c394612f17ec6d0f0c29b636473c8a9d",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash, err := GetSpecHash(c.obj)
			if c.expectedErr != nil {
				if err == nil {
					t.Errorf("Expected error %v, but got nil", c.expectedErr)
				}
				if err.Error() != c.expectedErr.Error() {
					t.Errorf("Expected error %v, but got %v", c.expectedErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got %v", err)
				}

				if hash != c.expectedHash {
					t.Errorf("Expected hash %s, but got %s", c.expectedHash, hash)
				}
			}
		})
	}
}
