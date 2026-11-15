package agentdeploy

import (
	"testing"
	"time"

	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

// The deploy controller renders every add-on through WithSPIFFE, so its manifests are adapted on
// clusters whose add-ons use the spiffe driver.
func TestNewAddonDeployControllerWrapsAddOnsWithSPIFFE(t *testing.T) {
	addonClient := fakeaddon.NewSimpleClientset()
	workClient := fakework.NewSimpleClientset()
	clusters := clusterinformers.NewSharedInformerFactory(fakecluster.NewSimpleClientset(), time.Minute).Cluster().V1().ManagedClusters()
	addOns := addoninformers.NewSharedInformerFactory(addonClient, time.Minute).Addon().V1beta1().ManagedClusterAddOns()
	works := workinformers.NewSharedInformerFactory(workClient, time.Minute).Work().V1().ManifestWorks()
	if c := NewAddonDeployController(workClient, addonClient, clusters, addOns, works,
		map[string]agent.AgentAddon{"my-addon": &testAgent{name: "my-addon"}}, utils.AllowAllAddOns); c == nil {
		t.Fatal("no controller")
	}

	wrapped := withSPIFFE(map[string]agent.AgentAddon{"my-addon": &testAgent{name: "my-addon"}})
	if _, ok := wrapped["my-addon"].(interface {
		GetAgentAddonOptions() agent.AgentAddonOptions
	}); !ok || len(wrapped) != 1 {
		t.Fatalf("wrapped %v", wrapped)
	}
	if agent.WithSPIFFE(wrapped["my-addon"]) != wrapped["my-addon"] {
		t.Error("the add-on isn't wrapped with WithSPIFFE")
	}
}
