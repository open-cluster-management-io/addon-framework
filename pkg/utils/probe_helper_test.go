package utils

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func boolPtr(n int64) *int64 {
	return &n
}

func TestDeploymentProbe(t *testing.T) {
	cases := []struct {
		name        string
		result      workapiv1.StatusFeedbackResult
		expectedErr bool
	}{
		{
			name:        "no result",
			result:      workapiv1.StatusFeedbackResult{},
			expectedErr: true,
		},
		{
			name: "no matched value",
			result: workapiv1.StatusFeedbackResult{
				Values: []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
				},
			},
			expectedErr: true,
		},
		{
			name: "check failed with 0 replica",
			result: workapiv1.StatusFeedbackResult{
				Values: []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(0),
						},
					},
				},
			},
			expectedErr: true,
		},
		{
			name: "check passed",
			result: workapiv1.StatusFeedbackResult{
				Values: []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(2),
						},
					},
				},
			},
			expectedErr: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prober := NewDeploymentProber(types.NamespacedName{Name: "test", Namespace: "testns"})

			fields := prober.WorkProber.ProbeFields

			err := prober.WorkProber.HealthCheck(fields[0].ResourceIdentifier, c.result)
			if err != nil && !c.expectedErr {
				t.Errorf("expected no error but got %v", err)
			}

			if err == nil && c.expectedErr {
				t.Error("expected error but got no error")
			}
		})
	}
}

func TestFilterDeployment(t *testing.T) {
	deploymentJson := `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default"
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx"
						}
					]
				}
			}
		}
	}`
	objDeploymentUnstructured := &unstructured.Unstructured{}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "test",
		},
		Spec: appsv1.DeploymentSpec{},
	}
	err := objDeploymentUnstructured.UnmarshalJSON([]byte(deploymentJson))
	if err != nil {
		t.Errorf("failed to unmarshal json: %v", err)
	}
	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"namespace": "default",
				"name":      "test",
			},
		},
	}
	cases := []struct {
		name                string
		objs                []runtime.Object
		expectedDeployments int
	}{
		{
			name:                "no obj",
			objs:                []runtime.Object{},
			expectedDeployments: 0,
		},
		{
			name:                "no deployment",
			objs:                []runtime.Object{configMap},
			expectedDeployments: 0,
		},
		{
			name:                "1 deployment",
			objs:                []runtime.Object{configMap, deployment},
			expectedDeployments: 1,
		},
		{
			name:                "1 deployment from unstructured",
			objs:                []runtime.Object{configMap, objDeploymentUnstructured},
			expectedDeployments: 1,
		},
		{
			name:                "2 deployments",
			objs:                []runtime.Object{configMap, deployment, objDeploymentUnstructured},
			expectedDeployments: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deployments := FilterDeployments(c.objs)
			if len(deployments) != c.expectedDeployments {
				t.Errorf("name %s expected %d deployments but got %d", c.name, c.expectedDeployments, len(deployments))
			}
		})
	}
}
