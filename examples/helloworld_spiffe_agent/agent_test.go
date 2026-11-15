package helloworld_spiffe_agent //nolint:revive

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe"
	"open-cluster-management.io/addon-framework/pkg/spiffeclient"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	cloudeventsaddon "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	"open-cluster-management.io/sdk-go/pkg/testing/fakespire"
)

const (
	agentID = "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/addon/helloworld-spiffe/agent/agent"
	hubID   = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
)

var errFailed = errors.New("failed")

func settingsDir(t *testing.T, socket string) string {
	t.Helper()
	dir := t.TempDir()
	for k, v := range map[string]string{spiffeclient.KeyHubEndpoint: "127.0.0.1:1", spiffeclient.KeyHubSPIFFEID: hubID,
		spiffeclient.KeyAgentSPIFFEID: agentID, spiffeclient.KeyWorkloadAPISocket: socket} {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func addOn() *addonapiv1beta1.ManagedClusterAddOn {
	return &addonapiv1beta1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: helloworld_spiffe.AddonName, Namespace: "cluster1"},
		Status: addonapiv1beta1.ManagedClusterAddOnStatus{Conditions: []metav1.Condition{
			{Type: "Available", Status: metav1.ConditionTrue, Reason: "x", LastTransitionTime: metav1.Now()}}},
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func TestNewAgentCommand(t *testing.T) {
	cmd := NewAgentCommand()
	if err := cmd.ParseFlags([]string{"--hub-config-dir=/d", "--cluster-name=c1", "--addon-namespace=ns", "--interval=5s"}); err != nil {
		t.Fatal(err)
	}
	if cmd.Use != "agent" || cmd.Flags().Lookup("interval").Value.String() != "5s" {
		t.Errorf("command %s, interval %s", cmd.Use, cmd.Flags().Lookup("interval").Value)
	}
}

func TestHubAddOnClient(t *testing.T) {
	ca := fakespire.NewCA(t, "ocm-hub.example.org")
	spire := fakespire.Start(t, ca)
	spire.SetSVIDs(ca.Issue(t, agentID, time.Hour))
	dir := settingsDir(t, spire.Addr)

	client, closer, err := hubAddOnClient(context.Background(), dir, "cluster1")
	if err != nil || client == nil {
		t.Fatalf("client %v, %v", client, err)
	}
	_ = closer.Close()

	if _, _, err := hubAddOnClient(context.Background(), t.TempDir(), "cluster1"); err == nil {
		t.Error("no error without settings")
	}

	defer func() { addOnInterface = cloudeventsaddon.ManagedClusterAddOnInterface }()
	addOnInterface = func(context.Context, *cloudeventsoptions.GenericClientOptions[*addonapiv1beta1.ManagedClusterAddOn]) (
		addonclient.Interface, error) {
		return nil, errFailed
	}
	if _, _, err := hubAddOnClient(context.Background(), dir, "cluster1"); !errors.Is(err, errFailed) {
		t.Errorf("the client's error: %v", err)
	}
}

func TestRunAgentErrors(t *testing.T) {
	defer func() { newHubAddOnClient = hubAddOnClient }()
	notSPIFFE := t.TempDir()
	incomplete := t.TempDir()
	if err := os.WriteFile(filepath.Join(incomplete, spiffeclient.KeyAgentSPIFFEID), []byte(agentID), 0o600); err != nil {
		t.Fatal(err)
	}
	complete := settingsDir(t, "unix:///nonexistent.sock")
	fakeHub := func(context.Context, string, string) (addonclient.Interface, io.Closer, error) {
		return addonfake.NewSimpleClientset(addOn()), nopCloser{}, nil
	}
	cases := []struct {
		name    string
		dir     string
		hub     func(context.Context, string, string) (addonclient.Interface, io.Closer, error)
		cancel  bool
		config  *rest.Config
		wantErr string
	}{
		{name: "a hub kubeconfig instead of SPIFFE settings", dir: notSPIFFE, wantErr: "no SPIFFE settings"},
		{name: "incomplete settings", dir: incomplete, wantErr: "SPIFFE setting"},
		{name: "no hub client", dir: complete, hub: func(context.Context, string, string) (addonclient.Interface, io.Closer, error) {
			return nil, nil, errFailed
		}, wantErr: "failed"},
		{name: "stopped before the informer synced", dir: complete, hub: fakeHub, cancel: true, wantErr: "didn't sync"},
		{name: "an unusable spoke kubeconfig", dir: complete, hub: fakeHub,
			config:  &rest.Config{Host: "https://spoke", TLSClientConfig: rest.TLSClientConfig{CAFile: "/nonexistent/ca.crt"}},
			wantErr: "ca.crt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newHubAddOnClient = hubAddOnClient
			if c.hub != nil {
				newHubAddOnClient = c.hub
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.cancel {
				cancel()
			}
			config := c.config
			if config == nil {
				config = &rest.Config{Host: "https://spoke"}
			}
			o := &AgentOptions{HubConfigDir: c.dir, ClusterName: "cluster1", AddonNamespace: "ns", Interval: time.Hour}
			if err := o.RunAgent(ctx, config); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %v, want %q", err, c.wantErr)
			}
		})
	}
}

// The agent says hello until it is stopped; a failed hello is logged and retried.
func TestRunAgentLoop(t *testing.T) {
	defer func() { newHubAddOnClient = hubAddOnClient }()
	hub := addonfake.NewSimpleClientset(addOn())
	newHubAddOnClient = func(context.Context, string, string) (addonclient.Interface, io.Closer, error) {
		return hub, nopCloser{}, nil
	}
	spoke := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "spoke down", http.StatusInternalServerError)
	}))
	defer spoke.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		o := &AgentOptions{HubConfigDir: settingsDir(t, "unix:///s.sock"), ClusterName: "cluster1", AddonNamespace: "ns",
			Interval: 10 * time.Millisecond}
		done <- o.RunAgent(ctx, &rest.Config{Host: spoke.URL})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := hub.AddonV1beta1().ManagedClusterAddOns("cluster1").Get(ctx, helloworld_spiffe.AddonName, metav1.GetOptions{})
		if got != nil && meta.FindStatusCondition(got.Status.Conditions, helloworld_spiffe.ConditionHello) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("RunAgent: %v", err)
	}
}

