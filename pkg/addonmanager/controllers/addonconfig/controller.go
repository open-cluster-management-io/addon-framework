package addonconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	configRef  addonapiv1alpha1.ConfigReference
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
		return nil
	case err != nil:
		return err
	}

	//TODO handle cluster deleteing and addon deleteing to stop the addon config controller

	// start a config conroller
	c.startConfigController(ctx, addon.DeepCopy())
	return nil
}

func (c *addonConfigController) startConfigController(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	if len(addon.Status.ConfigReference.ConfigGVR.Group) == 0 ||
		len(addon.Status.ConfigReference.Config.Name) == 0 {
		// addon config reference removed, stop the controller
		c.stopConfigController(addon)
		return
	}

	ctrlKey := configCtrlKey(addon)
	ctrlName := fmt.Sprintf("addon-%s-config-controller", ctrlKey)

	if ctrlCtx, ok := c.configControlllers[ctrlKey]; ok {
		if equality.Semantic.DeepEqual(ctrlCtx.configRef, addon.Status.ConfigReference) {
			return
		}

		// addon config reference changed, stop the old controller
		c.stopConfigController(addon)
	}

	configInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		c.dynamicClient,
		10*time.Minute,
		addon.Status.ConfigReference.Config.Namespace,
		func(listOptions *metav1.ListOptions) {
			listOptions.FieldSelector = fields.OneTermEqualSelector(
				"metadata.name",
				addon.Status.ConfigReference.Config.Name,
			).String()
		},
	)

	configInformer := configInformerFactory.ForResource(toConfigGVR(addon.Status.ConfigReference.ConfigGVR))

	configCtrlCtx, cancel := context.WithCancel(ctx)
	c.configControlllers[ctrlKey] = configSyncContext{
		configRef:  addon.Status.ConfigReference,
		cancelFunc: cancel,
	}

	configCtrl := &configController{
		addonName:      addon.Name,
		addonNamespace: addon.Namespace,
		addonClient:    c.addonClient,
		configLister:   configInformer.Lister(),
		addonLister:    c.addonLister,
		eventRecorder:  c.eventRecorder.WithComponentSuffix(ctrlName),
	}
	go factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		},
		configInformer.Informer(),
	).WithSync(configCtrl.sync).ToController(ctrlName, c.eventRecorder).Run(configCtrlCtx, 1)

	go configInformerFactory.Start(configCtrlCtx.Done())
}

func (c *addonConfigController) stopConfigController(addon *addonapiv1alpha1.ManagedClusterAddOn) {
	ctrlKey := configCtrlKey(addon)

	klog.Infof("Stop contorller %q", ctrlKey)

	ctrlCtx, ok := c.configControlllers[ctrlKey]
	if !ok {
		return
	}

	if ctrlCtx.cancelFunc != nil {
		ctrlCtx.cancelFunc()
	}

	delete(c.configControlllers, ctrlKey)
}

// configController reconciles the addon config on the hub.
type configController struct {
	addonName      string
	addonNamespace string
	addonClient    addonv1alpha1client.Interface
	configLister   cache.GenericLister
	addonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	eventRecorder  events.Recorder
}

func (c *configController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	configName := syncCtx.QueueKey()
	klog.Infof("Reconcil the addon %s config %s", c.addonName, configName)

	config, err := c.configLister.Get(configName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	addon, err := c.addonLister.ManagedClusterAddOns(c.addonNamespace).Get(c.addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	addon = addon.DeepCopy()

	generation, err := getConfigGeneration(config)
	if err != nil {
		return err
	}

	// TODO need handle if there is no gneration in the object
	//addon.Status.ConfigReference.LastObservedGeneration = addon.Status.ConfigReference.LastObservedGeneration + 1

	addon.Status.ConfigReference.LastObservedGeneration = generation

	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(c.addonNamespace).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
	return err
}

func toConfigGVR(gvr addonapiv1alpha1.ConfigGVR) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    gvr.Group,
		Version:  gvr.Version,
		Resource: gvr.Resource,
	}
}

func configCtrlKey(addon *addonapiv1alpha1.ManagedClusterAddOn) string {
	return fmt.Sprintf("%s-%s", addon.Namespace, addon.Name)
}

func getConfigGeneration(config runtime.Object) (int64, error) {
	unstructuredConifg, err := runtime.DefaultUnstructuredConverter.ToUnstructured(config)
	if err != nil {
		return 0, err
	}

	generation, found, err := unstructured.NestedInt64(unstructuredConifg, "metadata", "generation")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}

	return generation, nil
}
