package garbagecollector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"k8s.io/apimachinery/pkg/util/wait"

	workv1 "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/work"
)

const (
	manifestWorkByOwner = "manifestWorkByOwner"
)

// monitor watches resource changes and enqueues the changes for processing.
type monitor struct {
	cache.Controller
	cache.SharedIndexInformer
}

type monitors map[schema.GroupVersionResource]*monitor

// dependent is a struct that holds the owner UID and the dependent manifestwork namespaced name.
type dependent struct {
	ownerUID       types.UID
	namespacedName types.NamespacedName
}

// The GarbageCollector controller monitors manifestworks and associated owner resources,
// managing the relationship between them and deleting manifestworks when all owner resources are removed.
// It currently supports only background deletion policy, lacking support for foreground and orphan policies.
// To prevent overwhelming the API server, the garbage collector operates with rate limiting.
// It is designed to run alongside the cloudevents source work client, eg. each addon controller
// utilizing the cloudevents driver should be accompanied by its own garbage collector.
type GarbageCollector struct {
	// workClient from cloudevents client builder
	workClient workv1.WorkV1Interface
	// workIndexer to index manifestwork by owner resources
	workIndexer cache.Indexer
	// workInformer from cloudevents client builder
	workInformer workv1informers.ManifestWorkInformer
	// metadataClient to operate on the owner resources
	metadataClient metadata.Interface
	// owner resource and filter pairs
	ownerGVRFilters map[schema.GroupVersionResource]*metav1.ListOptions
	// each monitor list/watches a resource (including manifestwork)
	monitors monitors
	// garbage collector attempts to delete the items in attemptToDelete queue when the time is ripe.
	attemptToDelete workqueue.RateLimitingInterface
}

// NewGarbageCollector creates a new garbage collector instance.
func NewGarbageCollector(
	workClientHolder *cloudeventswork.ClientHolder,
	workInformer workv1informers.ManifestWorkInformer,
	metadataClient metadata.Interface,
	ownerGVRFilters map[schema.GroupVersionResource]*metav1.ListOptions) *GarbageCollector {

	workClient := workClientHolder.WorkInterface().WorkV1()
	if err := workInformer.Informer().AddIndexers(cache.Indexers{
		manifestWorkByOwner: indexManifestWorkByOwner,
	}); err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to add indexers: %v", err))
	}

	return &GarbageCollector{
		workClient:      workClient,
		workIndexer:     workInformer.Informer().GetIndexer(),
		workInformer:    workInformer,
		metadataClient:  metadataClient,
		ownerGVRFilters: ownerGVRFilters,
		attemptToDelete: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "garbage_collector_attempt_to_delete"),
	}
}

// Run starts garbage collector monitors and workers.
func (gc *GarbageCollector) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()
	defer gc.attemptToDelete.ShutDown()

	logger := klog.FromContext(ctx)
	logger.Info("Starting garbage collector")
	defer logger.Info("Shutting down garbage collector")

	// start monitors
	if err := gc.startMonitors(ctx, logger); err != nil {
		logger.Error(err, "Failed to start monitors")
		return
	}

	// wait for the controller cache to sync
	if !cache.WaitForNamedCacheSync("garbage collector", ctx.Done(), func() bool {
		return gc.hasSynced(logger)
	}) {
		return
	}
	logger.Info("All resource monitors have synced, proceeding to collect garbage")

	// run gc workers to process attemptToDelete queue.
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, gc.runAttemptToDeleteWorker, 1*time.Second)
	}

	<-ctx.Done()
}

