package clustermanagement

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
)

// clusterManagementController reconciles instances of managedclusteradd on the hub
// based on the clustermanagementaddon.
type clusterManagementController struct {
	addonClient                  addonv1alpha1client.Interface
	mapper                       meta.RESTMapper
	managedClusterLister         clusterlister.ManagedClusterLister
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	agentAddons                  map[string]agent.AgentAddon
	eventRecorder                events.Recorder
}

func NewClusterManagementController(
	addonClient addonv1alpha1client.Interface,
	mapper meta.RESTMapper,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterManagementController{
		addonClient:                  addonClient,
		mapper:                       mapper,
		managedClusterLister:         clusterInformers.Lister(),
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		agentAddons:                  agentAddons,
		eventRecorder:                recorder.WithComponentSuffix("cluster-management-addon-controller"),
	}

	return factory.New().WithFilteredEventsInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}

			return true
		},
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController("cluster-management-addon-controller", recorder)
}

func (c *clusterManagementController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon %q", key)

	namespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	clusterManagementAddon, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	if len(namespace) == 0 {
		return c.syncAllAddon(syncCtx, addonName)
	}

	owner := metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))

	addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(namespace).Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	addonCopy := addon.DeepCopy()

	// AddOwner if it does not exist
	modified := resourcemerge.BoolPtr(false)
	resourcemerge.MergeOwnerRefs(modified, &addonCopy.OwnerReferences, []metav1.OwnerReference{*owner})
	if *modified {
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Update(ctx, addonCopy, metav1.UpdateOptions{})
		return err
	}

	utils.MergeRelatedObjects(modified, &addonCopy.Status.RelatedObjects, addonapiv1alpha1.ObjectReference{
		Name:     clusterManagementAddon.Name,
		Resource: "clustermanagementaddons",
		Group:    addonapiv1alpha1.GroupVersion.Group,
	})

	if err := mergeConfigReference(c.mapper, modified, clusterManagementAddon, addonCopy); err != nil {
		_, _, err = utils.UpdateManagedClusterAddOnStatus(
			ctx,
			c.addonClient,
			namespace,
			addonName,
			utils.UpdateManagedClusterAddOnConditionFn(metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnCondtionConfigured,
				Status:  metav1.ConditionFalse,
				Reason:  "ConfigurationWrong",
				Message: err.Error(),
			}),
		)
		return err
	}

	if !*modified {
		return nil
	}

	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).UpdateStatus(ctx, addonCopy, metav1.UpdateOptions{})
	return err
}

func (c *clusterManagementController) syncAllAddon(syncCtx factory.SyncContext, addonName string) error {
	clusters, err := c.managedClusterLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(cluster.Name).Get(addonName)
		switch {
		case errors.IsNotFound(err):
			continue
		case err != nil:
			return err
		}

		key, _ := cache.MetaNamespaceKeyFunc(addon)
		syncCtx.Queue().Add(key)
	}

	return nil
}

func mergeConfigReference(mapper meta.RESTMapper,
	modified *bool,
	clusterManagementAddon *addonapiv1alpha1.ClusterManagementAddOn,
	addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	if clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource.Group == "" {
		// the ConfigGroupResource is not set, the config coordinates may be used
		mergeConfigCoordinates(modified, clusterManagementAddon, addon)
		return nil
	}

	configGVR, err := mapper.ResourceFor(schema.GroupVersionResource{
		Group:    clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource.Group,
		Resource: clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource.Resource,
	})
	if err != nil {
		return fmt.Errorf("the configuration resource type is not found, %v", err)
	}

	if clusterManagementAddon.Spec.AddOnConfiguration.DefaultConfig.Name == "" && addon.Spec.Config.Name == "" {
		return fmt.Errorf("the configuration name is required")
	}

	expectedConfigReference := addonapiv1alpha1.ConfigReference{
		ConfigGroupResource: clusterManagementAddon.Spec.AddOnConfiguration.ConfigGroupResource,
		ConfigReferent:      clusterManagementAddon.Spec.AddOnConfiguration.DefaultConfig,
		Version:             configGVR.Version,
	}

	if addon.Spec.Config.Namespace != "" {
		expectedConfigReference.ConfigReferent.Namespace = addon.Spec.Config.Namespace
	}

	if addon.Spec.Config.Name != "" {
		expectedConfigReference.ConfigReferent.Name = addon.Spec.Config.Name
	}

	actualConfigReference := addonapiv1alpha1.ConfigReference{
		ConfigGroupResource: addon.Status.ConfigReference.ConfigGroupResource,
		ConfigReferent:      addon.Status.ConfigReference.ConfigReferent,
		Version:             addon.Status.ConfigReference.Version,
	}

	if !equality.Semantic.DeepEqual(actualConfigReference, expectedConfigReference) {
		addon.Status.ConfigReference.ConfigGroupResource = expectedConfigReference.ConfigGroupResource
		addon.Status.ConfigReference.ConfigReferent = expectedConfigReference.ConfigReferent
		addon.Status.ConfigReference.Version = expectedConfigReference.Version
		*modified = true
	}

	return nil
}

// keep this function in the next few releases for compatibility
func mergeConfigCoordinates(modified *bool,
	clusterManagementAddon *addonapiv1alpha1.ClusterManagementAddOn, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	expectedCoordinate := addonapiv1alpha1.ConfigCoordinates{
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRDName: clusterManagementAddon.Spec.AddOnConfiguration.CRDName,
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRName: clusterManagementAddon.Spec.AddOnConfiguration.CRName,
	}
	actualCoordinate := addonapiv1alpha1.ConfigCoordinates{
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRDName: addon.Status.AddOnConfiguration.CRDName,
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRName: addon.Status.AddOnConfiguration.CRName,
	}

	if !equality.Semantic.DeepEqual(expectedCoordinate, actualCoordinate) {
		//lint:ignore SA1019 Ignore the deprecation warnings
		addon.Status.AddOnConfiguration.CRDName = expectedCoordinate.CRDName
		//lint:ignore SA1019 Ignore the deprecation warnings
		addon.Status.AddOnConfiguration.CRName = expectedCoordinate.CRName
		*modified = true
	}
}
