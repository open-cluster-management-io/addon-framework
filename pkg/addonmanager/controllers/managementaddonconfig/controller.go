package managementaddonconfig

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

const (
	controllerName                 = "management-addon-config-controller"
	byClusterManagementAddOnConfig = "by-cluster-management-addon-config"
)

type enqueueFunc func(obj interface{})

// clusterManagementAddonConfigController reconciles all interested addon config types (GroupVersionResource) on the hub.
type clusterManagementAddonConfigController struct {
	addonClient                   addonv1alpha1client.Interface
	clusterManagementAddonLister  addonlisterv1alpha1.ClusterManagementAddOnLister
	clusterManagementAddonIndexer cache.Indexer
	configListers                 map[string]dynamiclister.Lister
	queue                         workqueue.RateLimitingInterface
}

func NewManagementAddonConfigController(
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	configInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	configGVRs map[schema.GroupVersionResource]bool,
) factory.Controller {
	syncCtx := factory.NewSyncContext(controllerName)

	c := &clusterManagementAddonConfigController{
		addonClient:                   addonClient,
		clusterManagementAddonLister:  clusterManagementAddonInformers.Lister(),
		clusterManagementAddonIndexer: clusterManagementAddonInformers.Informer().GetIndexer(),
		configListers:                 map[string]dynamiclister.Lister{},
		queue:                         syncCtx.Queue(),
	}

	configInformers := c.buildConfigInformers(configInformerFactory, configGVRs)

	if err := clusterManagementAddonInformers.Informer().AddIndexers(cache.Indexers{byClusterManagementAddOnConfig: c.indexByConfig}); err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeysFunc(func(obj runtime.Object) []string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return []string{key}
		}, clusterManagementAddonInformers.Informer()).
		WithBareInformers(configInformers...).
		WithSync(c.sync).ToController(controllerName)
}

func (c *clusterManagementAddonConfigController) buildConfigInformers(
	configInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	configGVRs map[schema.GroupVersionResource]bool,
) []factory.Informer {
	configInformers := []factory.Informer{}
	for gvr := range configGVRs {
		genericInformer := configInformerFactory.ForResource(gvr)
		indexInformer := genericInformer.Informer()
		_, err := indexInformer.AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: c.enqueueClusterManagementAddOnsByConfig(gvr),
				UpdateFunc: func(oldObj, newObj interface{}) {
					c.enqueueClusterManagementAddOnsByConfig(gvr)(newObj)
				},
				DeleteFunc: c.enqueueClusterManagementAddOnsByConfig(gvr),
			},
		)
		if err != nil {
			utilruntime.HandleError(err)
		}
		configInformers = append(configInformers, indexInformer)
		c.configListers[toListerKey(gvr.Group, gvr.Resource)] = dynamiclister.New(indexInformer.GetIndexer(), gvr)
	}
	return configInformers
}

func (c *clusterManagementAddonConfigController) enqueueClusterManagementAddOnsByConfig(gvr schema.GroupVersionResource) enqueueFunc {
	return func(obj interface{}) {
		name, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error to get accessor of object: %v", obj))
			return
		}

		objs, err := c.clusterManagementAddonIndexer.ByIndex(byClusterManagementAddOnConfig, fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Resource, name))
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error to get addons: %v", err))
			return
		}

		for _, obj := range objs {
			if obj == nil {
				continue
			}

			cma := obj.(*addonapiv1alpha1.ClusterManagementAddOn)
			key, _ := cache.MetaNamespaceKeyFunc(cma)
			c.queue.Add(key)
		}
	}
}

func (c *clusterManagementAddonConfigController) indexByConfig(obj interface{}) ([]string, error) {
	cma, ok := obj.(*addonapiv1alpha1.ClusterManagementAddOn)
	if !ok {
		return nil, fmt.Errorf("obj is supposed to be a ClusterManagementAddOn, but is %T", obj)
	}

	configNames := sets.New[string]()
	for _, defaultConfigRef := range cma.Status.DefaultConfigReferences {
		if defaultConfigRef.DesiredConfig == nil || defaultConfigRef.DesiredConfig.Name == "" {
			// bad config reference, ignore
			continue
		}

		configNames.Insert(getDefaultConfigIndex(defaultConfigRef))
	}

	for _, installProgression := range cma.Status.InstallProgressions {
		for _, configReference := range installProgression.ConfigReferences {
			if configReference.DesiredConfig == nil || configReference.DesiredConfig.Name == "" {
				// bad config reference, ignore
				continue
			}

			configNames.Insert(getInstallConfigIndex(configReference))
		}
	}

	if configNames.Len() == 0 {
		// no configs , ignore
		return nil, nil
	}

	return configNames.UnsortedList(), nil
}

