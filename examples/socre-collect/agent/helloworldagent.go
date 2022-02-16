package agent

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	"open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"open-cluster-management.io/addon-framework/pkg/lease"
	"open-cluster-management.io/addon-framework/pkg/version"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterinformers1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	v1alpha2 "open-cluster-management.io/api/cluster/v1alpha1"
)

// Helloworld Agent is an example that syncs configmap in cluster namespace of hub cluster
// to the install namespace in managedcluster.
// addOnAgentInstallationNamespace is the namespace on the managed cluster to install the helloworld addon agent.
const HelloworldAgentInstallationNamespace = "default"
const AddOnPlacementScoresName = "test-score1"

func NewAgentCommand(addonName string) *cobra.Command {
	o := NewAgentOptions(addonName)
	cmd := controllercmd.
		NewControllerCommandConfig("addonplacementscorecollect-addon-agent", version.Get(), o.RunAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the addon agent"

	o.AddFlags(cmd)
	return cmd
}

// AgentOptions defines the flags for workload agent
type AgentOptions struct {
	HubKubeconfigFile string
	SpokeClusterName  string
	AddonName         string
	AddonNamespace    string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewAgentOptions(addonName string) *AgentOptions {
	return &AgentOptions{AddonName: addonName}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.AddonNamespace, "addon-namespace", o.AddonNamespace, "Installation namespace of addon.")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// build kubeclient of managed cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// build kubeinformerfactory of hub cluster
	hubRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.HubKubeconfigFile)
	if err != nil {
		return err
	}
	// ++2
	hubClusterClient, err := clusterclient.NewForConfig(hubRestConfig)
	if err != nil {
		return nil
	}

	if err != nil {
		return err
	}
	// hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute, informers.WithNamespace(o.SpokeClusterName))
	// ++4
	clusterInformers := clusterinformers.NewSharedInformerFactoryWithOptions(hubClusterClient, 10*time.Minute, clusterinformers.WithNamespace(o.SpokeClusterName))

	// create an agent controller
	agent := newAgentController(
		spokeKubeClient,
		hubClusterClient,
		clusterInformers.Cluster().V1alpha1().AddOnPlacementScores(),
		o.SpokeClusterName,
		o.AddonName,
		o.AddonNamespace,
		controllerContext.EventRecorder,
		AddOnPlacementScoresName,
	)
	// create a lease updater
	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		o.AddonName,
		o.AddonNamespace,
	)

	// go hubKubeInformerFactory.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())
	go agent.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}

type agentController struct {
	spokeKubeClient           kubernetes.Interface
	hubKubeClient             clientset.Interface
	addonClient               addonv1alpha1client.Interface
	AddOnPlacementScoreLister v1alpha1.AddOnPlacementScoreLister
	clusterName               string
	addonName                 string
	addonNamespace            string
	recorder                  events.Recorder
}

func newAgentController(
	spokeKubeClient kubernetes.Interface,
	hubKubeClient clientset.Interface,
	addOnPlacementScoreInformer clusterinformers1.AddOnPlacementScoreInformer,
	clusterName string,
	addonName string,
	addonNamespace string,
	recorder events.Recorder,
	scoreName string,
) factory.Controller {
	c := &agentController{
		spokeKubeClient:           spokeKubeClient,
		hubKubeClient:             hubKubeClient,
		clusterName:               clusterName,
		addonName:                 addonName,
		addonNamespace:            addonNamespace,
		AddOnPlacementScoreLister: addOnPlacementScoreInformer.Lister(),
		recorder:                  recorder,
	}
	//return factory.New().WithFilteredEventsInformersQueueKeyFunc(
	//	func(obj runtime.Object) string {
	//		key, _ := cache.MetaNamespaceKeyFunc(obj)
	//		return key
	//	}, func(obj interface{}) bool {
	//		accessor, err := meta.Accessor(obj)
	//		if err != nil {
	//			return false
	//		}
	//		name := accessor.GetName()
	//		return name == scoreName
	//	}, addOnPlacementScoreInformer.Informer()).WithSync(c.sync).ResyncEvery(time.Second*30).ToController("score-agent-controller", recorder)
	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, addOnPlacementScoreInformer.Informer()).
		WithSync(c.sync).ResyncEvery(time.Second*60).ToController("score-agent-controller", recorder)
}

func (c *agentController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	fmt.Printf("start syncing\n")
	klog.Infof("start syncing")
	// 在agent上跑手机cpu，内存，信息的score
	// 查询hub端有没有addonplacementscore，如果没有这个score，那么创建这个score
	// 如果有这个score，修改这个score
	addonPlacementScore, err := c.AddOnPlacementScoreLister.AddOnPlacementScores(c.clusterName).Get(AddOnPlacementScoresName)
	switch {
	case errors.IsNotFound(err):
		// 不存在创建一个分数都是0的默认
		addonPlacementScore = &v1alpha2.AddOnPlacementScore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: c.clusterName,
				Name:      AddOnPlacementScoresName,
			},
			Status: v1alpha2.AddOnPlacementScoreStatus{
				Scores: []v1alpha2.AddOnPlacementScoreItem{
					{
						Name:  "cpu",
						Value: 0,
					},
					{
						Name:  "mem",
						Value: 0,
					},
				},
			},
		}
		_, err = c.hubKubeClient.ClusterV1alpha1().AddOnPlacementScores(c.clusterName).Create(ctx, addonPlacementScore, v1.CreateOptions{})
		if err != nil {
			return err
		}
		klog.Infof("create a new AddOnPlacementScore: %+v", addonPlacementScore.Status)
		fmt.Printf("create a new AddOnPlacementScore: %+v\n", addonPlacementScore.Status)
		return nil
	case err != nil:
		return err
	}

	// todo addonplacementscore存在， 手机具体分数，测试暂时指定分数都是99
	addonPlacementScore.Status.Scores = []v1alpha2.AddOnPlacementScoreItem{
		{
			Name:  "cpu",
			Value: 99,
		},
		{
			Name:  "mem",
			Value: 99,
		},
	}

	// 更新到hub
	_, err = c.hubKubeClient.ClusterV1alpha1().AddOnPlacementScores(c.clusterName).UpdateStatus(ctx, addonPlacementScore, v1.UpdateOptions{})
	klog.Infof("update AddOnPlacementScore: %+v", addonPlacementScore.Status)
	fmt.Printf("update AddOnPlacementScore: %+v\n", addonPlacementScore.Status)
	return err
}
