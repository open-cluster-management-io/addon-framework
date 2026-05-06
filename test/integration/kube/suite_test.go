package kube

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	certificatesv1 "k8s.io/api/certificates/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var addOnDeploymentConfigGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1beta1",
	Resource: "addondeploymentconfigs",
}

var testEnv *envtest.Environment
var hubWorkClient workclientset.Interface
var hubClusterClient clusterv1client.Interface
var hubAddonClient addonclient.Interface
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

	// Update the stored versions of addon CRDs to v1beta1
	apiExtensionsClient, err := apiextensionsclient.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	for _, crdName := range []string{
		"managedclusteraddons.addon.open-cluster-management.io",
		"clustermanagementaddons.addon.open-cluster-management.io",
	} {
		crd, err := apiExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crdName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		for i, v := range crd.Spec.Versions {
			crd.Spec.Versions[i].Storage = v.Name == "v1beta1"
		}
		crd, err = apiExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().Update(context.TODO(), crd, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		crd.Status.StoredVersions = []string{"v1beta1"}
		_, err = apiExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().UpdateStatus(context.TODO(), crd, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubClusterClient, err = clusterv1client.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubAddonClient, err = addonclient.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	hubKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	testAddonImpl = &testAddon{
		name:          "test",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]agent.RegistrationConfig{},
		hostInfoFn:    constants.GetHostedModeInfo,
	}

	testHostedAddonImpl = &testAddon{
		name:              "test-hosted",
		manifests:         map[string][]runtime.Object{},
		registrations:     map[string][]agent.RegistrationConfig{},
		hostedModeEnabled: true,
		hostInfoFn:        constants.GetHostedModeInfo,
	}

	testInstallByLableAddonImpl = &testAddon{
		name:          "test-install-all",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]agent.RegistrationConfig{},
		hostInfoFn:    constants.GetHostedModeInfo,
	}

	testMultiWorksAddonImpl = &testAddon{
		name:          "test-multi-works",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]agent.RegistrationConfig{},
		hostInfoFn:    constants.GetHostedModeInfo,
	}

	mgrContext, cancel = context.WithCancel(context.TODO())
	// start hub controller
	go func() {
		addonManager, err = addonmanager.New(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testInstallByLableAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testHostedAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testMultiWorksAddonImpl)
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
	registrations       map[string][]agent.RegistrationConfig
	approveCSR          bool
	cert                []byte
	prober              *agent.HealthProber
	hostedModeEnabled   bool
	hostInfoFn          func(addon *addonapiv1beta1.ManagedClusterAddOn, cluster *clusterv1.ManagedCluster) (string, string)
	supportedConfigGVRs []schema.GroupVersionResource
}

func (t *testAddon) Manifests(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return t.manifests[cluster.Name], nil
}

func (t *testAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	option := agent.AgentAddonOptions{
		AddonName:           t.name,
		HealthProber:        t.prober,
		HostedModeEnabled:   t.hostedModeEnabled,
		HostedModeInfoFunc:  t.hostInfoFn,
		SupportedConfigGVRs: t.supportedConfigGVRs,
	}

	if len(t.registrations) > 0 {
		option.Registration = &agent.RegistrationOption{
			Configurations: func(ctx context.Context, cluster *clusterv1.ManagedCluster,
				addon *addonapiv1beta1.ManagedClusterAddOn) ([]agent.RegistrationConfig, error) {
				return t.registrations[cluster.Name], nil
			},
			CSRApproveCheck: func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return t.approveCSR
			},
			CSRSign: func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn,
				csr *certificatesv1.CertificateSigningRequest) ([]byte, error) {
				return t.cert, nil
			},
		}
	}

	return option
}

func newClusterManagementAddon(name string) *addonapiv1beta1.ClusterManagementAddOn {
	return &addonapiv1beta1.ClusterManagementAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
			InstallStrategy: addonapiv1beta1.InstallStrategy{
				Type: addonapiv1beta1.AddonInstallStrategyManual,
			},
		},
	}
}