func (c *clusterManagementAddonConfigController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
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

	cmaCopy := cma.DeepCopy()

	if err := c.updateConfigSpecHash(cmaCopy); err != nil {
		return err
	}

	return c.patchConfigReferences(ctx, cma, cmaCopy)
}

func (c *clusterManagementAddonConfigController) updateConfigSpecHash(cma *addonapiv1alpha1.ClusterManagementAddOn) error {

	for i, defaultConfigReference := range cma.Status.DefaultConfigReferences {
		if defaultConfigReference.DesiredConfig == nil || defaultConfigReference.DesiredConfig.Name == "" {
			continue
		}

		var err error
		group := defaultConfigReference.Group
		resource := defaultConfigReference.Resource
		namespace := defaultConfigReference.DesiredConfig.Namespace
		name := defaultConfigReference.DesiredConfig.Name

		cma.Status.DefaultConfigReferences[i].DesiredConfig.SpecHash, err = c.getConfigSpecHash(group, resource, namespace, name)
		if err != nil {
			return nil
		}
	}

	for i, installProgression := range cma.Status.InstallProgressions {
		for j, configReference := range installProgression.ConfigReferences {
			if configReference.DesiredConfig == nil || configReference.DesiredConfig.Name == "" {
				continue
			}

			var err error
			group := configReference.Group
			resource := configReference.Resource
			namespace := configReference.DesiredConfig.Namespace
			name := configReference.DesiredConfig.Name

			cma.Status.InstallProgressions[i].ConfigReferences[j].DesiredConfig.SpecHash, err = c.getConfigSpecHash(group, resource, namespace, name)
			if err != nil {
				return nil
			}
		}
	}

	return nil
}

func (c *clusterManagementAddonConfigController) patchConfigReferences(ctx context.Context, old, new *addonapiv1alpha1.ClusterManagementAddOn) error {
	if equality.Semantic.DeepEqual(new.Status.DefaultConfigReferences, old.Status.DefaultConfigReferences) &&
		equality.Semantic.DeepEqual(new.Status.InstallProgressions, old.Status.InstallProgressions) {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ClusterManagementAddOn{
		Status: addonapiv1alpha1.ClusterManagementAddOnStatus{
			DefaultConfigReferences: old.Status.DefaultConfigReferences,
			InstallProgressions:     old.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonapiv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonapiv1alpha1.ClusterManagementAddOnStatus{
			DefaultConfigReferences: new.Status.DefaultConfigReferences,
			InstallProgressions:     new.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(4).Infof("Patching addon %s/%s config reference with %s", new.Namespace, new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Patch(
		ctx,
		new.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
		"status",
	)
	return err
}

func (c *clusterManagementAddonConfigController) getConfigSpecHash(group, resource, namespace, name string) (string, error) {
	lister, ok := c.configListers[toListerKey(group, resource)]
	if !ok {
		return "", nil
	}

	var config *unstructured.Unstructured
	var err error
	if namespace == "" {
		config, err = lister.Get(name)
	} else {
		config, err = lister.Namespace(namespace).Get(name)
	}

	if errors.IsNotFound(err) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return GetSpecHash(config)
}

func getDefaultConfigIndex(config addonapiv1alpha1.DefaultConfigReference) string {
	if config.DesiredConfig.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", config.Group, config.Resource, config.DesiredConfig.Namespace, config.DesiredConfig.Name)
	}

	return fmt.Sprintf("%s/%s/%s", config.Group, config.Resource, config.DesiredConfig.Name)
}

func getInstallConfigIndex(config addonapiv1alpha1.InstallConfigReference) string {
	if config.DesiredConfig.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", config.Group, config.Resource, config.DesiredConfig.Namespace, config.DesiredConfig.Name)
	}

	return fmt.Sprintf("%s/%s/%s", config.Group, config.Resource, config.DesiredConfig.Name)
}

func toListerKey(group, resource string) string {
	return fmt.Sprintf("%s/%s", group, resource)
}

func GetSpecHash(obj *unstructured.Unstructured) (string, error) {
	spec, ok := obj.Object["spec"]
	if !ok {
		return "", fmt.Errorf("object has no spec field")
	}

	specBytes, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(specBytes)

	return fmt.Sprintf("%x", hash), nil
}