// startMonitors starts the monitor list/watches a resource (including manifestwork)
func (gc *GarbageCollector) startMonitors(ctx context.Context, logger klog.Logger) error {
	logger.Info("Starting monitors")
	gc.monitors = make(monitors)
	// add monitor for manifestwork
	gc.monitors[workapiv1.SchemeGroupVersion.WithResource("manifestworks")] = &monitor{
		Controller:          gc.workInformer.Informer().GetController(),
		SharedIndexInformer: gc.workInformer.Informer(),
	}

	// add monitor for owner resources
	for gvr, listOptions := range gc.ownerGVRFilters {
		monitor, err := gc.monitorFor(logger, gvr, listOptions)
		if err != nil {
			return err
		}
		gc.monitors[gvr] = monitor
	}

	// start monitors
	started := 0
	for _, monitor := range gc.monitors {
		go monitor.Controller.Run(ctx.Done())
		go monitor.SharedIndexInformer.Run(ctx.Done())
		started++
	}

	logger.V(4).Info("Started monitors", "started", started, "total", len(gc.monitors))
	return nil
}

// monitorFor creates monitor for owner resource
func (gc *GarbageCollector) monitorFor(logger klog.Logger, gvr schema.GroupVersionResource, listOptions *metav1.ListOptions) (*monitor, error) {
	handlers := cache.ResourceEventHandlerFuncs{
		// TODO: Handle the case where the owner resource is deleted
		// while the garbage collector is restarting.
		AddFunc:    func(obj interface{}) {},
		UpdateFunc: func(oldObj, newObj interface{}) {},
		DeleteFunc: func(obj interface{}) {
			// delta fifo may wrap the object in a cache.DeletedFinalStateUnknown, unwrap it
			if deletedFinalStateUnknown, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = deletedFinalStateUnknown.Obj
			}
			accessor, err := meta.Accessor(obj)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("cannot access obj: %v", err))
				return
			}

			ownerUID := accessor.GetUID()
			objs, err := gc.workIndexer.ByIndex(manifestWorkByOwner, string(ownerUID))
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("failed to get manifestwork by owner UID index: %v", err))
				return
			}

			for _, o := range objs {
				manifestWork, ok := o.(*workapiv1.ManifestWork)
				if !ok {
					utilruntime.HandleError(fmt.Errorf("expect a *ManifestWork, got %v", o))
					continue
				}
				namesapcedName := types.NamespacedName{Namespace: manifestWork.Namespace, Name: manifestWork.Name}
				logger.V(4).Info("enqueue manifestWork because of owner deletion", "manifestwork", namesapcedName, "owner UID", ownerUID)
				gc.attemptToDelete.Add(&dependent{ownerUID: ownerUID, namespacedName: namesapcedName})
			}
		},
	}

	// create informer for owner resource with GVR and listOptions.
	informer := metadatainformer.NewFilteredMetadataInformer(gc.metadataClient, gvr,
		metav1.NamespaceAll, 10*time.Minute,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		func(options *metav1.ListOptions) {
			if listOptions != nil {
				options.FieldSelector = listOptions.FieldSelector
				options.LabelSelector = listOptions.LabelSelector
			}
		})
	if _, err := informer.Informer().AddEventHandlerWithResyncPeriod(handlers, 0); err != nil {
		return nil, err
	}
	return &monitor{
		Controller:          informer.Informer().GetController(),
		SharedIndexInformer: informer.Informer(),
	}, nil
}

// HasSynced returns true if any monitors exist AND all those monitors'
// controllers HasSynced functions return true.
func (gc *GarbageCollector) hasSynced(logger klog.Logger) bool {
	if len(gc.monitors) == 0 {
		logger.V(4).Info("garbage collector monitors are not synced: no monitors")
		return false
	}

	for resource, monitor := range gc.monitors {
		if !monitor.Controller.HasSynced() {
			logger.V(4).Info("garbage controller monitor is not yet synced", "resource", resource)
			return false
		}
	}

	return true
}

// runAttemptToDeleteWorker start work to process the attemptToDelete queue.
func (gc *GarbageCollector) runAttemptToDeleteWorker(ctx context.Context) {
	for gc.processAttemptToDeleteWorker(ctx) {
	}
}

func (gc *GarbageCollector) processAttemptToDeleteWorker(ctx context.Context) bool {
	item, quit := gc.attemptToDelete.Get()
	if quit {
		return false
	}
	defer gc.attemptToDelete.Done(item)

	action := gc.attemptToDeleteWorker(ctx, item)
	switch action {
	case forgetItem:
		gc.attemptToDelete.Forget(item)
	case requeueItem:
		gc.attemptToDelete.AddRateLimited(item)
	}

	return true
}

