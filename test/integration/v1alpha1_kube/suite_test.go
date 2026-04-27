package kube

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/agent"
	agentv1alpha1 "open-cluster-management.io/addon-framework/pkg/agent/v1alpha1"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	certificatesv1 "k8s.io/api/certificates/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var addOnDeploymentConfigGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "addondeploymentconfigs",
}

var testEnv *envtest.Environment
var hubWorkClient workclientset.Interface
var hubClusterClient clusterv1client.Interface
var hubAddonClient addonv1alpha1client.Interface
var hubKubeClient kubernetes.Interface
var testAddonImpl *testAddon
var testHostedAddonImpl *testAddon
var testInstallByLableAddonImpl *testAddon
var testMultiWorksAddonImpl *testAddon
var cancel context.CancelFunc
var mgrContext context.Context
var addonManager addonmanager.AddonManager

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func(done ginkgo.Done) {
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1beta1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1beta1", "0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1beta1", "0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1beta1", "0000_02_addon.open-cluster-management.io_addondeploymentconfigs.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1alpha1", "0000_03_addon.open-cluster-management.io_addontemplates.crd.yaml"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubClusterClient, err = clusterv1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubAddonClient, err = addonv1alpha1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	testAddonImpl = &testAddon{
		name:          "test",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostInfoFn:    getHostedModeInfoV1alpha1,
	}

	testHostedAddonImpl = &testAddon{
		name:              "test-hosted",
		manifests:         map[string][]runtime.Object{},
		registrations:     map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostedModeEnabled: true,
		hostInfoFn:        getHostedModeInfoV1alpha1,
	}

	testInstallByLableAddonImpl = &testAddon{
		name:          "test-install-all",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostInfoFn:    getHostedModeInfoV1alpha1,
	}

	testMultiWorksAddonImpl = &testAddon{
		name:          "test-multi-works",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostInfoFn:    getHostedModeInfoV1alpha1,
	}

	mgrContext, cancel = context.WithCancel(context.TODO())
	// start hub controller
	go func() {
		defer ginkgo.GinkgoRecover()
		addonManager, err = addonmanager.New(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		// Wrap v1alpha1 addons with adapter to work with v1beta1 manager
		err = addonManager.AddAgent(agent.WrapV1alpha1(testAddonImpl))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(agent.WrapV1alpha1(testInstallByLableAddonImpl))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(agent.WrapV1alpha1(testHostedAddonImpl))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(agent.WrapV1alpha1(testMultiWorksAddonImpl))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.Start(mgrContext)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	close(done)
}, 300)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	cancel()
	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

type testAddon struct {
	name                string
	manifests           map[string][]runtime.Object
	registrations       map[string][]addonapiv1alpha1.RegistrationConfig
	approveCSR          bool
	cert                []byte
	prober              *agentv1alpha1.HealthProber
	hostedModeEnabled   bool
	hostInfoFn          func(addon *addonapiv1alpha1.ManagedClusterAddOn, cluster *clusterv1.ManagedCluster) (string, string)
	supportedConfigGVRs []schema.GroupVersionResource
}

func (t *testAddon) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return t.manifests[cluster.Name], nil
}

func (t *testAddon) GetAgentAddonOptions() agentv1alpha1.AgentAddonOptions {
	option := agentv1alpha1.AgentAddonOptions{
		AddonName:           t.name,
		HealthProber:        t.prober,
		HostedModeEnabled:   t.hostedModeEnabled,
		HostedModeInfoFunc:  t.hostInfoFn,
		SupportedConfigGVRs: t.supportedConfigGVRs,
	}

	if len(t.registrations) > 0 {
		option.Registration = &agentv1alpha1.RegistrationOption{
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster,
				addon *addonapiv1alpha1.ManagedClusterAddOn) ([]addonapiv1alpha1.RegistrationConfig, error) {
				return t.registrations[cluster.Name], nil
			},
			CSRApproveCheck: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return t.approveCSR
			},
			CSRSign: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
				csr *certificatesv1.CertificateSigningRequest) ([]byte, error) {
				return t.cert, nil
			},
		}
	}

	return option
}

func newClusterManagementAddon(name string) *addonapiv1alpha1.ClusterManagementAddOn {
	return &addonapiv1alpha1.ClusterManagementAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
			InstallStrategy: addonapiv1alpha1.InstallStrategy{
				Type: addonapiv1alpha1.AddonInstallStrategyManual,
			},
		},
	}
}

// getHostedModeInfoV1alpha1 is a v1alpha1 adapter for constants.GetHostedModeInfo
func getHostedModeInfoV1alpha1(addon *addonapiv1alpha1.ManagedClusterAddOn, _ *clusterv1.ManagedCluster) (string, string) {
	if len(addon.Annotations) == 0 {
		return constants.InstallModeDefault, ""
	}
	hostingClusterName, ok := addon.Annotations[addonapiv1alpha1.HostingClusterNameAnnotationKey]
	if !ok {
		return constants.InstallModeDefault, ""
	}
	return constants.InstallModeHosted, hostingClusterName
}

// newV1alpha1DeploymentProber creates a v1alpha1 HealthProber for deployments
func newV1alpha1DeploymentProber(deployments ...types.NamespacedName) *agentv1alpha1.HealthProber {
	probeFields := []agentv1alpha1.ProbeField{}
	for _, deploy := range deployments {
		mc := utils.DeploymentWellKnowManifestConfig(deploy.Namespace, deploy.Name)
		probeFields = append(probeFields, agentv1alpha1.ProbeField{
			ResourceIdentifier: mc.ResourceIdentifier,
			ProbeRules:         mc.FeedbackRules,
		})
	}
	return &agentv1alpha1.HealthProber{
		Type: agentv1alpha1.HealthProberTypeWork,
		WorkProber: &agentv1alpha1.WorkHealthProber{
			ProbeFields:   probeFields,
			HealthChecker: v1alpha1DeploymentAvailabilityHealthChecker,
		},
	}
}

func newV1alpha1AllDeploymentsProber() *agentv1alpha1.HealthProber {
	probeFields := []agentv1alpha1.ProbeField{
		{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      "*",
				Namespace: "*",
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		},
	}

	return &agentv1alpha1.HealthProber{
		Type: agentv1alpha1.HealthProberTypeWork,
		WorkProber: &agentv1alpha1.WorkHealthProber{
			ProbeFields:   probeFields,
			HealthChecker: v1alpha1DeploymentAvailabilityHealthChecker,
		},
	}
}

func v1alpha1DeploymentAvailabilityHealthChecker(results []agentv1alpha1.FieldResult,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	for _, result := range results {
		if err := utils.DeploymentAvailabilityHealthCheck(result.ResourceIdentifier, result.FeedbackResult); err != nil {
			return err
		}
	}
	return nil
}
