package utils

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
)

func TestPermissionBuilder(t *testing.T) {
	testCluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
	}
	testAddon := &v1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test-addon"},
	}
	creatingClusterRole1 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo1", UID: "foo1"},
	}
	creatingRole1 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCluster.Name, Name: "foo1", UID: "foo1"},
	}
	existingClusterRole2 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo2", UID: "foo2"},
	}
	existingRole2 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCluster.Name, Name: "foo2", UID: "foo2"},
	}
	updatingClusterRole2 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo2", UID: "foo2"},
	}
	updatingRole2 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCluster.Name, Name: "foo2", UID: "foo2"},
	}
	fakeKubeClient := fake.NewSimpleClientset(existingClusterRole2, existingRole2)
	permissionConfigFn := NewRBACPermissionConfigBuilder(fakeKubeClient).
		WithStaticClusterRole(creatingClusterRole1).
		WithStaticClusterRole(updatingClusterRole2).
		WithStaticRole(creatingRole1).
		WithStaticRole(updatingRole2).
		Build()

	assert.NoError(t, permissionConfigFn(testCluster, testAddon))

	actualClusterRole1, err := fakeKubeClient.RbacV1().ClusterRoles().Get(context.TODO(), "foo1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, creatingClusterRole1.UID, actualClusterRole1.UID)
	actualClusterRole2, err := fakeKubeClient.RbacV1().ClusterRoles().Get(context.TODO(), "foo2", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, updatingClusterRole2.UID, actualClusterRole2.UID)
	actualRole1, err := fakeKubeClient.RbacV1().Roles(testCluster.Name).Get(context.TODO(), "foo1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, creatingRole1.UID, actualRole1.UID)
	actualRole2, err := fakeKubeClient.RbacV1().Roles(testCluster.Name).Get(context.TODO(), "foo2", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, updatingRole2.UID, actualRole2.UID)
}

func TestTemplatePermissionConfigFunc(t *testing.T) {
	cases := []struct {
		name                   string
		agentName              string
		cluster                *clusterv1.ManagedCluster
		addon                  *addonapiv1alpha1.ManagedClusterAddOn
		template               *addonapiv1alpha1.AddOnTemplate
		expectedErr            error
		validatePermissionFunc func(*testing.T, kubernetes.Interface)
	}{
		{
			name:      "kubeclient current cluster binding",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingCurrentCluster,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "Role",
									Name:     "test",
								},
							},
						},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
			validatePermissionFunc: func(t *testing.T, kubeClient kubernetes.Interface) {
				rb, err := kubeClient.RbacV1().RoleBindings("cluster1").Get(context.TODO(),
					fmt.Sprintf("open-cluster-management:%s:%s:agent", "addon1", strings.ToLower("Role")),
					metav1.GetOptions{},
				)
				if err != nil {
					t.Errorf("failed to get rolebinding: %v", err)
				}

				if rb.RoleRef.Name != "test" {
					t.Errorf("expected rolebinding %s, got %s", "test", rb.RoleRef.Name)
				}
				if len(rb.OwnerReferences) != 1 {
					t.Errorf("expected rolebinding to have 1 owner reference, got %d", len(rb.OwnerReferences))
				}
				if rb.OwnerReferences[0].Kind != "ManagedClusterAddOn" {
					t.Errorf("expected rolebinding owner reference kind to be ManagedClusterAddOn, got %s",
						rb.OwnerReferences[0].Kind)
				}
				if rb.OwnerReferences[0].Name != "addon1" {
					t.Errorf("expected rolebinding owner reference name to be addon1, got %s",
						rb.OwnerReferences[0].Name)
				}
			},
		},
		{
			name:      "kubeclient single namespace binding",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
								},
							},
						},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
			validatePermissionFunc: func(t *testing.T, kubeClient kubernetes.Interface) {
				rb, err := kubeClient.RbacV1().RoleBindings("test").Get(context.TODO(),
					fmt.Sprintf("open-cluster-management:%s:%s:agent", "addon1", strings.ToLower("ClusterRole")),
					metav1.GetOptions{},
				)
				if err != nil {
					t.Errorf("failed to get rolebinding: %v", err)
				}

				if rb.RoleRef.Name != "test" {
					t.Errorf("expected rolebinding %s, got %s", "test", rb.RoleRef.Name)
				}
				if len(rb.OwnerReferences) != 1 {
					t.Errorf("expected rolebinding to have 1 owner reference, got %d", len(rb.OwnerReferences))
				}
				if rb.OwnerReferences[0].Kind != "ManagedCluster" {
					t.Errorf("expected rolebinding owner reference kind to be ManagedCluster, got %s",
						rb.OwnerReferences[0].Kind)
				}
				if rb.OwnerReferences[0].Name != "cluster1" {
					t.Errorf("expected rolebinding owner reference name to be cluster1, got %s",
						rb.OwnerReferences[0].Name)
				}
			},
		},
		{
			name:      "customsigner",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Namespace: "ns1",
							Name:      "name1"},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
		},
	}
	for _, c := range cases {
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		hubKubeClient := fakekube.NewSimpleClientset()
		f := TemplatePermissionConfigFunc(c.addon.Name, addonClient, hubKubeClient)
		err := f(c.cluster, c.addon)
		if err != c.expectedErr {
			t.Errorf("expected registrationConfigs %v, but got %v", c.expectedErr, err)
		}
		if c.validatePermissionFunc != nil {
			c.validatePermissionFunc(t, hubKubeClient)
		}
	}
}
