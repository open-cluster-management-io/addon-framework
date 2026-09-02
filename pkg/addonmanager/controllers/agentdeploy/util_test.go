package agentdeploy

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

func TestConfigsToAnnotations(t *testing.T) {
	cases := []struct {
		name              string
		configReference   []addonapiv1beta1.ConfigReference
		expectAnnotations map[string]string
	}{
		{
			name: "generate annotaions",
			configReference: []addonapiv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name:      "test",
							Namespace: "open-cluster-management",
						},
						SpecHash: "hash1",
					},
				},
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Resource: "addonhubconfigs",
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name: "test",
						},
						SpecHash: "hash2",
					},
				},
			},
			expectAnnotations: map[string]string{
				workapiv1.ManifestConfigSpecHashAnnotationKey: `{"addondeploymentconfigs.addon.open-cluster-management.io/open-cluster-management/test":"hash1","addonhubconfigs//test":"hash2"}`},
		},
		{
			name:              "generate annotaions without configReference",
			configReference:   []addonapiv1beta1.ConfigReference{},
			expectAnnotations: nil,
		},
		{
			name: "generate annotaions without DesiredConfig",
			configReference: []addonapiv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
				},
			},
			expectAnnotations: map[string]string{
				workapiv1.ManifestConfigSpecHashAnnotationKey: `{}`},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			annotations, err := configsToAnnotations(c.configReference)
			assert.NoError(t, err)
			if !reflect.DeepEqual(annotations, c.expectAnnotations) {
				t.Fatalf("Expected annotations to be equal but got %v (expected) and %v (actual)", c.expectAnnotations, annotations)
			}
		})
	}
}

func TestAddonRemoveFinalizer(t *testing.T) {
	cases := []struct {
		name               string
		existingFinalizers []string
		finalizerToRemove  string
		expectedFinalizers []string
	}{
		{
			name: "no finalizers",
		},
		{
			name:               "no matched finalizer",
			existingFinalizers: []string{"test"},
			finalizerToRemove:  "test1",
			expectedFinalizers: []string{"test"},
		},
		{
			name:               "remove deprecated",
			existingFinalizers: []string{addonapiv1alpha1.AddonDeprecatedHostingPreDeleteHookFinalizer, "test"},
			finalizerToRemove:  "test1",
			expectedFinalizers: []string{"test"},
		},
		{
			name:               "remove deprecated and matched",
			existingFinalizers: []string{addonapiv1alpha1.AddonDeprecatedHostingPreDeleteHookFinalizer, "test"},
			finalizerToRemove:  "test",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addon := &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Finalizers: c.existingFinalizers},
			}
			addonRemoveFinalizer(addon, c.finalizerToRemove)
			if !reflect.DeepEqual(c.expectedFinalizers, addon.GetFinalizers()) {
				t.Errorf("expected finalizer is not correct expects %v got %v", c.expectedFinalizers, addon.Finalizers)
			}
		})
	}
}

func TestAddonAddFinalizer(t *testing.T) {
	finalizerToAdd := "test"
	cases := []struct {
		name               string
		existingFinalizers []string
		expectedFinalizers []string
	}{
		{
			name:               "no finalizers",
			expectedFinalizers: []string{"test"},
		},
		{
			name:               "append finalizer",
			existingFinalizers: []string{"test1"},
			expectedFinalizers: []string{"test1", "test"},
		},
		{
			name:               "remove deprecated",
			existingFinalizers: []string{addonapiv1alpha1.AddonDeprecatedHostingPreDeleteHookFinalizer},
			expectedFinalizers: []string{"test"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addon := &addonapiv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Finalizers: c.existingFinalizers},
			}
			addonAddFinalizer(addon, finalizerToAdd)
			if !reflect.DeepEqual(c.expectedFinalizers, addon.GetFinalizers()) {
				t.Errorf("expected finalizer is not correct expects %v got %v", c.expectedFinalizers, addon.Finalizers)
			}
		})
	}
}

