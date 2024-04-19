package cloudevents

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"

	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/index"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/codec"
)

// cloudeventsAddonManager is the implementation of AddonManager with
// the base implementation and cloudevents options
type cloudeventsAddonManager struct {
	*addonmanager.BaseAddonManagerImpl
	options *CloudEventsOptions
}

func (a *cloudeventsAddonManager) Start(ctx context.Context) error {
	config := a.GetConfig()
	addonAgents := a.GetAddonAgents()

	var addonNames []string
	for key := range addonAgents {
		addonNames = append(addonNames, key)
	}

	// To support sending ManifestWorks to different drivers (like the Kubernetes apiserver or MQTT broker), we build
	// ManifestWork client that implements the ManifestWorkInterface and ManifestWork informer based on different
	// driver configuration.
	// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.
	_, clientConfig, err := cloudeventswork.NewConfigLoader(a.options.WorkDriver, a.options.WorkDriverConfig).
		WithKubeConfig(a.GetConfig()).
		LoadConfig()
	if err != nil {
		return err
	}

	// we need a separated filtered manifestwork informers so we only watch the manifestworks that manifestworkreplicaset cares.
	// This could reduce a lot of memory consumptions
	workInformOption := workinformers.WithTweakListOptions(
		func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpIn,
						Values:   addonNames,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		},
	)

	clientHolder, err := cloudeventswork.NewClientHolderBuilder(clientConfig).
		WithClientID(a.options.CloudEventsClientID).
		WithSourceID(a.options.SourceID).
		WithInformerConfig(10*time.Minute, workInformOption).
		WithCodecs(codec.NewManifestBundleCodec()).
		NewSourceClientHolder(ctx)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	addonClient, err := addonv1alpha1client.NewForConfig(config)
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(config)
	if err != nil {
		return err
	}

	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute,
		kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpIn,
						Values:   addonNames,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)

	workClient := clientHolder.WorkInterface()
	workInformers := clientHolder.ManifestWorkInformer()

	// addonDeployController
	err = workInformers.Informer().AddIndexers(
		cache.Indexers{
			index.ManifestWorkByAddon:           index.IndexManifestWorkByAddon,
			index.ManifestWorkByHostedAddon:     index.IndexManifestWorkByHostedAddon,
			index.ManifestWorkHookByHostedAddon: index.IndexManifestWorkHookByHostedAddon,
		},
	)
	if err != nil {
		return err
	}

	err = addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
		cache.Indexers{
			index.ManagedClusterAddonByNamespace: index.IndexManagedClusterAddonByNamespace, // addonDeployController
			index.ManagedClusterAddonByName:      index.IndexManagedClusterAddonByName,      // addonConfigController
			index.AddonByConfig:                  index.IndexAddonByConfig,                  // addonConfigController
		},
	)
	if err != nil {
		return err
	}

	err = addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().AddIndexers(
		cache.Indexers{
			index.ClusterManagementAddonByConfig:    index.IndexClusterManagementAddonByConfig,    // managementAddonConfigController
			index.ClusterManagementAddonByPlacement: index.IndexClusterManagementAddonByPlacement, // addonConfigController
		})
	if err != nil {
		return err
	}

	err = a.StartWithInformers(ctx, workClient, workInformers, kubeInformers, addonInformers, clusterInformers, dynamicInformers)
	if err != nil {
		return err
	}

	kubeInformers.Start(ctx.Done())
	go workInformers.Informer().Run(ctx.Done())
	addonInformers.Start(ctx.Done())
	clusterInformers.Start(ctx.Done())
	dynamicInformers.Start(ctx.Done())
	return nil
}

// New returns a new addon manager with the given config and optional options
func New(config *rest.Config, opts *CloudEventsOptions) (addonmanager.AddonManager, error) {
	cloudeventsAddonManager := &cloudeventsAddonManager{
		BaseAddonManagerImpl: addonmanager.NewBaseAddonManagerImpl(config),
		options:              opts,
	}

	return cloudeventsAddonManager, nil
}
