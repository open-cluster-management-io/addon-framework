package addonconfiguration

import (
	"k8s.io/apimachinery/pkg/util/sets"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

// configurationGraph is a snapshot graph on the configuration of addons
type configurationGraph struct {
	// placementNodes maintains a list between a placement and its related mcas
	placementNodes []*placementNode
	// defaults are the addons that not defined in any placementNode
	defaults *placementNode
}

// placementNode is a node in configurationGraph defined by a placement
type placementNode struct {
	desiredConfigs addonConfigMap
	addons         map[string]*addonNode
	clusters       sets.String
}

// addonNode is a child of placementNode
type addonNode struct {
	desiredConfigs addonConfigMap
	mca            *addonv1alpha1.ManagedClusterAddOn
}

type addonConfigMap map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference

func (d addonConfigMap) copy() addonConfigMap {
	ouput := addonConfigMap{}
	for k, v := range d {
		ouput[k] = v
	}
	return ouput
}

func newGraph(supportedConfigs []addonv1alpha1.ConfigMeta) *configurationGraph {
	graph := &configurationGraph{
		placementNodes: []*placementNode{},
		defaults: &placementNode{
			desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{},
			addons:         map[string]*addonNode{},
		},
	}

	for _, config := range supportedConfigs {
		if config.DefaultConfig != nil {
			graph.defaults.desiredConfigs[config.ConfigGroupResource] = addonv1alpha1.ConfigReference{
				ConfigGroupResource: config.ConfigGroupResource,
				ConfigReferent: addonv1alpha1.ConfigReferent{
					Name:      config.DefaultConfig.Name,
					Namespace: config.DefaultConfig.Namespace,
				},
			}
		}
	}

	return graph
}

// addAddonNode to the graph, starting from placement with the highest order
func (g *configurationGraph) addAddonNode(mca *addonv1alpha1.ManagedClusterAddOn) {
	for i := len(g.placementNodes) - 1; i >= 0; i-- {
		if g.placementNodes[i].clusters.Has(mca.Namespace) {
			g.placementNodes[i].addNode(mca)
			return
		}
	}

	g.defaults.addNode(mca)
}

// addNode delete clusters on existing graph so the new configuration overrides the previous
func (g *configurationGraph) addPlacementNode(configs []addonv1alpha1.AddOnConfig, clusters []string) {
	node := &placementNode{
		desiredConfigs: g.defaults.desiredConfigs,
		addons:         map[string]*addonNode{},
		clusters:       sets.NewString(clusters...),
	}

	// overrides configuration by install strategy
	if len(configs) > 0 {
		node.desiredConfigs = node.desiredConfigs.copy()
		for _, config := range configs {
			node.desiredConfigs[config.ConfigGroupResource] = addonv1alpha1.ConfigReference{
				ConfigGroupResource: config.ConfigGroupResource,
				ConfigReferent:      config.ConfigReferent,
			}
		}
	}

	// remove addon in defaults and other placements.
	for _, cluster := range clusters {
		if _, ok := g.defaults.addons[cluster]; ok {
			node.addNode(g.defaults.addons[cluster].mca)
			delete(g.defaults.addons, cluster)
		}
		for _, placement := range g.placementNodes {
			if _, ok := placement.addons[cluster]; ok {
				node.addNode(placement.addons[cluster].mca)
				delete(placement.addons, cluster)
			}
		}
	}
	g.placementNodes = append(g.placementNodes, node)
}

func (g *configurationGraph) addonToUpdate() []*addonNode {
	var addons []*addonNode
	for _, placement := range g.placementNodes {
		addons = append(addons, placement.addonToUpdate()...)
	}

	addons = append(addons, g.defaults.addonToUpdate()...)

	return addons
}

func (n *placementNode) addNode(addon *addonv1alpha1.ManagedClusterAddOn) {
	n.addons[addon.Namespace] = &addonNode{
		mca:            addon,
		desiredConfigs: n.desiredConfigs,
	}

	// override configuration by mca spec
	if len(addon.Spec.Configs) > 0 {
		n.addons[addon.Namespace].desiredConfigs = n.addons[addon.Namespace].desiredConfigs.copy()
		// TODO we should also filter out the configs which are not supported configs.
		for _, config := range addon.Spec.Configs {
			n.addons[addon.Namespace].desiredConfigs[config.ConfigGroupResource] = addonv1alpha1.ConfigReference{
				ConfigGroupResource: config.ConfigGroupResource,
				ConfigReferent:      config.ConfigReferent,
			}
		}
	}
}

// addonToUpdate finds the addons to be updated by placement
func (n *placementNode) addonToUpdate() []*addonNode {
	var addons []*addonNode

	for _, addon := range n.addons {
		addons = append(addons, addon)
	}

	return addons
}
