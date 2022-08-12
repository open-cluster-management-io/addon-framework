package addonconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

type configSyncContext struct {
	configMeta string
	cancelFunc context.CancelFunc
}

// addonConfigController reconciles the addon to start a controller to reconcile the config of addon on the hub.
type addonConfigController struct {
	dynamicClient      dynamic.Interface
	addonClient        addonv1alpha1client.Interface
	addonLister        addonlisterv1alpha1.ManagedClusterAddOnLister
	agentAddons        map[string]agent.AgentAddon
	configControlllers map[string]configSyncContext
	eventRecorder      events.Recorder
}

func NewAddonConfigController(
	dynamicClient dynamic.Interface,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &addonConfigController{
		dynamicClient:      dynamicClient,
		addonClient:        addonClient,
		addonLister:        addonInformers.Lister(),
		agentAddons:        agentAddons,
		configControlllers: make(map[string]configSyncContext),
		eventRecorder:      recorder.WithComponentSuffix("addon-config-controller"),
	}

	return factory.New().WithFilteredEventsInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetNamespace() + "/" + accessor.GetName()
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}

			return true
		},
		addonInformers.Informer()).
		WithSync(c.sync).ToController("addon-config-controller", recorder)
}

func (c *addonConfigController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon %q", key)

	namespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.addonLister.ManagedClusterAddOns(namespace).Get(addonName)
	switch {
	case errors.IsNotFound(err):
		// addon cloud be deleted, stop its config controller
		c.stopConfigController(key)
		return nil
	case err != nil:
		return err
	}

	if !addon.DeletionTimestamp.IsZero() {
		// addon is deleting, stop its config controller
		c.stopConfigController(key)
		return nil
	}

	if addon.Status.ConfigReference.Version == "" {
		// the addon config version is not discovered, we do not start its config controller,
		// and if the addon has a started config controller, stop it
		c.stopConfigController(key)
		return nil
	}

	if ctrlCtx, ok := c.configControlllers[key]; ok {
		if ctrlCtx.configMeta == toConfigMetaInfo(addon.Status.ConfigReference) {
			// the config is not changed, do nothing
			return nil
		}

		// addon config reference changed, stop the old controller firstly
		c.stopConfigController(key)
	}

	configControllerContext, cancel := context.WithCancel(ctx)
	c.configControlllers[key] = configSyncContext{
		configMeta: toConfigMetaInfo(addon.Status.ConfigReference),
		cancelFunc: cancel,
	}

	// start a config conroller
	c.startConfigController(configControllerContext, addon.DeepCopy())
	return nil
}

func (c *addonConfigController) startConfigController(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	configInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		c.dynamicClient,
		10*time.Minute,
		addon.Status.ConfigReference.Namespace,
		func(listOptions *metav1.ListOptions) {
			listOptions.FieldSelector = fields.OneTermEqualSelector(
				"metadata.name",
				addon.Status.ConfigReference.Name,
			).String()
		},
	)
	configInformer := configInformerFactory.ForResource(schema.GroupVersionResource{
		Group:    addon.Status.ConfigReference.Group,
		Version:  addon.Status.ConfigReference.Version,
		Resource: addon.Status.ConfigReference.Resource,
	})

	configController := newConfigController(
		addon,
		c.addonClient,
		c.addonLister,
		configInformer,
		c.eventRecorder,
	)

	go configController.Run(ctx, 1)

	go configInformerFactory.Start(ctx.Done())
}

func (c *addonConfigController) stopConfigController(key string) {
	ctrlCtx, ok := c.configControlllers[key]
	if !ok {
		return
	}

	klog.Infof("Stopping addon config contorller %q", key)

	if ctrlCtx.cancelFunc != nil {
		ctrlCtx.cancelFunc()
	}

	delete(c.configControlllers, key)
}

func toConfigMetaInfo(configRefer addonapiv1alpha1.ConfigReference) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s",
		configRefer.Group, configRefer.Version, configRefer.Resource, configRefer.Namespace, configRefer.Name)
}
