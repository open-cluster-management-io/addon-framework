package managementaddonprogressing

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		placements             []runtime.Object
		placementDecisions     []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                "no managedClusteraddon",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "no placement",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "update clustermanagementaddon status with condition Progressing installing",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:   addonv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status: metav1.ConditionFalse,
					Reason: constants.ProgressingInstalling,
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig != nil {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != constants.ProgressingInstalling {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Reason)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/2 installing..." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Message)
				}
			},
		},
		{
			name:    "update clustermanagementaddon status with condition Progressing install succeed",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:   addonv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status: metav1.ConditionFalse,
					Reason: constants.ProgressingInstallSucceed,
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if !apiequality.Semantic.DeepEqual(cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig, cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if !apiequality.Semantic.DeepEqual(cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig, cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != constants.ProgressingInstallSucceed {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/1 install completed with no errors." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name:    "update clustermanagementaddon status with condition Progressing upgrading",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:   addonv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status: metav1.ConditionFalse,
					Reason: constants.ProgressingUpgrading,
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig != nil {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != constants.ProgressingUpgrading {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/2 upgrading..." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name:    "update clustermanagementaddon status with condition Progressing upgrade succeed",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:   addonv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status: metav1.ConditionFalse,
					Reason: constants.ProgressingUpgradeSucceed,
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if !apiequality.Semantic.DeepEqual(cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig, cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if !apiequality.Semantic.DeepEqual(cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig, cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != constants.ProgressingUpgradeSucceed {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/1 upgrade completed with no errors." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name:    "update clustermanagementaddon status with condition Progressing ConfigurationUnsupported",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  constants.ProgressingConfigurationUnsupported,
					Message: fmt.Sprintf("Configuration with gvr core/foo is not supported for this addon"),
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &v1alpha1.ConfigSpecHash{
								ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig != nil {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != constants.ProgressingConfigurationUnsupported {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "cluster1/test: Configuration with gvr core/foo is not supported for this addon" {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append(c.clusterManagementAddon, c.managedClusteraddon...)
			clusterObj := append(c.placements, c.placementDecisions...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.placements {
				if err := clusterInformers.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.placementDecisions {
				if err := clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := managementAddonProgressingController{
				addonClient:                  fakeAddonClient,
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				placementLister:              clusterInformers.Cluster().V1beta1().Placements().Lister(),
				placementDecisionLister:      clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err := controller.sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
