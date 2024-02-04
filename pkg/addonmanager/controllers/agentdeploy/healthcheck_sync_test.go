package agentdeploy

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/work/v1"
)

var manifestAppliedCondition = metav1.Condition{
	Type:   addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
	Status: metav1.ConditionTrue,
	Reason: addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
}

func boolPtr(n int64) *int64 {
	return &n
}

type healthCheckTestAgent struct {
	name   string
	health *agent.HealthProber
}

func (t *healthCheckTestAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	return []runtime.Object{NewFakeDeployment("test-deployment", "default")}, nil
}

func (t *healthCheckTestAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:    t.name,
		HealthProber: t.health,
	}
}

func NewFakeDeployment(namespace, name string) *appsv1.Deployment {
	var one int32 = 1
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespace,
			Namespace: name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"addon": "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
						},
					},
				},
			},
		},
	}
}

func TestHealthCheckReconcile(t *testing.T) {
	cases := []struct {
		name                     string
		existingWork             []runtime.Object
		addon                    *addonapiv1alpha1.ManagedClusterAddOn
		testAddon                *healthCheckTestAgent
		cluster                  *clusterv1.ManagedCluster
		expectedErr              error
		expectedHealthCheckMode  addonapiv1alpha1.HealthCheckMode
		expectAvailableCondition metav1.Condition
	}{
		{
			name:                    "healthprober is nil",
			testAddon:               &healthCheckTestAgent{name: "test", health: nil},
			addon:                   addontesting.NewAddon("test", "cluster1"),
			expectedErr:             nil,
			expectedHealthCheckMode: "",
		},
		{
			name: "Health check mode is none",
			testAddon: &healthCheckTestAgent{name: "test", health: &agent.HealthProber{
				Type: agent.HealthProberTypeNone,
			}},
			addon:                   addontesting.NewAddon("test", "cluster1"),
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
		},
		{
			name: "Health check mode is lease",
			testAddon: &healthCheckTestAgent{name: "test", health: &agent.HealthProber{
				Type: agent.HealthProberTypeLease,
			}},
			addon:                   addontesting.NewAddon("test", "cluster1"),
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeLease,
		},
		{
			name: "Health check mode is work but WorkProber is nil",
			testAddon: &healthCheckTestAgent{name: "test", health: &agent.HealthProber{
				Type: agent.HealthProberTypeWork,
			}},
			addon:                   addontesting.NewAddon("test", "cluster1"),
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkApply,
				Message: "Addon work is applied",
			},
		},
		{
			name: "Health check mode is work but manifestApplied condition is not true",
			testAddon: &healthCheckTestAgent{name: "test",
				health: utils.NewDeploymentProber(types.NamespacedName{Name: "test-deployment", Namespace: "default"})},
			addon:                    addontesting.NewAddon("test", "cluster1"),
			expectedErr:              nil,
			expectedHealthCheckMode:  addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{},
		},
		{
			name: "Health check mode is work but no work",
			testAddon: &healthCheckTestAgent{name: "test",
				health: utils.NewDeploymentProber(types.NamespacedName{Name: "test-deployment", Namespace: "default"})},
			addon:                   addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotFound,
				Message: "Work for addon is not found",
			},
		},
		{
			name: "Health check mode is work but work is unavailable",
			testAddon: &healthCheckTestAgent{name: "test",
				health: utils.NewDeploymentProber(types.NamespacedName{Name: "test-deployment", Namespace: "default"})},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:    v1.WorkAvailable,
								Status:  metav1.ConditionFalse,
								Message: "failed to apply",
							},
						},
					},
				},
			},
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotApply,
				Message: "failed to apply",
			},
		},
		{
			name: "Health check mode is work but no result",
			testAddon: &healthCheckTestAgent{name: "test",
				health: utils.NewDeploymentProber(types.NamespacedName{Name: "test-deployment", Namespace: "default"})},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1.WorkAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1alpha1.AddonAvailableReasonNoProbeResult,
				Message: "Probe results are not returned",
			},
		},
		{
			name: "Health check mode is work but WorkProber check pass",
			testAddon: &healthCheckTestAgent{name: "test",
				health: utils.NewDeploymentProber(types.NamespacedName{Name: "test-deployment", Namespace: "default"}),
			},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						ResourceStatus: v1.ManifestResourceStatus{
							Manifests: []v1.ManifestCondition{
								{
									ResourceMeta: v1.ManifestResourceMeta{
										Ordinal:   0,
										Group:     "apps",
										Version:   "",
										Kind:      "",
										Resource:  "deployments",
										Name:      "test-deployment",
										Namespace: "default",
									},
									StatusFeedbacks: v1.StatusFeedbackResult{
										Values: []v1.FeedbackValue{
											{
												Name: "Replicas",
												Value: v1.FieldValue{
													Integer: boolPtr(1),
												},
											},
											{
												Name: "ReadyReplicas",
												Value: v1.FieldValue{
													Integer: boolPtr(2),
												},
											},
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   v1.WorkAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1alpha1.AddonAvailableReasonProbeAvailable,
				Message: "test add-on is available.",
			},
		},

		{
			name: "Health check mode is deployment availability but manifestApplied condition is not true",
			testAddon: &healthCheckTestAgent{name: "test",
				health: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			addon:                    addontesting.NewAddon("test", "cluster1"),
			expectedErr:              nil,
			expectedHealthCheckMode:  addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{},
		},
		{
			name: "Health check mode is deployment availability but no work",
			testAddon: &healthCheckTestAgent{name: "test",
				health: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			addon:                   addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotFound,
				Message: "Work for addon is not found",
			},
		},
		{
			name: "Health check mode is deployment availability but work is unavailable",
			testAddon: &healthCheckTestAgent{name: "test",
				health: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:    v1.WorkAvailable,
								Status:  metav1.ConditionFalse,
								Message: "failed to apply",
							},
						},
					},
				},
			},
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotApply,
				Message: "failed to apply",
			},
		},
		{
			name: "Health check mode is deployment availability but no result",
			testAddon: &healthCheckTestAgent{name: "test",
				health: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1.WorkAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1alpha1.AddonAvailableReasonNoProbeResult,
				Message: "Probe results are not returned",
			},
		},
		{
			name: "Health check mode is deployment availability but cluster availability is unknown",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.ManagedClusterConditionAvailable,
							Status: metav1.ConditionUnknown,
						},
					},
				},
			},
			testAddon: &healthCheckTestAgent{name: "test",
				health: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						ResourceStatus: v1.ManifestResourceStatus{
							Manifests: []v1.ManifestCondition{
								{
									ResourceMeta: v1.ManifestResourceMeta{
										Ordinal:   0,
										Group:     "apps",
										Version:   "",
										Kind:      "",
										Resource:  "deployments",
										Name:      "test-deployment",
										Namespace: "default",
									},
									StatusFeedbacks: v1.StatusFeedbackResult{
										Values: []v1.FeedbackValue{
											{
												Name: "Replicas",
												Value: v1.FieldValue{
													Integer: boolPtr(1),
												},
											},
											{
												Name: "ReadyReplicas",
												Value: v1.FieldValue{
													Integer: boolPtr(2),
												},
											},
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   v1.WorkAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedErr:              nil,
			expectedHealthCheckMode:  addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{},
		},
		{
			name: "Health check mode is deployment availability and WorkProber check pass",
			testAddon: &healthCheckTestAgent{name: "test",
				health: &agent.HealthProber{Type: agent.HealthProberTypeDeploymentAvailability},
			},
			addon: addontesting.NewAddonWithConditions("test", "cluster1", manifestAppliedCondition),
			existingWork: []runtime.Object{
				&v1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "addon-test-deploy-01",
						Namespace: "cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/addon-name": "test",
						},
					},
					Spec: v1.ManifestWorkSpec{},
					Status: v1.ManifestWorkStatus{
						ResourceStatus: v1.ManifestResourceStatus{
							Manifests: []v1.ManifestCondition{
								{
									ResourceMeta: v1.ManifestResourceMeta{
										Ordinal:   0,
										Group:     "apps",
										Version:   "",
										Kind:      "",
										Resource:  "deployments",
										Name:      "test-deployment",
										Namespace: "default",
									},
									StatusFeedbacks: v1.StatusFeedbackResult{
										Values: []v1.FeedbackValue{
											{
												Name: "Replicas",
												Value: v1.FieldValue{
													Integer: boolPtr(1),
												},
											},
											{
												Name: "ReadyReplicas",
												Value: v1.FieldValue{
													Integer: boolPtr(2),
												},
											},
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   v1.WorkAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedErr:             nil,
			expectedHealthCheckMode: addonapiv1alpha1.HealthCheckModeCustomized,
			expectAvailableCondition: metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1alpha1.AddonAvailableReasonProbeAvailable,
				Message: "test add-on is available.",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeWorkClient := fakework.NewSimpleClientset(c.existingWork...)
			workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
			err := workInformerFactory.Work().V1().ManifestWorks().Informer().AddIndexers(
				cache.Indexers{
					index.ManifestWorkByAddon:           index.IndexManifestWorkByAddon,
					index.ManifestWorkByHostedAddon:     index.IndexManifestWorkByHostedAddon,
					index.ManifestWorkHookByHostedAddon: index.IndexManifestWorkHookByHostedAddon,
				},
			)

			if err != nil {
				t.Fatal(err)
			}
			for _, obj := range c.existingWork {
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			addonDeploymentController := addonDeployController{
				workIndexer: workInformerFactory.Work().V1().ManifestWorks().Informer().GetIndexer(),
				agentAddons: map[string]agent.AgentAddon{c.testAddon.name: c.testAddon},
			}

			healthCheckSyncer := healthCheckSyncer{
				getWorkByAddon: addonDeploymentController.getWorksByAddonFn(index.ManifestWorkByAddon),
				agentAddon:     addonDeploymentController.agentAddons[c.testAddon.name],
			}

			addon, err := healthCheckSyncer.sync(context.TODO(), addontesting.NewFakeSyncContext(t), c.cluster, c.addon)
			if (err == nil && c.expectedErr != nil) || (err != nil && c.expectedErr == nil) {
				t.Errorf("name %s, expected err %v, but got %v", c.name, c.expectedErr, err)
			} else if err != nil && c.expectedErr != nil && err.Error() != c.expectedErr.Error() {
				t.Errorf("name %s, expected err %v, but got %v", c.name, c.expectedErr, err)
			}

			if !equality.Semantic.DeepEqual(addon.Status.HealthCheck.Mode, c.expectedHealthCheckMode) {
				t.Errorf("name %s, expected err %v, but got %v",
					c.name, addon.Status.HealthCheck.Mode, c.expectedHealthCheckMode)
			}

			if c.expectAvailableCondition.Type != "" {
				cond := meta.FindStatusCondition(addon.Status.Conditions, c.expectAvailableCondition.Type)
				if cond == nil {
					t.Fatalf("name %s, expected condition %v, but connot get", c.name, c.expectAvailableCondition.Type)
				}
				if cond.Status != c.expectAvailableCondition.Status {
					t.Errorf("name %s, expected condition status %v, but got %v",
						c.name, c.expectAvailableCondition.Status, cond.Status)
				}
				if cond.Reason != c.expectAvailableCondition.Reason {
					t.Errorf("name %s, expected condition reason %v, but got %v",
						c.name, c.expectAvailableCondition.Reason, cond.Reason)
				}
			} else {
				if meta.FindStatusCondition(addon.Status.Conditions,
					addonapiv1alpha1.ManagedClusterAddOnConditionAvailable) != nil {
					t.Errorf("name %s, expected condition not found", c.name)
				}
			}
		})
	}
}