func listerFor(t *testing.T, objs ...*addonapiv1beta1.ManagedClusterAddOn) (*addonfake.Clientset,
	addoninformers.SharedInformerFactory) {
	t.Helper()
	runtimeObjs := make([]runtime.Object, 0, len(objs))
	for _, o := range objs {
		runtimeObjs = append(runtimeObjs, o)
	}
	hub := addonfake.NewSimpleClientset(runtimeObjs...)
	f := addoninformers.NewSharedInformerFactory(hub, 0)
	for _, o := range objs {
		if err := f.Addon().V1beta1().ManagedClusterAddOns().Informer().GetStore().Add(o); err != nil {
			t.Fatal(err)
		}
	}
	return hub, f
}

func TestSayHello(t *testing.T) {
	o := &AgentOptions{ClusterName: "cluster1", AddonNamespace: "ns"}
	hub, f := listerFor(t, addOn())
	lister := f.Addon().V1beta1().ManagedClusterAddOns().Lister()
	spoke := kubefake.NewSimpleClientset()

	// First hello creates the ConfigMap, the second updates it; the hub keeps the other conditions.
	for i := 0; i < 2; i++ {
		if err := o.sayHello(context.TODO(), hub, lister, spoke, agentID); err != nil {
			t.Fatal(err)
		}
	}
	cm, err := spoke.CoreV1().ConfigMaps("ns").Get(context.TODO(), HelloConfigMap, metav1.GetOptions{})
	if err != nil || cm.Data["agent"] != agentID {
		t.Fatalf("ConfigMap %+v, %v", cm, err)
	}
	got, _ := hub.AddonV1beta1().ManagedClusterAddOns("cluster1").Get(context.TODO(), helloworld_spiffe.AddonName, metav1.GetOptions{})
	if meta.FindStatusCondition(got.Status.Conditions, helloworld_spiffe.ConditionHello) == nil ||
		meta.FindStatusCondition(got.Status.Conditions, "Available") == nil {
		t.Errorf("conditions %+v", got.Status.Conditions)
	}
}

func TestSayHelloErrors(t *testing.T) {
	o := &AgentOptions{ClusterName: "cluster1", AddonNamespace: "ns"}
	failOn := func(c *clienttesting.Fake, verb, resource string) {
		c.PrependReactor(verb, resource, func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errFailed
		})
	}
	cases := []struct {
		name    string
		listed  bool
		prepare func(hub *addonfake.Clientset, spoke *kubefake.Clientset)
		want    string
	}{
		{name: "the add-on isn't on the hub", want: "get the ManagedClusterAddOn"},
		{name: "the hub refuses the status", listed: true, prepare: func(hub *addonfake.Clientset, _ *kubefake.Clientset) {
			failOn(&hub.Fake, "patch", "managedclusteraddons")
		}, want: "update the ManagedClusterAddOn status"},
		{name: "the ConfigMap can't be read", listed: true, prepare: func(_ *addonfake.Clientset, spoke *kubefake.Clientset) {
			failOn(&spoke.Fake, "get", "configmaps")
		}, want: "write ConfigMap"},
		{name: "the ConfigMap can't be created", listed: true, prepare: func(_ *addonfake.Clientset, spoke *kubefake.Clientset) {
			failOn(&spoke.Fake, "create", "configmaps")
		}, want: "write ConfigMap"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var hub *addonfake.Clientset
			var f addoninformers.SharedInformerFactory
			if c.listed {
				hub, f = listerFor(t, addOn())
			} else {
				hub, f = listerFor(t)
			}
			spoke := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}})
			if c.prepare != nil {
				c.prepare(hub, spoke)
			}
			err := o.sayHello(context.TODO(), hub, f.Addon().V1beta1().ManagedClusterAddOns().Lister(), spoke, agentID)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v, want %q", err, c.want)
			}
		})
	}
}
