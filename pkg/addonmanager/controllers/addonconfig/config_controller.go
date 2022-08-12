package addonconfig

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

// configController reconciles the addon config on the hub.
type configController struct {
	addonName      string
	addonNamespace string
	addonClient    addonv1alpha1client.Interface
	configLister   cache.GenericLister
	addonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	eventRecorder  events.Recorder
}

func newConfigController(
	addon *addonv1alpha1.ManagedClusterAddOn,
	addonClient addonv1alpha1client.Interface,
	addonLister addonlisterv1alpha1.ManagedClusterAddOnLister,
	configInformer informers.GenericInformer,
	eventRecorder events.Recorder,
) factory.Controller {
	ctrlName := fmt.Sprintf("addon-config-%s-%s-controller", addon.Namespace, addon.Name)

	configCtrl := &configController{
		addonName:      addon.Name,
		addonNamespace: addon.Namespace,
		addonClient:    addonClient,
		configLister:   configInformer.Lister(),
		addonLister:    addonLister,
		eventRecorder:  eventRecorder.WithComponentSuffix(ctrlName),
	}

	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		},
		configInformer.Informer(),
	).WithSync(configCtrl.sync).ToController(ctrlName, eventRecorder)
}

func (c *configController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.Infof("Reconciling the addon %s config %s", c.addonName, key)

	var err error
	namespace, configName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	var config runtime.Object
	if namespace == "" {
		config, err = c.configLister.Get(configName)
	} else {
		config, err = c.configLister.ByNamespace(namespace).Get(configName)
	}
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

	if generation == 0 {
		// if the object does not have the generation field, we just added the last observed generation
		addon.Status.ConfigReference.LastObservedGeneration = addon.Status.ConfigReference.LastObservedGeneration + 1
	} else {
		addon.Status.ConfigReference.LastObservedGeneration = generation
	}

	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(c.addonNamespace).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
	return err
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
