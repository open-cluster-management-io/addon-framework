package managementaddon

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

const (
	controllerName = "management-addon-controller"
)

// clusterManagementAddonController reconciles cma on the hub.
type clusterManagementAddonController struct {
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	agentAddons                  map[string]agent.AgentAddon
	addonFilterFunc              factory.EventFilterFunc
}

func NewManagementAddonController(
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	syncCtx := factory.NewSyncContext(controllerName)

	c := &clusterManagementAddonController{
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		agentAddons:                  agentAddons,
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				return []string{key}
			},
			c.addonFilterFunc, clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController(controllerName)
}

func (c *clusterManagementAddonController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	_, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		// addon cloud be deleted, ignore
		return nil
	}
	if err != nil {
		return err
	}

	addon := c.agentAddons[cma.GetName()]
	if addon.GetAgentAddonOptions().InstallStrategy == nil {
		return nil
	}

	// If the addon defines install strategy via WithInstallStrategy(), force add annotation "addon.open-cluster-management.io/lifecycle: self" to cma.
	// The annotation with value "self" will be removed when remove WithInstallStrategy() in addon-framework.
	// The migration plan refer to https://github.com/open-cluster-management-io/ocm/issues/355.
	cmaCopy := cma.DeepCopy()
	if cmaCopy.Annotations == nil {
		cmaCopy.Annotations = map[string]string{}
	}
	cmaCopy.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey] = addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue

	err = c.patchMgmtAddonAnnotations(ctx, cmaCopy, cma)
	return err
}

func (c *clusterManagementAddonController) patchMgmtAddonAnnotations(ctx context.Context, new, old *addonapiv1alpha1.ClusterManagementAddOn) error {
	if equality.Semantic.DeepEqual(new.Annotations, old.Annotations) {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: old.Annotations,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonapiv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
			Annotations:     new.Annotations,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching clustermanagementaddon %s annotations with %s", new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}