type workQueueItemAction int

const (
	requeueItem = iota
	forgetItem
)

func (gc *GarbageCollector) attemptToDeleteWorker(ctx context.Context, item interface{}) workQueueItemAction {
	dep, ok := item.(*dependent)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("expect a *dependent, got %v", item))
		return forgetItem
	}

	logger := klog.FromContext(ctx)
	logger.V(4).Info("Attempting to delete manifestwork", "ownerUID", dep.ownerUID, "namespacedName", dep.namespacedName)

	latest, err := gc.workClient.ManifestWorks(dep.namespacedName.Namespace).Get(ctx, dep.namespacedName.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(4).Info("Manifestwork not found, skipping", "manifestwork", dep.namespacedName)
			return forgetItem
		}
		return requeueItem
	}

	ownerReferences := latest.GetOwnerReferences()
	found := false
	for _, owner := range ownerReferences {
		if owner.UID == dep.ownerUID {
			found = true
			break
		}
	}

	if found {
		// if the deleted owner reference is the only owner reference then delete the manifestwork
		if len(ownerReferences) == 1 {
			logger.V(4).Info("All owner references are deleted for manifestwork, deleting the manifestwork itself", "manifestwork", dep.namespacedName)
			if err := gc.workClient.ManifestWorks(dep.namespacedName.Namespace).Delete(ctx, dep.namespacedName.Name, metav1.DeleteOptions{}); err != nil {
				return requeueItem
			}
			return forgetItem
		}

		// remove the owner reference from the manifestwork
		logger.V(4).Info("Removing owner reference from manifestwork", "owner", dep.ownerUID, "manifestwork", dep.namespacedName)
		jmp, err := generateDeleteOwnerRefJSONMergePatch(latest, dep.ownerUID)
		if err != nil {
			logger.Error(err, "Failed to generate JSON merge patch", "error")
			return requeueItem
		}
		if _, err = gc.workClient.ManifestWorks(dep.namespacedName.Namespace).Patch(ctx, dep.namespacedName.Name, types.MergePatchType, jmp, metav1.PatchOptions{}); err != nil {
			logger.Error(err, "Failed to patch manifestwork with json patch")
			return requeueItem
		}
		logger.V(4).Info("Successfully removed owner reference from manifestwork", "owner", dep.ownerUID, "manifestwork", dep.namespacedName)
	}

	return forgetItem
}

func indexManifestWorkByOwner(obj interface{}) ([]string, error) {
	manifestWork, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManifestWork", obj)
	}

	var ownerKeys []string
	for _, ownerRef := range manifestWork.GetOwnerReferences() {
		ownerKeys = append(ownerKeys, string(ownerRef.UID))
	}

	return ownerKeys, nil
}

// objectForOwnerRefsPatch defines object struct for owner references patch operation.
type objectForOwnerRefsPatch struct {
	ObjectMetaForOwnerRefsPatch `json:"metadata"`
}

// ObjectMetaForOwnerRefsPatch defines object meta struct for owner references patch operation.
type ObjectMetaForOwnerRefsPatch struct {
	ResourceVersion string                  `json:"resourceVersion"`
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
}

// returns JSON merge patch that removes the ownerReferences matching ownerUID.
func generateDeleteOwnerRefJSONMergePatch(obj metav1.Object, ownerUID types.UID) ([]byte, error) {
	expectedObjectMeta := objectForOwnerRefsPatch{}
	expectedObjectMeta.ResourceVersion = obj.GetResourceVersion()
	refs := obj.GetOwnerReferences()
	for _, ref := range refs {
		if ref.UID != ownerUID {
			expectedObjectMeta.OwnerReferences = append(expectedObjectMeta.OwnerReferences, ref)
		}
	}
	return json.Marshal(expectedObjectMeta)
}
