package integration

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"gopkg.in/yaml.v2"

	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	certificatesv1 "k8s.io/api/certificates/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/test/integration/util"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/spoke"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

const (
	sourceID            = "addonmanager-integration-test"
	cloudEventsClientID = "addonmanager-integration-test"
	mqttBrokerHost      = "127.0.0.1:1883"
	workDriverType      = "mqtt"
)

var mqttBroker *mochimqtt.Server
var workDriverSourceConfigFile string

var tempDir string
var testEnv *envtest.Environment

var hubClusterClient clusterv1client.Interface
var hubAddonClient addonv1alpha1client.Interface
var hubKubeClient kubernetes.Interface
var spokeRestConfig *rest.Config
var spokeKubeClient kubernetes.Interface
var spokeWorkClient workclientset.Interface
var testAddonImpl *testAddon
var testHostedAddonImpl *testAddon
var managerCtx context.Context
var managerCtxCancel context.CancelFunc
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

	err = mqttBroker.AddListener(listeners.NewTCP("mqtt-test-broker", mqttBrokerHost, nil))
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// start the mqtt broker
	go func() {
		err := mqttBroker.Serve()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}()

	tempDir, err = os.MkdirTemp("", "test")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(tempDir).ToNot(gomega.BeEmpty())
	workDriverSourceConfigFile = path.Join(tempDir, "sourcemqttconfig")

	// write the mqtt broker config to a file
	sourceDriverConfig := mqtt.MQTTConfig{
		BrokerHost: mqttBrokerHost,
		Topics: &types.Topics{
			SourceEvents:    fmt.Sprintf("sources/%s/clusters/+/sourceevents", sourceID),
			AgentEvents:     fmt.Sprintf("sources/%s/clusters/+/agentevents", sourceID),
			SourceBroadcast: fmt.Sprintf("sources/%s/sourcebroadcast", sourceID),
		},
	}
	sourceDriverConfigYAML, err := yaml.Marshal(sourceDriverConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = os.WriteFile(workDriverSourceConfigFile, sourceDriverConfigYAML, 0600)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_00_work.open-cluster-management.io_manifestworks.crd.yaml"),
			filepath.Join(".", "vendor", "open-cluster-management.io", "api", "work", "v1", "0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml"),
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

	features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)
	spokeRestConfig = cfg
	spokeKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	spokeWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	testAddonImpl = &testAddon{
		name:          "test",
		manifests:     map[string][]runtime.Object{},
		registrations: map[string][]addonapiv1alpha1.RegistrationConfig{},
	}

	testHostedAddonImpl = &testAddon{
		name:              "test-hosted",
		manifests:         map[string][]runtime.Object{},
		registrations:     map[string][]addonapiv1alpha1.RegistrationConfig{},
		hostedModeEnabled: true,
		hostInfoFn:        constants.GetHostedModeInfo,
	}

	managerCtx, managerCtxCancel = context.WithCancel(context.TODO())
	// start hub controller
	go func() {
		managerOptions := addonmanager.NewManagerOptions()
		managerOptions.SourceID = sourceID
		managerOptions.WorkDriver = workDriverType
		managerOptions.WorkDriverConfig = workDriverSourceConfigFile
		managerOptions.CloudEventsClientID = cloudEventsClientID
		addonManager, err = addonmanager.New(cfg, managerOptions)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.AddAgent(testHostedAddonImpl)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = addonManager.Start(managerCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	close(done)
}, 300)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	managerCtxCancel()
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
		SupportedConfigGVRs: t.supportedConfigGVRs,
		HostedModeInfoFunc:  t.hostInfoFn,
	}

	if len(t.registrations) > 0 {
		option.Registration = &agent.RegistrationOption{
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
				return t.registrations[cluster.Name]
			},
			CSRApproveCheck: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return t.approveCSR
			},
			CSRSign: func(csr *certificatesv1.CertificateSigningRequest) []byte {
				return t.cert
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

func startAgent(ctx context.Context, clusterName string) {
	workDriverAgentConfigFile := path.Join(tempDir, fmt.Sprintf("%s-agentmqttconfig", clusterName))
	agentDriverConfig := mqtt.MQTTConfig{
		BrokerHost: mqttBrokerHost,
		Topics: &types.Topics{
			SourceEvents: fmt.Sprintf("sources/%s/clusters/%s/sourceevents", sourceID, clusterName),
			AgentEvents:  fmt.Sprintf("sources/%s/clusters/%s/agentevents", sourceID, clusterName),
		},
	}
	agentDriverConfigYAML, err := yaml.Marshal(agentDriverConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = os.WriteFile(workDriverAgentConfigFile, agentDriverConfigYAML, 0600)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	o := spoke.NewWorkloadAgentOptions()
	o.StatusSyncInterval = 3 * time.Second
	o.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
	o.WorkloadSourceDriver = workDriverType
	o.WorkloadSourceConfig = workDriverAgentConfigFile
	o.CloudEventsClientID = fmt.Sprintf("%s-work-client", clusterName)
	o.CloudEventsClientCodecs = []string{"manifestbundle"}

	commOptions := commonoptions.NewAgentOptions()
	commOptions.SpokeClusterName = clusterName

	go runWorkAgent(ctx, o, commOptions)
}

func runWorkAgent(ctx context.Context, o *spoke.WorkloadAgentOptions, commOption *commonoptions.AgentOptions) {
	agentConfig := spoke.NewWorkAgentConfig(commOption, o)
	err := agentConfig.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
		KubeConfig:    spokeRestConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}
