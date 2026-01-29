package utils

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	certificatesv1 "k8s.io/api/certificates/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/api/addon/v1alpha1"
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

func TestBindKubeClientClusterRole_PendingError(t *testing.T) {
	testCluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
	}

	tests := []struct {
		name        string
		addon       *v1alpha1.ManagedClusterAddOn
		expectError bool
	}{
		{
			name: "no registrations - returns pending error",
			addon: &v1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster"},
				Status: v1alpha1.ManagedClusterAddOnStatus{
					Registrations: []v1alpha1.RegistrationConfig{},
				},
			},
			expectError: true,
		},
		{
			name: "empty subject - returns pending error",
			addon: &v1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster"},
				Status: v1alpha1.ManagedClusterAddOnStatus{
					Registrations: []v1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject:    v1alpha1.Subject{}, // empty subject
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "valid subject - no error",
			addon: &v1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster"},
				Status: v1alpha1.ManagedClusterAddOnStatus{
					Registrations: []v1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: v1alpha1.Subject{
								User:   "system:serviceaccount:test:test-sa",
								Groups: []string{"system:authenticated"},
							},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeKubeClient := fake.NewSimpleClientset()
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "test-role"},
			}

			permissionConfigFn := NewRBACPermissionConfigBuilder(fakeKubeClient).
				BindKubeClientClusterRole(clusterRole).
				Build()

			err := permissionConfigFn(testCluster, tt.addon)

			if tt.expectError {
				assert.Error(t, err)
				var subjectErr *agent.SubjectNotReadyError
				assert.True(t, errors.As(err, &subjectErr), "error should be SubjectNotReadyError")
			} else {
				assert.NoError(t, err)
				// Verify binding was created
				binding, err := fakeKubeClient.RbacV1().ClusterRoleBindings().Get(context.TODO(), "test-role", metav1.GetOptions{})
				assert.NoError(t, err)
				assert.NotNil(t, binding)
				assert.Len(t, binding.Subjects, 2) // user + group
			}
		})
	}
}

func TestBindKubeClientRole_PendingError(t *testing.T) {
	testCluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
	}

	tests := []struct {
		name        string
		addon       *v1alpha1.ManagedClusterAddOn
		expectError bool
	}{
		{
			name: "no registrations - returns pending error",
			addon: &v1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster"},
				Status: v1alpha1.ManagedClusterAddOnStatus{
					Registrations: []v1alpha1.RegistrationConfig{},
				},
			},
			expectError: true,
		},
		{
			name: "empty subject - returns pending error",
			addon: &v1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster"},
				Status: v1alpha1.ManagedClusterAddOnStatus{
					Registrations: []v1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject:    v1alpha1.Subject{}, // empty subject
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "valid subject - no error",
			addon: &v1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster"},
				Status: v1alpha1.ManagedClusterAddOnStatus{
					Registrations: []v1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: v1alpha1.Subject{
								User:   "system:serviceaccount:test:test-sa",
								Groups: []string{"system:authenticated"},
							},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeKubeClient := fake.NewSimpleClientset()
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "test-role"},
			}

			permissionConfigFn := NewRBACPermissionConfigBuilder(fakeKubeClient).
				BindKubeClientRole(role).
				Build()

			err := permissionConfigFn(testCluster, tt.addon)

			if tt.expectError {
				assert.Error(t, err)
				var subjectErr *agent.SubjectNotReadyError
				assert.True(t, errors.As(err, &subjectErr), "error should be SubjectNotReadyError")
			} else {
				assert.NoError(t, err)
				// Verify binding was created
				binding, err := fakeKubeClient.RbacV1().RoleBindings(testCluster.Name).Get(context.TODO(), "test-role", metav1.GetOptions{})
				assert.NoError(t, err)
				assert.NotNil(t, binding)
				assert.Len(t, binding.Subjects, 2) // user + group
			}
		})
	}
}
