package cloudevents

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"

	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/agent/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	certificatesv1 "k8s.io/api/certificates/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/cloudevents"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/store"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

const (
	sourceID            = "addon-manager-integration-test"
	cloudEventsClientID = "addon-manager-integration-test"
	mqttBrokerHost      = "127.0.0.1:1883"
	workDriverType      = "mqtt"
)

var mqttBroker *mochimqtt.Server
var tempDir string
var workDriverConfigFile string

var testEnv *envtest.Environment
var hubClusterClient clusterv1client.Interface
var hubAddonClient addonv1alpha1client.Interface
var hubKubeClient kubernetes.Interface
var testAddonImpl *testAddon
var testHostedAddonImpl *testAddon
var testInstallByLableAddonImpl *testAddon
var testMultiWorksAddonImpl *testAddon
var mgrCtx context.Context
var mgrCtxCancel context.CancelFunc
var addonManager addonmanager.AddonManager

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func(done ginkgo.Done) {
	ginkgo.By("bootstrapping test mqtt broker")

	mqttBroker = mochimqtt.New(nil)
	err := mqttBroker.AddHook(new(auth.AllowHook), nil)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = mqttBroker.AddListener(listeners.NewTCP(listeners.Config{
		ID:      "test-mqtt-broker",
		Address: mqttBrokerHost,
	}))
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// start the mqtt broker
	go func() {
		err := mqttBroker.Serve()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}()

	tempDir, err = os.MkdirTemp("", "test")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(tempDir).ToNot(gomega.BeEmpty())
	workDriverConfigFile = path.Join(tempDir, "mqttconfig")

	// write the mqtt broker config to a file
	workDriverConfig := mqtt.MQTTConfig{
		BrokerHost: mqttBrokerHost,
		Topics: &types.Topics{
			SourceEvents:    fmt.Sprintf("sources/%s/clusters/+/sourceevents", sourceID),
			AgentEvents:     fmt.Sprintf("sources/%s/clusters/+/agentevents", sourceID),
			SourceBroadcast: fmt.Sprintf("sources/%s/sourcebroadcast", sourceID),
		},
	}
	workDriverConfigYAML, err := yaml.Marshal(workDriverConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = os.WriteFile(workDriverConfigFile, workDriverConfigYAML, 0600)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "cluster", "v1beta1"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "addon", "v1alpha1"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())
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
		hostInfoFn:    constants.GetHostedModeInfo,
	}

	testHostedAddonImpl = &testAddon{
		name:              "test-hosted",
		manifests:         map[string][]runtime.Object{},
		registrations:     map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostedModeEnabled: true,
		hostInfoFn:        constants.GetHostedModeInfo,
	}

	testInstallByLableAddonImpl = &testAddon{
		name:          "test-install-all",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostInfoFn:    constants.GetHostedModeInfo,
	}

	testMultiWorksAddonImpl = &testAddon{
		name:          "test-multi-works",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostInfoFn:    constants.GetHostedModeInfo,
	}

	mgrCtx, mgrCtxCancel = context.WithCancel(context.TODO())
	// start hub controller
	go func() {
		cloudEventsOptions := cloudevents.NewCloudEventsOptions()
		cloudEventsOptions.WorkDriver = workDriverType
		cloudEventsOptions.WorkDriverConfig = workDriverConfigFile
		cloudEventsOptions.SourceID = sourceID
		cloudEventsOptions.CloudEventsClientID = cloudEventsClientID
		addonManager, err = cloudevents.New(cfg, cloudEventsOptions)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testInstallByLableAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testHostedAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testMultiWorksAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.Start(mgrCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	close(done)
}, 300)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	mgrCtxCancel()
	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = mqttBroker.Close()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
})

type testAddon struct {
	name                string
	manifests           map[string][]runtime.Object
	registrations       map[string][]addonapiv1alpha1.RegistrationConfig
	approveCSR          bool
	cert                []byte
	prober              *agent.HealthProber
	hostedModeEnabled   bool
	hostInfoFn          func(addon *addonapiv1alpha1.ManagedClusterAddOn, cluster *clusterv1.ManagedCluster) (string, string)
	supportedConfigGVRs []schema.GroupVersionResource
}

func (t *testAddon) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
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
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster) ([]addonapiv1alpha1.RegistrationConfig, error) {
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

func startWorkAgent(ctx context.Context, clusterName string) (*work.ClientHolder, workv1informers.ManifestWorkInformer, error) {
	config := &mqtt.MQTTOptions{
		KeepAlive: 60,
		PubQoS:    1,
		SubQoS:    1,
		Dialer: &mqtt.MQTTDialer{
			Timeout:    5 * time.Second,
			BrokerHost: mqttBrokerHost,
		},

		Topics: types.Topics{
			SourceEvents:   fmt.Sprintf("sources/%s/clusters/%s/sourceevents", sourceID, clusterName),
			AgentEvents:    fmt.Sprintf("sources/%s/clusters/%s/agentevents", sourceID, clusterName),
			AgentBroadcast: fmt.Sprintf("clusters/%s/agentbroadcast", clusterName),
		},
	}
	watcherStore := store.NewAgentInformerWatcherStore()

	clientOptions := options.NewGenericClientOptions(
		config, codec.NewManifestBundleCodec(), clusterName).
		WithClusterName(clusterName).
		WithClientWatcherStore(watcherStore)
	clientHolder, err := work.NewAgentClientHolder(ctx, clientOptions)
	if err != nil {
		return nil, nil, err
	}

	workClient := clientHolder.WorkInterface()
	factory := workinformers.NewSharedInformerFactoryWithOptions(
		workClient, 30*time.Minute, workinformers.WithNamespace(clusterName))
	workInformers := factory.Work().V1().ManifestWorks()

	// For cloudevents work client, we use the informer store as the client store
	if watcherStore != nil {
		watcherStore.SetInformer(workInformers.Informer())
	}

	go workInformers.Informer().Run(ctx.Done())

	return clientHolder, workInformers, nil
}
