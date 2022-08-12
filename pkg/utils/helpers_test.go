package utils

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/diff"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
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
			modified := resourcemerge.BoolPtr(false)
			MergeRelatedObjects(modified, &c.existingObject, c.obj)

			if !equality.Semantic.DeepEqual(c.existingObject, c.expected) {
				t.Errorf("Unexpected related object, expect %v, but got %v", c.expected, c.existingObject)
			}

			if *modified != c.modified {
				t.Errorf("Unexpected modified value")
			}
		})
	}
}

func TestUpdateManagedClusterAddOnStatus(t *testing.T) {
	nowish := metav1.Now()
	beforeish := metav1.Time{Time: nowish.Add(-10 * time.Second)}
	afterish := metav1.Time{Time: nowish.Add(10 * time.Second)}

	cases := []struct {
		name               string
		startingConditions []metav1.Condition
		newCondition       metav1.Condition
		expextedUpdated    bool
		expectedConditions []metav1.Condition
	}{
		{
			name:               "add to empty",
			startingConditions: []metav1.Condition{},
			newCondition:       addontesting.NewCondition("test", "True", "my-reason", "my-message", nil),
			expextedUpdated:    true,
			expectedConditions: []metav1.Condition{addontesting.NewCondition("test", "True", "my-reason", "my-message", nil)},
		},
		{
			name: "add to non-conflicting",
			startingConditions: []metav1.Condition{
				addontesting.NewCondition("two", "True", "my-reason", "my-message", nil),
			},
			newCondition:    addontesting.NewCondition("one", "True", "my-reason", "my-message", nil),
			expextedUpdated: true,
			expectedConditions: []metav1.Condition{
				addontesting.NewCondition("two", "True", "my-reason", "my-message", nil),
				addontesting.NewCondition("one", "True", "my-reason", "my-message", nil),
			},
		},
		{
			name: "change existing status",
			startingConditions: []metav1.Condition{
				addontesting.NewCondition("two", "True", "my-reason", "my-message", nil),
				addontesting.NewCondition("one", "True", "my-reason", "my-message", nil),
			},
			newCondition:    addontesting.NewCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			expextedUpdated: true,
			expectedConditions: []metav1.Condition{
				addontesting.NewCondition("two", "True", "my-reason", "my-message", nil),
				addontesting.NewCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			},
		},
		{
			name: "leave existing transition time",
			startingConditions: []metav1.Condition{
				addontesting.NewCondition("two", "True", "my-reason", "my-message", nil),
				addontesting.NewCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
			newCondition:    addontesting.NewCondition("one", "True", "my-reason", "my-message", &afterish),
			expextedUpdated: false,
			expectedConditions: []metav1.Condition{
				addontesting.NewCondition("two", "True", "my-reason", "my-message", nil),
				addontesting.NewCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddOnClient := fakeaddon.NewSimpleClientset(&addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "test"},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Conditions: c.startingConditions,
				},
			})

			status, updated, err := UpdateManagedClusterAddOnStatus(
				context.TODO(),
				fakeAddOnClient,
				"test", "test",
				UpdateManagedClusterAddOnConditionFn(c.newCondition),
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if updated != c.expextedUpdated {
				t.Errorf("expected %t, but %t", c.expextedUpdated, updated)
			}
			for i := range c.expectedConditions {
				expected := c.expectedConditions[i]
				actual := status.Conditions[i]
				if expected.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}
				if !equality.Semantic.DeepEqual(expected, actual) {
					t.Errorf(diff.ObjectDiff(expected, actual))
				}
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