func TestGetManifestConfigOption(t *testing.T) {
	cases := []struct {
		name                         string
		agentAddon                   agent.AgentAddon
		expectedManifestConfigOption []workapiv1.ManifestConfigOption
	}{
		{
			name: "no manifest config option",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
				},
			},
			expectedManifestConfigOption: []workapiv1.ManifestConfigOption{},
		},
		{
			name: "work type",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
				},
				healthProber: utils.NewDeploymentProber(types.NamespacedName{Name: "test-deployment", Namespace: "default"}),
			},
			expectedManifestConfigOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			},
		},
		{
			name: "deployment availability type",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					NewFakeDeployment("test-deployment", "default"),
				},
				healthProber: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			expectedManifestConfigOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			},
		},
		{
			name: "workload availability type",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					NewFakeDeployment("test-deployment", "default"),
					NewFakeDaemonSet("test-daemonset", "default"),
				},
				healthProber: &agent.HealthProber{Type: agent.HealthProberTypeWorkloadAvailability},
			},
			expectedManifestConfigOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "daemonsets",
						Name:      "test-daemonset",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			},
		},
		{
			name: "merge feedback rules",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					NewFakeDeployment("test-deployment", "default"),
				},
				healthProber: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
				ManifestConfigs: []workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Name:      "test-deployment",
							Namespace: "default",
						},
						FeedbackRules: []workapiv1.FeedbackRule{
							{
								Type: workapiv1.JSONPathsType,
								JsonPaths: []workapiv1.JsonPath{
									{
										Name: "test-name",
										Path: ".metadata.name",
									},
								},
							},
						},
					},
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Name:      "test-deployment-1",
							Namespace: "default",
						},
						FeedbackRules: []workapiv1.FeedbackRule{
							{
								Type: workapiv1.JSONPathsType,
								JsonPaths: []workapiv1.JsonPath{
									{
										Name: "test-name",
										Path: ".metadata.name",
									},
								},
							},
						},
					},
				},
			},
			expectedManifestConfigOption: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "test-name",
									Path: ".metadata.name",
								},
							},
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment-1",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "test-name",
									Path: ".metadata.name",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			manifestConfigOptions, err := getManifestConfigOption(context.TODO(), c.agentAddon, nil, nil)
			assert.Nil(t, err)
			assert.Equal(t, c.expectedManifestConfigOption, manifestConfigOptions)
		})
	}
}

func TestMergeFeedbackRule(t *testing.T) {
	cases := []struct {
		name                  string
		existFeedbackRules    []workapiv1.FeedbackRule
		feedbackRule          workapiv1.FeedbackRule
		expectedFeedbackRules []workapiv1.FeedbackRule
	}{
		{
			name: "no exist feedback rules",
			feedbackRule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "test-name",
						Path: ".metadata.name",
					},
				},
			},
			expectedFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
					},
				},
			},
		},
		{
			name: "no matched well known status type",
			existFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
					},
				},
			},
			feedbackRule: workapiv1.FeedbackRule{
				Type: workapiv1.WellKnownStatusType,
			},
			expectedFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
					},
				},
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		},
		{
			name: "no matched feedback rules",
			existFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
					},
				},
			},
			feedbackRule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "test-name-1",
						Path: ".metadata.name",
					},
				},
			},
			expectedFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
					},
				},
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name-1",
							Path: ".metadata.name",
						},
					},
				},
			},
		},
		{
			name: "ignore existing json paths",
			existFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
						{
							Name: "test-namespace",
							Path: ".metadata.namespace",
						},
					},
				},
			},
			feedbackRule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "test-name-1",
						Path: ".metadata.name",
					},
					{
						Name: "test-namespace", // this should be ignored
						Path: ".metadata.name",
					},
				},
			},
			expectedFeedbackRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name",
							Path: ".metadata.name",
						},
						{
							Name: "test-namespace",
							Path: ".metadata.namespace",
						},
					},
				},
				{
					Type: workapiv1.JSONPathsType,
					JsonPaths: []workapiv1.JsonPath{
						{
							Name: "test-name-1",
							Path: ".metadata.name",
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			feedbackRules := mergeFeedbackRule(c.existFeedbackRules, c.feedbackRule)
			assert.Equal(t, c.expectedFeedbackRules, feedbackRules)
		})
	}
}

func hookWorkWith(resource, feedbackName, feedbackValue string, available bool) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{}
	work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
		{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Resource:  resource,
				Name:      "test",
				Namespace: "default",
			},
		},
	}
	if available {
		work.Status.Conditions = []metav1.Condition{
			{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue},
		}
	}
	if feedbackName != "" {
		v := feedbackValue
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Resource:  resource,
						Name:      "test",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: feedbackName,
								Value: workapiv1.FieldValue{
									Type:   workapiv1.String,
									String: &v,
								},
							},
						},
					},
				},
			},
		}
	}
	return work
}

