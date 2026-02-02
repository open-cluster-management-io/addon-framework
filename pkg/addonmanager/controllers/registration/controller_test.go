package registration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type testAgent struct {
	name                  string
	namespace             string
	registrations         []addonapiv1alpha1.RegistrationConfig
	agentInstallNamespace func(addon *addonapiv1alpha1.ManagedClusterAddOn) (string, error)
	permissionConfig      agent.PermissionConfigFunc
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
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
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]addonapiv1alpha1.RegistrationConfig, error) {
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
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{}},
		},
		{
			name:                 "no addon",
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{}},
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied) {
					t.Errorf("Unexpected status condition patch, got %s", string(actual))
				}
			},
			testaddon: &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{}},
		},
		{
			name:    "no owner",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					return addon
				}(),
			},
			testaddon: &testAgent{name: "test", namespace: "default", registrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "test",
				},
			}},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "with registrations",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "default" {
					t.Errorf("Namespace in status is not correct")
				}
			},
			testaddon: &testAgent{name: "test", namespace: "default", registrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "test",
				},
			}},
		},
		{
			name:    "with registrations and override namespace",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					addon.Spec.InstallNamespace = "default2"
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "default2" {
					t.Errorf("Namespace %s in status is not correct default2", addOn.Status.Namespace)
				}
			},
			testaddon: &testAgent{name: "test", namespace: "default", registrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "test",
				},
			}},
		},
		{
			name:    "with registrations and override namespace by agentInstallNamespace",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					addon.Spec.InstallNamespace = "default2"
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "default3" {
					t.Errorf("Namespace %s in status is not correct default3", addOn.Status.Namespace)
				}
			},
			testaddon: &testAgent{name: "test", namespace: "default",
				registrations: []addonapiv1alpha1.RegistrationConfig{{SignerName: "test"}},
				agentInstallNamespace: func(addon *addonapiv1alpha1.ManagedClusterAddOn) (string, error) {
					return "default3", nil
				},
			},
		},
		{
			name:    "default namespace when no namespace is specified",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if addOn.Status.Namespace != "open-cluster-management-agent-addon" {
					t.Errorf("Namespace %s in status is not correct, expected open-cluster-management-agent-addon", addOn.Status.Namespace)
				}
			},
			testaddon: &testAgent{name: "test", namespace: "", registrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "test",
				},
			}},
		},
		{
			name:    "permission config pending - prerequisites not met",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied)
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
				registrations: []addonapiv1alpha1.RegistrationConfig{
					{SignerName: "test"},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
					return &agent.SubjectNotReadyError{}
				},
			},
		},
		{
			name:    "success - both permission and subject ready",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", metav1.OwnerReference{
						Kind: "ClusterManagementAddOn",
						Name: "test",
					})
					addon.Status.KubeClientDriver = "csr"
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied)
				if cond == nil {
					t.Errorf("Expected RegistrationApplied condition to be set")
				} else if cond.Status != metav1.ConditionTrue {
					t.Errorf("Expected RegistrationApplied condition status to be True, got %s", cond.Status)
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1alpha1.RegistrationConfig{
					{SignerName: certificatesv1.KubeAPIServerClientSignerName},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
					return nil
				},
			},
		},
		{
			name:    "success - permission ready even with empty kubeClientDriver",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied) {
					t.Errorf("Expected RegistrationApplied condition to be true")
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1alpha1.RegistrationConfig{
					{SignerName: certificatesv1.KubeAPIServerClientSignerName},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
					return nil
				},
			},
		},
		{
			name:    "permission not ready - takes precedence",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied)
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
				registrations: []addonapiv1alpha1.RegistrationConfig{
					{SignerName: certificatesv1.KubeAPIServerClientSignerName},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
					return &agent.SubjectNotReadyError{}
				},
			},
		},
		{
			name:        "permission config failed - real error",
			cluster:     []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			expectError: true,
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
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
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied)
				if cond == nil {
					t.Errorf("Expected RegistrationApplied condition to be set")
				} else if cond.Status != metav1.ConditionFalse {
					t.Errorf("Expected RegistrationApplied condition status to be False, got %s", cond.Status)
				} else if cond.Reason != addonapiv1alpha1.RegistrationAppliedSetPermissionFailed {
					t.Errorf("Expected RegistrationApplied condition reason to be RegistrationAppliedSetPermissionFailed, got %s", cond.Reason)
				}
			},
			testaddon: &testAgent{
				name:      "test",
				namespace: "default",
				registrations: []addonapiv1alpha1.RegistrationConfig{
					{SignerName: "test"},
				},
				permissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
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
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonRegistrationController{
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
			}

			for _, obj := range c.addon {
				addon := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
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
	testSubject := addonapiv1alpha1.Subject{
		User:   "test-user",
		Groups: []string{"test-group"},
	}
	existingSubject := addonapiv1alpha1.Subject{
		User:   "existing-user",
		Groups: []string{"existing-group"},
	}

	tests := []struct {
		name                  string
		kubeClientDriver      string
		newConfigs            []addonapiv1alpha1.RegistrationConfig
		existingRegistrations []addonapiv1alpha1.RegistrationConfig
		expectedRegistrations []addonapiv1alpha1.RegistrationConfig
	}{
		{
			name:                  "empty new configs",
			kubeClientDriver:      "",
			newConfigs:            []addonapiv1alpha1.RegistrationConfig{},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{},
		},
		{
			name:             "no existing registrations - kubeClientDriver empty",
			kubeClientDriver: "",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject, // use subject from newConfigs
				},
			},
		},
		{
			name:             "non-kubeclient type - use subject from newConfigs",
			kubeClientDriver: "",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: customSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: customSignerName,
					Subject:    testSubject, // from newConfigs
				},
			},
		},
		{
			name:             "kubeclient with csr driver - use subject from newConfigs",
			kubeClientDriver: "csr",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject, // from newConfigs (not preserved because driver is csr)
				},
			},
		},
		{
			name:             "kubeclient with token driver - preserve existing subject",
			kubeClientDriver: "token",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    existingSubject,
				},
			},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    existingSubject, // preserved from existing
				},
			},
		},
		{
			name:             "kubeclient with empty driver - use subject from newConfigs",
			kubeClientDriver: "",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    existingSubject,
				},
			},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject, // use subject from newConfigs
				},
			},
		},
		{
			name:             "kubeclient with token driver and empty existing subject",
			kubeClientDriver: "token",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    addonapiv1alpha1.Subject{}, // empty subject
				},
			},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    addonapiv1alpha1.Subject{}, // preserved empty subject
				},
			},
		},
		{
			name:             "kubeclient with token driver - no existing registration",
			kubeClientDriver: "token",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{}, // no existing registration
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    addonapiv1alpha1.Subject{}, // cleared, waiting for agent to set
				},
			},
		},
		{
			name:             "kubeclient with csr driver - existing registrations ignored",
			kubeClientDriver: "csr",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    existingSubject,
				},
			},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject, // from newConfigs, existing ignored
				},
			},
		},
		{
			name:             "multiple configs - mixed signer types",
			kubeClientDriver: "token",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
				{
					SignerName: customSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    existingSubject,
				},
			},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    existingSubject, // preserved from existing for KubeClient
				},
				{
					SignerName: customSignerName,
					Subject:    testSubject, // from newConfigs for custom signer
				},
			},
		},
		{
			name:             "kubeclient with token driver - mismatched signer in existing",
			kubeClientDriver: "token",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    testSubject,
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: customSignerName, // different signer
					Subject:    existingSubject,
				},
			},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    addonapiv1alpha1.Subject{}, // cleared, no matching existing
				},
			},
		},
		{
			name:             "kubeclient with csr driver - empty subject gets default",
			kubeClientDriver: "csr",
			newConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject:    addonapiv1alpha1.Subject{}, // empty subject
				},
			},
			existingRegistrations: []addonapiv1alpha1.RegistrationConfig{},
			expectedRegistrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject: addonapiv1alpha1.Subject{
						User:   "system:open-cluster-management:cluster:cluster1:addon:test-addon:agent:test-addon",
						Groups: []string{"system:open-cluster-management:cluster:cluster1:addon:test-addon", "system:open-cluster-management:addon:test-addon"},
					}, // default subject set
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRegistrationConfigs(tt.newConfigs, tt.existingRegistrations, tt.kubeClientDriver, "cluster1", "test-addon")

			if len(result) != len(tt.expectedRegistrations) {
				t.Errorf("expected %d registrations, got %d", len(tt.expectedRegistrations), len(result))
				return
			}

			for i, expected := range tt.expectedRegistrations {
				if result[i].SignerName != expected.SignerName {
					t.Errorf("registration[%d].SignerName: expected %q, got %q", i, expected.SignerName, result[i].SignerName)
				}
				if result[i].Subject.User != expected.Subject.User {
					t.Errorf("registration[%d].Subject.User: expected %q, got %q", i, expected.Subject.User, result[i].Subject.User)
				}
				if len(result[i].Subject.Groups) != len(expected.Subject.Groups) {
					t.Errorf("registration[%d].Subject.Groups length: expected %d, got %d", i, len(expected.Subject.Groups), len(result[i].Subject.Groups))
				} else {
					for j, group := range expected.Subject.Groups {
						if result[i].Subject.Groups[j] != group {
							t.Errorf("registration[%d].Subject.Groups[%d]: expected %q, got %q", i, j, group, result[i].Subject.Groups[j])
						}
					}
				}
			}
		})
	}
}
