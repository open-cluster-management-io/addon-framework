package registration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type testAgent struct {
	name                  string
	namespace             string
	registrations         []addonapiv1beta1.RegistrationConfig
	agentInstallNamespace func(addon *addonapiv1beta1.ManagedClusterAddOn) (string, error)
	permissionConfig      agent.PermissionConfigFunc
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return []runtime.Object{}, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	if len(t.registrations) == 0 {
		return agent.AgentAddonOptions{
			AddonName: t.name,
		}
	}
	agentOption := agent.AgentAddonOptions{
		AddonName: t.name,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) ([]addonapiv1beta1.RegistrationConfig, error) {
				return t.registrations, nil
			},
			PermissionConfig: t.permissionConfig,
			Namespace:        t.namespace,
		},
	}

	if t.agentInstallNamespace != nil {
		agentOption.Registration.AgentInstallNamespace = t.agentInstallNamespace
	}
	return agentOption
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		addon                []runtime.Object
		cluster              []runtime.Object
		testaddon            *testAgent
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		expectError          bool
	}{
		{
			name:                 "no cluster",
			addon:                []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster:              []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1beta1.RegistrationConfig{}},
		},
		{
			name:                 "no addon",
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1beta1.RegistrationConfig{}},
		},
		{
			name:    "no registrations",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
				Kind: "ClusterManagementAddOn",
				Name: "test"})},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(addOn.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied) {
					t.Errorf("Unexpected status condition patch, got %s", string(actual))
				}
			},
			testaddon: &testAgent{name: "test", registrations: []addonapiv1beta1.RegistrationConfig{}},
		},
		{
			name:    "no owner",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					return addon
				}(),
			},
			testaddon: &testAgent{name: "test", namespace: "default", registrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: "test",
					},
				},
			}},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "with registrations",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].Type != addonapiv1beta1.CustomSigner || addOn.Status.Registrations[0].CustomSigner.SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "default" {
					t.Errorf("Namespace in status is not correct")
				}
			},
			testaddon: &testAgent{name: "test", namespace: "default", registrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: "test",
					},
				},
			}},
		},
		{
			name:    "with registrations and override namespace",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					addon.Annotations = map[string]string{addonapiv1beta1.InstallNamespaceAnnotation: "default2"}
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].Type != addonapiv1beta1.CustomSigner || addOn.Status.Registrations[0].CustomSigner.SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "default" {
					t.Errorf("Namespace %s in status is not correct, expected default", addOn.Status.Namespace)
				}
			},
			testaddon: &testAgent{name: "test", namespace: "default", registrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: "test",
					},
				},
			}},
		},
		{
			name:    "with registrations and override namespace by agentInstallNamespace",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					addon.Annotations = map[string]string{addonapiv1beta1.InstallNamespaceAnnotation: "default2"}
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].Type != addonapiv1beta1.CustomSigner || addOn.Status.Registrations[0].CustomSigner.SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "default3" {
					t.Errorf("Namespace %s in status is not correct default3", addOn.Status.Namespace)
				}
			},
			testaddon: &testAgent{name: "test", namespace: "default",
				registrations: []addonapiv1beta1.RegistrationConfig{{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: "test",
					},
				}},
				agentInstallNamespace: func(addon *addonapiv1beta1.ManagedClusterAddOn) (string, error) {
					return "default3", nil
				},
			},
		},
		{
			name:    "default namespace when no namespace is specified",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].Type != addonapiv1beta1.CustomSigner || addOn.Status.Registrations[0].CustomSigner.SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "open-cluster-management-agent-addon" {
					t.Errorf("Namespace %s in status is not correct, expected open-cluster-management-agent-addon", addOn.Status.Namespace)
				}
			},
			testaddon: &testAgent{name: "test", namespace: "", registrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: "test",
					},
				},
			}},
		},
		{
			name:    "permission config pending - prerequisites not met",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied)
				if cond == nil {
					t.Errorf("Expected RegistrationApplied condition to be set")
				} else if cond.Status != metav1.ConditionFalse {
					t.Errorf("Expected RegistrationApplied condition status to be False, got %s", cond.Status)
				} else if cond.Reason != "PermissionConfigPending" {
					t.Errorf("Expected RegistrationApplied condition reason to be PermissionConfigPending, got %s", cond.Reason)
				} else if cond.Message != "registration subject not ready" {
					t.Errorf("Expected specific pending message, got: %s", cond.Message)
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1beta1.RegistrationConfig{
					{
						Type: addonapiv1beta1.CustomSigner,
						CustomSigner: &addonapiv1beta1.CustomSignerConfig{
							SignerName: "test",
						},
					},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
					return &agent.SubjectNotReadyError{}
				},
			},
		},
		{
			name:    "success - both permission and subject ready",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					addon.Status.Registrations = []addonapiv1beta1.RegistrationConfig{
						{
							Type:       addonapiv1beta1.KubeClient,
							KubeClient: &addonapiv1beta1.KubeClientConfig{Driver: "csr"},
						},
					}
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied)
				if cond == nil {
					t.Errorf("Expected RegistrationApplied condition to be set")
				} else if cond.Status != metav1.ConditionTrue {
					t.Errorf("Expected RegistrationApplied condition status to be True, got %s", cond.Status)
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1beta1.RegistrationConfig{
					{
						Type: addonapiv1beta1.KubeClient,
					},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
					return nil
				},
			},
		},
		{
			name:    "success - permission ready even with empty kubeClientDriver",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					// kubeClientDriver is empty
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(addOn.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied) {
					t.Errorf("Expected RegistrationApplied condition to be true")
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1beta1.RegistrationConfig{
					{
						Type: addonapiv1beta1.KubeClient,
					},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
					return nil
				},
			},
		},
		{
			name:    "permission not ready - takes precedence",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					// kubeClientDriver is empty
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied)
				if cond == nil {
					t.Errorf("Expected RegistrationApplied condition to be set")
				} else if cond.Status != metav1.ConditionFalse {
					t.Errorf("Expected RegistrationApplied condition status to be False, got %s", cond.Status)
				} else if cond.Reason != "PermissionConfigPending" {
					t.Errorf("Expected RegistrationApplied condition reason to be PermissionConfigPending, got %s", cond.Reason)
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1beta1.RegistrationConfig{
					{
						Type: addonapiv1beta1.KubeClient,
					},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
					return &agent.SubjectNotReadyError{}
				},
			},
		},
		{
			name:        "permission config failed - real error",
			cluster:     []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			expectError: true,
			addon: []runtime.Object{
				func() *addonapiv1beta1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1beta1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnRegistrationApplied)
				if cond == nil {
					t.Errorf("Expected RegistrationApplied condition to be set")
				} else if cond.Status != metav1.ConditionFalse {
					t.Errorf("Expected RegistrationApplied condition status to be False, got %s", cond.Status)
				} else if cond.Reason != addonapiv1beta1.RegistrationAppliedSetPermissionFailed {
					t.Errorf("Expected RegistrationApplied condition reason to be RegistrationAppliedSetPermissionFailed, got %s", cond.Reason)
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1beta1.RegistrationConfig{
					{
						Type: addonapiv1beta1.CustomSigner,
						CustomSigner: &addonapiv1beta1.CustomSignerConfig{
							SignerName: "test",
						},
					},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
					return fmt.Errorf("permission config failed")
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			for _, obj := range c.cluster {
				if err := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.addon {
				if err := addonInformers.Addon().V1beta1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonRegistrationController{
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1beta1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
			}

			for _, obj := range c.addon {
				addon := obj.(*addonapiv1beta1.ManagedClusterAddOn)
				key := fmt.Sprintf("%s/%s", addon.Namespace, addon.Name)
				syncContext := addontesting.NewFakeSyncContext(t)
				err := controller.sync(context.TODO(), syncContext, key)
				if c.expectError {
					if err == nil {
						t.Errorf("expected error when sync, but got nil")
					}
				} else {
					if err != nil {
						t.Errorf("expected no error when sync: %v", err)
					}
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
			}

		})
	}
}

func TestBuildRegistrationConfigs(t *testing.T) {
	customSignerName := "example.com/custom-signer"
	testSubject := addonapiv1beta1.Subject{
		BaseSubject: addonapiv1beta1.BaseSubject{
			User:   "test-user",
			Groups: []string{"test-group"},
		},
	}
	existingSubject := addonapiv1beta1.Subject{
		BaseSubject: addonapiv1beta1.BaseSubject{
			User:   "existing-user",
			Groups: []string{"existing-group"},
		},
	}

	tests := []struct {
		name                  string
		newConfigs            []addonapiv1beta1.RegistrationConfig
		existingRegistrations []addonapiv1beta1.RegistrationConfig
		expectedRegistrations []addonapiv1beta1.RegistrationConfig
	}{
		{
			name:                  "empty new configs",
			newConfigs:            []addonapiv1beta1.RegistrationConfig{},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{},
		},
		{
			name: "no existing registrations - kubeClientDriver empty",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						}, // use subject from newConfigs
					},
				},
			},
		},
		{
			name: "non-kubeclient type - use subject from newConfigs",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: customSignerName,
						Subject:    testSubject,
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: customSignerName,
						Subject:    testSubject, // from newConfigs
					},
				},
			},
		},
		{
			name: "kubeclient with csr driver - use subject from newConfigs",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "csr",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "csr",
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						}, // from newConfigs (not preserved because driver is csr)
					},
				},
			},
		},
		{
			name: "kubeclient with token driver - preserve existing subject",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "token",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   existingSubject.User,
								Groups: existingSubject.Groups,
							},
						},
					},
				},
			},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "token",
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   existingSubject.User,
								Groups: existingSubject.Groups,
							},
						}, // preserved from existing
					},
				},
			},
		},
		{
			name: "kubeclient with empty driver - use subject from newConfigs",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   existingSubject.User,
								Groups: existingSubject.Groups,
							},
						},
					},
				},
			},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						}, // use subject from newConfigs
					},
				},
			},
		},
		{
			name: "kubeclient with token driver and empty existing subject",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "token",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{}, // empty subject
					},
				},
			},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver:  "token",
						Subject: addonapiv1beta1.KubeClientSubject{}, // preserved empty subject
					},
				},
			},
		},
		{
			name: "kubeclient with token driver - no existing registration",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "token",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{}, // no existing registration
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver:  "token",
						Subject: addonapiv1beta1.KubeClientSubject{}, // cleared, waiting for agent to set
					},
				},
			},
		},
		{
			name: "kubeclient with csr driver - existing registrations ignored",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "csr",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   existingSubject.User,
								Groups: existingSubject.Groups,
							},
						},
					},
				},
			},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "csr",
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						}, // from newConfigs, existing ignored
					},
				},
			},
		},
		{
			name: "multiple configs - mixed signer types",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "token",
					},
				},
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: customSignerName,
						Subject:    testSubject,
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   existingSubject.User,
								Groups: existingSubject.Groups,
							},
						},
					},
				},
			},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "token",
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   existingSubject.User,
								Groups: existingSubject.Groups,
							},
						}, // preserved from existing for KubeClient
					},
				},
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: customSignerName,
						Subject:    testSubject, // from newConfigs for custom signer
					},
				},
			},
		},
		{
			name: "kubeclient with token driver - mismatched signer in existing",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   testSubject.User,
								Groups: testSubject.Groups,
							},
						},
						Driver: "token",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.CustomSigner,
					CustomSigner: &addonapiv1beta1.CustomSignerConfig{
						SignerName: customSignerName, // different signer
						Subject:    existingSubject,
					},
				},
			},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver:  "token",
						Subject: addonapiv1beta1.KubeClientSubject{}, // cleared, no matching existing

					},
				},
			},
		},
		{
			name: "kubeclient with csr driver - empty subject gets default",
			newConfigs: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Subject: addonapiv1beta1.KubeClientSubject{}, // empty subject
						Driver:  "csr",
					},
				},
			},
			existingRegistrations: []addonapiv1beta1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1beta1.RegistrationConfig{
				{
					Type: addonapiv1beta1.KubeClient,
					KubeClient: &addonapiv1beta1.KubeClientConfig{
						Driver: "csr",
						Subject: addonapiv1beta1.KubeClientSubject{
							BaseSubject: addonapiv1beta1.BaseSubject{
								User:   "system:open-cluster-management:cluster:cluster1:addon:test-addon:agent:test-addon",
								Groups: []string{"system:open-cluster-management:cluster:cluster1:addon:test-addon", "system:open-cluster-management:addon:test-addon"},
							},
						}, // default subject set

					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRegistrationConfigs(tt.newConfigs, tt.existingRegistrations, "cluster1", "test-addon")

			if len(result) != len(tt.expectedRegistrations) {
				t.Errorf("expected %d registrations, got %d", len(tt.expectedRegistrations), len(result))
				return
			}

			for i, expected := range tt.expectedRegistrations {
				if result[i].Type != expected.Type {
					t.Errorf("registration[%d].Type: expected %q, got %q", i, expected.Type, result[i].Type)
				}

				// Check based on type
				if expected.Type == addonapiv1beta1.KubeClient {
					if expected.KubeClient == nil || result[i].KubeClient == nil {
						if expected.KubeClient != result[i].KubeClient {
							t.Errorf("registration[%d].KubeClient: expected %v, got %v", i, expected.KubeClient, result[i].KubeClient)
						}
					} else {
						// Check Driver field
						if result[i].KubeClient.Driver != expected.KubeClient.Driver {
							t.Errorf("registration[%d].KubeClient.Driver: expected %q, got %q", i, expected.KubeClient.Driver, result[i].KubeClient.Driver)
						}
						expectedUser := expected.KubeClient.Subject.User
						resultUser := result[i].KubeClient.Subject.User
						if resultUser != expectedUser {
							t.Errorf("registration[%d].KubeClient.Subject.User: expected %q, got %q", i, expectedUser, resultUser)
						}
						expectedGroups := expected.KubeClient.Subject.Groups
						resultGroups := result[i].KubeClient.Subject.Groups
						if len(resultGroups) != len(expectedGroups) {
							t.Errorf("registration[%d].KubeClient.Subject.Groups length: expected %d, got %d", i, len(expectedGroups), len(resultGroups))
						} else {
							for j, group := range expectedGroups {
								if resultGroups[j] != group {
									t.Errorf("registration[%d].KubeClient.Subject.Groups[%d]: expected %q, got %q", i, j, group, resultGroups[j])
								}
							}
						}
					}
				} else if expected.Type == addonapiv1beta1.CustomSigner {
					if expected.CustomSigner == nil || result[i].CustomSigner == nil {
						if expected.CustomSigner != result[i].CustomSigner {
							t.Errorf("registration[%d].CustomSigner: expected %v, got %v", i, expected.CustomSigner, result[i].CustomSigner)
						}
					} else {
						if result[i].CustomSigner.SignerName != expected.CustomSigner.SignerName {
							t.Errorf("registration[%d].CustomSigner.SignerName: expected %q, got %q", i, expected.CustomSigner.SignerName, result[i].CustomSigner.SignerName)
						}
						expectedUser := expected.CustomSigner.Subject.User
						resultUser := result[i].CustomSigner.Subject.User
						if resultUser != expectedUser {
							t.Errorf("registration[%d].CustomSigner.Subject.User: expected %q, got %q", i, expectedUser, resultUser)
						}
						expectedGroups := expected.CustomSigner.Subject.Groups
						resultGroups := result[i].CustomSigner.Subject.Groups
						if len(resultGroups) != len(expectedGroups) {
							t.Errorf("registration[%d].CustomSigner.Subject.Groups length: expected %d, got %d", i, len(expectedGroups), len(resultGroups))
						} else {
							for j, group := range expectedGroups {
								if resultGroups[j] != group {
									t.Errorf("registration[%d].CustomSigner.Subject.Groups[%d]: expected %q, got %q", i, j, group, resultGroups[j])
								}
							}
						}
					}
				}
			}
		})
	}
}
