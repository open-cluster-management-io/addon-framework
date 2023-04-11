package managementaddonprogressing

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformersv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/index"
)

// managementAddonProgressingController reconciles instances of clustermanagementaddon the hub
// based to update related object and status condition.
type managementAddonProgressingController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	placementLister              clusterlisterv1beta1.PlacementLister
	placementDecisionLister      clusterlisterv1beta1.PlacementDecisionLister
}

func NewManagementAddonProgressingController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	placementInformer clusterinformersv1beta1.PlacementInformer,
	placementDecisionInformer clusterinformersv1beta1.PlacementDecisionInformer,
) factory.Controller {
	c := &managementAddonProgressingController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
	}

	return factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			accessor, _ := meta.Accessor(obj)
			return []string{accessor.GetName()}
		},
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithInformersQueueKeysFunc(index.ClusterManagementAddonByPlacementDecisionQueueKey(clusterManagementAddonInformers), placementDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(index.ClusterManagementAddonByPlacementQueueKey(clusterManagementAddonInformers), placementInformer.Informer()).
		WithSync(c.sync).ToController("management-addon-status-controller")

}

func (c *managementAddonProgressingController) sync(ctx context.Context, syncCtx factory.SyncContext, addonName string) error {
	klog.V(4).Infof("Reconciling addon %q", addonName)

	mgmtAddon, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	mgmtAddonCopy := mgmtAddon.DeepCopy()

	// update install progression Conditions, LastAppliedConfig and LastKnownGoodConfig.
	for i, installProgression := range mgmtAddonCopy.Status.InstallProgressions {
		// get install progression clusters
		clusters, err := c.getClustersByPlacement(installProgression.PlacementRef.Name, installProgression.PlacementRef.Namespace)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}

		// get addons per install progression
		var addons []addonv1alpha1.ManagedClusterAddOn
		for _, clusterName := range clusters {
			addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
			if errors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return err
			}
			addons = append(addons, *addon)
		}

		// go through addons and update condition per install progression
		isUpgrade := false
		isConfigurationUnsupported := false
		done := 0
		progressing := 0
		for _, addon := range addons {
			cond := meta.FindStatusCondition(addon.Status.Conditions, addonv1alpha1.ManagedClusterAddOnConditionProgressing)
			if cond == nil {
				continue
			}
			if cond.Reason == constants.ProgressingConfigurationUnsupported {
				isConfigurationUnsupported = true
				meta.SetStatusCondition(&mgmtAddonCopy.Status.InstallProgressions[i].Conditions, metav1.Condition{
					Type:    addonv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  constants.ProgressingConfigurationUnsupported,
					Message: fmt.Sprintf("%s/%s: %s", addon.Namespace, addonName, cond.Message),
				})
				break
			}
			switch cond.Reason {
			case constants.ProgressingInstalling:
				progressing += 1
			case constants.ProgressingInstallSucceed:
				done += 1
			case constants.ProgressingUpgrading:
				isUpgrade = true
				progressing += 1
			case constants.ProgressingUpgradeSucceed:
				isUpgrade = true
				done += 1
			}
		}

		if len(addons) > 0 && !isConfigurationUnsupported {
			setAddOnInstallProgressions(isUpgrade, progressing, done, len(clusters), &mgmtAddonCopy.Status.InstallProgressions[i])
		}
	}

	// update cma status
	return c.patchMgmtAddonStatus(ctx, mgmtAddonCopy, mgmtAddon)
}

func (c *managementAddonProgressingController) getClustersByPlacement(name, namespace string) ([]string, error) {
	var clusters []string
	if c.placementLister == nil || c.placementDecisionLister == nil {
		return clusters, nil
	}
	_, err := c.placementLister.Placements(namespace).Get(name)
	if err != nil {
		return clusters, err
	}

	decisionSelector := labels.SelectorFromSet(labels.Set{
		clusterv1beta1.PlacementLabel: name,
	})
	decisions, err := c.placementDecisionLister.PlacementDecisions(namespace).List(decisionSelector)
	if err != nil {
		return clusters, err
	}

	for _, d := range decisions {
		for _, sd := range d.Status.Decisions {
			clusters = append(clusters, sd.ClusterName)
		}
	}

	return clusters, nil
}

func (c *managementAddonProgressingController) patchMgmtAddonStatus(ctx context.Context, new, old *addonv1alpha1.ClusterManagementAddOn) error {
	if equality.Semantic.DeepEqual(new.Status, old.Status) {
		return nil
	}

	oldData, err := json.Marshal(&addonv1alpha1.ClusterManagementAddOn{
		Status: addonv1alpha1.ClusterManagementAddOnStatus{
			InstallProgressions: old.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonv1alpha1.ClusterManagementAddOnStatus{
			InstallProgressions: new.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching clustermanagementaddon %s status with %s", new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}

func setAddOnInstallProgressions(isUpgrade bool, progressing, done, total int, installProgression *addonv1alpha1.InstallProgression) {
	condition := metav1.Condition{
		Type: addonv1alpha1.ManagedClusterAddOnConditionProgressing,
	}
	if done != total {
		condition.Status = metav1.ConditionTrue
		if isUpgrade {
			condition.Reason = constants.ProgressingUpgrading
			condition.Message = fmt.Sprintf("%d/%d upgrading...", progressing+done, total)
		} else {
			condition.Reason = constants.ProgressingInstalling
			condition.Message = fmt.Sprintf("%d/%d installing...", progressing+done, total)
		}
	} else {
		for i, configRef := range installProgression.ConfigReferences {
			installProgression.ConfigReferences[i].LastAppliedConfig = configRef.DesiredConfig.DeepCopy()
			installProgression.ConfigReferences[i].LastKnownGoodConfig = configRef.DesiredConfig.DeepCopy()
		}
		condition.Status = metav1.ConditionFalse
		if isUpgrade {
			condition.Reason = constants.ProgressingUpgradeSucceed
			condition.Message = fmt.Sprintf("%d/%d upgrade completed with no errors.", done, total)
		} else {
			condition.Reason = constants.ProgressingInstallSucceed
			condition.Message = fmt.Sprintf("%d/%d install completed with no errors.", done, total)
		}
	}
	meta.SetStatusCondition(&installProgression.Conditions, condition)
}