func TestHookWorkIsFailed(t *testing.T) {
	cases := []struct {
		name     string
		work     *workapiv1.ManifestWork
		expected bool
	}{
		{
			name:     "nil work",
			work:     nil,
			expected: false,
		},
		{
			name:     "work not available yet",
			work:     hookWorkWith("pods", "PodPhase", "Failed", false),
			expected: false,
		},
		{
			name:     "evicted pod (phase Failed) is failed",
			work:     hookWorkWith("pods", "PodPhase", "Failed", true),
			expected: true,
		},
		{
			name:     "pod with unknown phase is failed",
			work:     hookWorkWith("pods", "PodPhase", "Unknown", true),
			expected: true,
		},
		{
			name:     "running pod is not failed",
			work:     hookWorkWith("pods", "PodPhase", "Running", true),
			expected: false,
		},
		{
			name:     "succeeded pod is not failed",
			work:     hookWorkWith("pods", "PodPhase", "Succeeded", true),
			expected: false,
		},
		{
			name:     "pod without reported phase is not failed",
			work:     hookWorkWith("pods", "", "", true),
			expected: false,
		},
		{
			name:     "job with Failed condition true is failed",
			work:     hookWorkWith("jobs", "JobFailed", "True", true),
			expected: true,
		},
		{
			name:     "job with Failed condition false is not failed",
			work:     hookWorkWith("jobs", "JobFailed", "False", true),
			expected: false,
		},
		{
			name:     "job without failure feedback is not failed",
			work:     hookWorkWith("jobs", "JobComplete", "True", true),
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, hookWorkIsFailed(c.work))
		})
	}
}

func TestHookWorkIsCompleted(t *testing.T) {
	cases := []struct {
		name     string
		work     *workapiv1.ManifestWork
		expected bool
	}{
		{
			name:     "nil work",
			work:     nil,
			expected: false,
		},
		{
			name:     "succeeded pod is completed",
			work:     hookWorkWith("pods", "PodPhase", "Succeeded", true),
			expected: true,
		},
		{
			name:     "failed pod is not completed",
			work:     hookWorkWith("pods", "PodPhase", "Failed", true),
			expected: false,
		},
		{
			name:     "unknown pod is not completed",
			work:     hookWorkWith("pods", "PodPhase", "Unknown", true),
			expected: false,
		},
		{
			name:     "complete job is completed",
			work:     hookWorkWith("jobs", "JobComplete", "True", true),
			expected: true,
		},
		{
			name:     "failed job is not completed",
			work:     hookWorkWith("jobs", "JobFailed", "True", true),
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, hookWorkIsCompleted(c.work))
		})
	}
}

// TestHookWorkIsFailedWithUnreportedFirstManifest is a regression test for a
// hook work whose first manifest has no feedback values reported yet (e.g. the
// work-agent has not observed it) followed by a second hook resource that has
// terminally failed. The failure of the later resource must still be detected.
func TestHookWorkIsFailedWithUnreportedFirstManifest(t *testing.T) {
	failedValue := "Failed"
	work := &workapiv1.ManifestWork{}
	work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
		{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Resource:  "pods",
				Name:      "hook-1",
				Namespace: "default",
			},
		},
		{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Resource:  "pods",
				Name:      "hook-2",
				Namespace: "default",
			},
		},
	}
	work.Status.Conditions = []metav1.Condition{
		{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue},
	}
	work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
		Manifests: []workapiv1.ManifestCondition{
			{
				// First manifest has no feedback values reported yet.
				ResourceMeta: workapiv1.ManifestResourceMeta{
					Resource:  "pods",
					Name:      "hook-1",
					Namespace: "default",
				},
				StatusFeedbacks: workapiv1.StatusFeedbackResult{},
			},
			{
				// Second manifest has terminally failed (evicted).
				ResourceMeta: workapiv1.ManifestResourceMeta{
					Resource:  "pods",
					Name:      "hook-2",
					Namespace: "default",
				},
				StatusFeedbacks: workapiv1.StatusFeedbackResult{
					Values: []workapiv1.FeedbackValue{
						{
							Name: "PodPhase",
							Value: workapiv1.FieldValue{
								Type:   workapiv1.String,
								String: &failedValue,
							},
						},
					},
				},
			},
		},
	}

	if !hookWorkIsFailed(work) {
		t.Errorf("expected the failed second hook resource to be detected even though the first manifest has no feedback reported")
	}
	// FindManifestValue must locate the failed pod's phase directly.
	v := FindManifestValue(work.Status.ResourceStatus, work.Spec.ManifestConfigs[1].ResourceIdentifier, "PodPhase")
	if v.String == nil || *v.String != "Failed" {
		t.Errorf("expected FindManifestValue to return PodPhase=Failed for the second manifest, got %+v", v)
	}
}
