package rbac

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func AddonRBAC(kubeConfig *rest.Config) agent.PermissionConfigFunc {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-cluster-management:helloworld:agent",
		},
		Rules: []rbacv1.PolicyRule{
			{Verbs: []string{"get", "list", "watch"}, Resources: []string{"configmaps"}, APIGroups: []string{""}},
			{Verbs: []string{"get", "list", "watch"}, Resources: []string{"managedclusteraddons"}, APIGroups: []string{"addon.open-cluster-management.io"}},
		},
	}

	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		kubeclient, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			return err
		}

		permissionConfig := utils.NewRBACPermissionConfigBuilder(kubeclient).
			BindKubeClientRole(role).
			Build()

		return permissionConfig(cluster, addon)
	}
}
