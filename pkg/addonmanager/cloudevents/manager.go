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
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/addonconfig"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/agentdeploy"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/certificate"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/cmaconfig"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/cmamanagedby"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/registration"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/utils"
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
	addonConfigs := a.GetAddonConfigs()
	syncContexts := a.GetSyncContexts()

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

	v1CSRSupported, v1beta1Supported, err := utils.IsCSRSupported(kubeClient)
	if err != nil {
		return err
	}

	for _, agentImpl := range addonAgents {
		for _, configGVR := range agentImpl.GetAgentAddonOptions().SupportedConfigGVRs {
			addonConfigs[configGVR] = true
		}
	}

	deployController := agentdeploy.NewAddonDeployController(
		workClient,
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		workInformers,
		addonAgents,
	)

	registrationController := registration.NewAddonRegistrationController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonAgents,
	)

	// This controller is used during migrating addons to be managed by addon-manager.
	// This should be removed when the migration is done.
	// The migration plan refer to https://github.com/open-cluster-management-io/ocm/issues/355.
	managementAddonController := cmamanagedby.NewCMAManagedByController(
		addonClient,
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		addonAgents,
		utils.FilterByAddonName(addonAgents),
	)

	var addonConfigController, managementAddonConfigController factory.Controller
	if len(addonConfigs) != 0 {
		addonConfigController = addonconfig.NewAddonConfigController(
			addonClient,
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
			dynamicInformers,
			addonConfigs,
			utils.FilterByAddonName(addonAgents),
		)
		managementAddonConfigController = cmaconfig.NewCMAConfigController(
			addonClient,
			addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
			dynamicInformers,
			addonConfigs,
			utils.FilterByAddonName(addonAgents),
		)
	}

	var csrApproveController factory.Controller
	var csrSignController factory.Controller
	// Spawn the following controllers only if v1 CSR api is supported in the
	// hub cluster. Under v1beta1 CSR api, all the CSR objects will be signed
	// by the kube-controller-manager so custom CSR controller should be
	// disabled to avoid conflict.
	if v1CSRSupported {
		csrApproveController = certificate.NewCSRApprovingController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			kubeInformers.Certificates().V1().CertificateSigningRequests(),
			nil,
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			addonAgents,
		)
		csrSignController = certificate.NewCSRSignController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			kubeInformers.Certificates().V1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			addonAgents,
		)
	} else if v1beta1Supported {
		csrApproveController = certificate.NewCSRApprovingController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			nil,
			kubeInformers.Certificates().V1beta1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			addonAgents,
		)
	}

	a.SetSyncContexts(append(syncContexts, deployController.SyncContext()))

	go deployController.Run(ctx, 1)
	go registrationController.Run(ctx, 1)
	go managementAddonController.Run(ctx, 1)

	if addonConfigController != nil {
		go addonConfigController.Run(ctx, 1)
	}
	if managementAddonConfigController != nil {
		go managementAddonConfigController.Run(ctx, 1)
	}
	if csrApproveController != nil {
		go csrApproveController.Run(ctx, 1)
	}
	if csrSignController != nil {
		go csrSignController.Run(ctx, 1)
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
