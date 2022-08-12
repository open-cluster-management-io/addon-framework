package utils

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

type UpdateManagedClusterAddOnStatusFunc func(status *addonapiv1alpha1.ManagedClusterAddOnStatus) error

func MergeRelatedObjects(modified *bool, objs *[]addonapiv1alpha1.ObjectReference, obj addonapiv1alpha1.ObjectReference) {
	if *objs == nil {
		*objs = []addonapiv1alpha1.ObjectReference{}
	}

	for _, o := range *objs {
		if o.Group == obj.Group && o.Resource == obj.Resource && o.Name == obj.Name && o.Namespace == obj.Namespace {
			return
		}
	}

	*objs = append(*objs, obj)
	*modified = true
}

func UpdateManagedClusterAddOnStatus(
	ctx context.Context,
	client addonv1alpha1client.Interface,
	namespace, name string,
	updateFuncs ...UpdateManagedClusterAddOnStatusFunc) (*addonapiv1alpha1.ManagedClusterAddOnStatus, bool, error) {
	updated := false
	var updatedManagedClusterStatus *addonapiv1alpha1.ManagedClusterAddOnStatus

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		addOn, err := client.AddonV1alpha1().ManagedClusterAddOns(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &addOn.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedManagedClusterStatus = newStatus
			return nil
		}

		oldData, err := json.Marshal(addonapiv1alpha1.ManagedClusterAddOn{
			Status: *oldStatus,
		})

		if err != nil {
			return fmt.Errorf("failed to Marshal old data for cluster status %s: %w", addOn.Name, err)
		}

		newData, err := json.Marshal(addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				UID:             addOn.UID,
				ResourceVersion: addOn.ResourceVersion,
			}, // to ensure they appear in the patch as preconditions
			Status: *newStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to Marshal new data for cluster status %s: %w", addOn.Name, err)
		}

		patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
		if err != nil {
			return fmt.Errorf("failed to create patch for cluster %s: %w", addOn.Name, err)
		}

		updatedManagedCluster, err := client.AddonV1alpha1().ManagedClusterAddOns(namespace).Patch(
			ctx,
			name,
			types.MergePatchType,
			patchBytes,
			metav1.PatchOptions{},
			"status",
		)

		updatedManagedClusterStatus = &updatedManagedCluster.Status
		updated = err == nil
		return err
	})

	return updatedManagedClusterStatus, updated, err
}

func UpdateManagedClusterAddOnConditionFn(cond metav1.Condition) UpdateManagedClusterAddOnStatusFunc {
	return func(oldStatus *addonapiv1alpha1.ManagedClusterAddOnStatus) error {
		meta.SetStatusCondition(&oldStatus.Conditions, cond)
		return nil
	}
}
