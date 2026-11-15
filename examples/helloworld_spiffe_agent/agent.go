// Package helloworld_spiffe_agent is the agent of the helloworld-spiffe example add-on. It has no
// hub kubeconfig and no Secret: it reads the hub's address and its own SPIFFE ID from the
// <addon>-spiffe ConfigMap the registration agent writes, gets its SVID from the local SPIRE agent,
// and talks to the hub over gRPC. Every interval it sets a condition on its own
// ManagedClusterAddOn on the hub, and records the same in a ConfigMap on the managed cluster.
package helloworld_spiffe_agent //nolint:revive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/spiffeclient"
	"open-cluster-management.io/addon-framework/pkg/version"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	cloudeventsaddon "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	cloudeventsaddonv1beta1 "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1beta1"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	cloudeventsstore "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
)

// HelloConfigMap is the ConfigMap the agent keeps up to date on the managed cluster.
const HelloConfigMap = "helloworld-spiffe-hello"

// AgentOptions are the agent's flags.
type AgentOptions struct {
	HubConfigDir   string
	ClusterName    string
	AddonNamespace string
	Interval       time.Duration
}

func NewAgentCommand() *cobra.Command {
	o := &AgentOptions{HubConfigDir: "/var/run/hub", Interval: 30 * time.Second}
	cmd := cmdfactory.NewControllerCommandConfig("helloworld-spiffe-agent", version.Get(), o.RunAgent).NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the helloworld-spiffe add-on agent"
	flags := cmd.Flags()
	flags.StringVar(&o.HubConfigDir, "hub-config-dir", o.HubConfigDir,
		"Directory with the add-on's SPIFFE settings (the <addon>-spiffe ConfigMap).")
	flags.StringVar(&o.ClusterName, "cluster-name", o.ClusterName, "Name of the managed cluster.")
	flags.StringVar(&o.AddonNamespace, "addon-namespace", o.AddonNamespace, "The add-on's install namespace.")
	flags.DurationVar(&o.Interval, "interval", o.Interval, "How often to say hello.")
	return cmd
}

// RunAgent connects to the hub with the agent's SVID and says hello every interval.
func (o *AgentOptions) RunAgent(ctx context.Context, kubeconfig *rest.Config) error {
	if !spiffeclient.IsSPIFFEConfigDir(o.HubConfigDir) {
		return fmt.Errorf("%s has no SPIFFE settings: this example needs a cluster whose add-ons use the spiffe driver",
			o.HubConfigDir)
	}
	cfg, err := spiffeclient.LoadConfig(o.HubConfigDir)
	if err != nil {
		return err
	}
	hubAddOnClient, closer, err := newHubAddOnClient(ctx, o.HubConfigDir, o.ClusterName)
	if err != nil {
		return err
	}
	defer closer.Close()
	klog.Infof("connecting to the hub at %s as %s", cfg.URL, cfg.AgentSPIFFEID)

	// An informer on the add-on client: its first list initiates the client's store, and only then
	// does the client ask the hub to resync, that is, to send this cluster's ManagedClusterAddOns.
	addOnInformer := addoninformers.NewSharedInformerFactoryWithOptions(hubAddOnClient, 10*time.Minute,
		addoninformers.WithNamespace(o.ClusterName)).Addon().V1beta1().ManagedClusterAddOns()
	go addOnInformer.Informer().Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), addOnInformer.Informer().HasSynced) {
		return fmt.Errorf("the ManagedClusterAddOn informer didn't sync")
	}
	spokeClient, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := o.sayHello(ctx, hubAddOnClient, addOnInformer.Lister(), spokeClient, cfg.AgentSPIFFEID); err != nil {
			klog.Errorf("hello failed: %v", err)
		}
	}, o.Interval)
	return nil
}

// newHubAddOnClient and addOnInterface are replaceable in tests.
var (
	newHubAddOnClient = hubAddOnClient
	addOnInterface    = cloudeventsaddon.ManagedClusterAddOnInterface
)

// hubAddOnClient opens the local Workload API, where the agent's SVID stays in memory and is
// rotated by SPIRE, and builds a ManagedClusterAddOn client over the hub's gRPC service. Close
// the returned closer when the agent stops.
func hubAddOnClient(ctx context.Context, dir, clusterName string) (addonclient.Interface, io.Closer, error) {
	grpcOptions, closer, err := spiffeclient.NewGRPCOptions(ctx, dir)
	if err != nil {
		return nil, nil, err
	}
	store := cloudeventsstore.NewAgentInformerWatcherStore[*addonapiv1beta1.ManagedClusterAddOn]()
	client, err := addOnInterface(ctx, cloudeventsoptions.NewGenericClientOptions(grpcOptions,
		cloudeventsaddonv1beta1.NewManagedClusterAddOnCodec(), fmt.Sprintf("%s-%s", clusterName, helloworld_spiffe.AddonName)).
		WithClusterName(clusterName).WithClientWatcherStore(store))
	if err != nil {
		_ = closer.Close()
		return nil, nil, err
	}
	return client, closer, nil
}

func (o *AgentOptions) sayHello(ctx context.Context, hub addonclient.Interface,
	lister addonlisterv1beta1.ManagedClusterAddOnLister, spoke kubernetes.Interface, agentID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	addOn, err := lister.ManagedClusterAddOns(o.ClusterName).Get(helloworld_spiffe.AddonName)
	if err != nil {
		return fmt.Errorf("get the ManagedClusterAddOn from the hub: %w", err)
	}
	addOns := hub.AddonV1beta1().ManagedClusterAddOns(o.ClusterName)

	// The hub's status update replaces the conditions, so send them all, with ours set.
	conditions := append([]metav1.Condition(nil), addOn.Status.Conditions...)
	meta.SetStatusCondition(&conditions, metav1.Condition{
		Type:    helloworld_spiffe.ConditionHello,
		Status:  metav1.ConditionTrue,
		Reason:  "HelloSent",
		Message: fmt.Sprintf("hello from %s at %s", agentID, now),
	})
	// Without this, SetStatusCondition keeps the old time when only the message changes.
	meta.FindStatusCondition(conditions, helloworld_spiffe.ConditionHello).LastTransitionTime = metav1.Now()
	// Conditions always marshal.
	patch, _ := json.Marshal(map[string]any{"status": map[string]any{"conditions": conditions}})
	if _, err := addOns.Patch(ctx, addOn.Name, kubetypes.MergePatchType, patch, metav1.PatchOptions{}, "status"); err != nil {
		return fmt.Errorf("update the ManagedClusterAddOn status on the hub: %w", err)
	}

	cms := spoke.CoreV1().ConfigMaps(o.AddonNamespace)
	data := map[string]string{"agent": agentID, "lastHello": now, "hub": "ManagedClusterAddOn status updated"}
	cm, err := cms.Get(ctx, HelloConfigMap, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: HelloConfigMap, Namespace: o.AddonNamespace}, Data: data},
			metav1.CreateOptions{})
	case err == nil:
		cm.Data = data
		_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("write ConfigMap %s/%s: %w", o.AddonNamespace, HelloConfigMap, err)
	}
	klog.Infof("hello sent at %s", now)
	return nil
}
