package agentdeploy

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

func TestConfigsToAnnotations(t *testing.T) {
	cases := []struct {
		name              string
		configReference   []addonapiv1alpha1.ConfigReference
		expectAnnotations map[string]string
	}{
		{
			name: "generate annotaions",
			configReference: []addonapiv1alpha1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name:      "test",
							Namespace: "open-cluster-management",
						},
						SpecHash: "hash1",
					},
				},
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Resource: "addonhubconfigs",
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
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
			configReference:   []addonapiv1alpha1.ConfigReference{},
			expectAnnotations: nil,
		},
		{
			name: "generate annotaions without DesiredConfig",
			configReference: []addonapiv1alpha1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
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
			addon := &addonapiv1alpha1.ManagedClusterAddOn{
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
			addon := &addonapiv1alpha1.ManagedClusterAddOn{
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
			name: "set updater",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					NewFakeDeployment("test-deployment", "default"),
				},
				Updaters: []agent.Updater{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Name:      "test-deployment",
							Namespace: "default",
						},
						UpdateStrategy: workapiv1.UpdateStrategy{
							Type: workapiv1.UpdateStrategyTypeServerSideApply,
							ServerSideApply: &workapiv1.ServerSideApplyConfig{
								FieldManager: "work-agent-test",
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
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workapiv1.ServerSideApplyConfig{
							FieldManager: "work-agent-test",
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
		{
			name: "merge update strategy",
			agentAddon: &testAgent{
				name: "test",
				objects: []runtime.Object{
					NewFakeDeployment("test-deployment", "default"),
				},
				Updaters: []agent.Updater{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Name:      "test-deployment",
							Namespace: "default",
						},
						UpdateStrategy: workapiv1.UpdateStrategy{
							Type: workapiv1.UpdateStrategyTypeCreateOnly,
						},
					},
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Name:      "test-deployment-2",
							Namespace: "default",
						},
						UpdateStrategy: workapiv1.UpdateStrategy{
							Type: workapiv1.UpdateStrategyTypeCreateOnly,
						},
					},
				},
				ManifestConfigs: []workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Name:      "test-deployment",
							Namespace: "default",
						},
						UpdateStrategy: &workapiv1.UpdateStrategy{
							Type: workapiv1.UpdateStrategyTypeServerSideApply,
							ServerSideApply: &workapiv1.ServerSideApplyConfig{
								FieldManager: "work-agent-test",
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
						UpdateStrategy: &workapiv1.UpdateStrategy{
							Type: workapiv1.UpdateStrategyTypeServerSideApply,
							ServerSideApply: &workapiv1.ServerSideApplyConfig{
								FieldManager: "work-agent-test",
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
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workapiv1.ServerSideApplyConfig{
							FieldManager: "work-agent-test",
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment-2",
						Namespace: "default",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeCreateOnly,
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "test-deployment-1",
						Namespace: "default",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workapiv1.ServerSideApplyConfig{
							FieldManager: "work-agent-test",
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			manifestConfigOptions, err := getManifestConfigOption(c.agentAddon, nil, nil)
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
