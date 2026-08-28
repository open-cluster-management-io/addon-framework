# Hosted mode

Hosted mode runs an add-on agent on a hosting cluster while the `ManagedClusterAddOn` remains in
the namespace of the cluster that the add-on manages. The add-on implementation must opt in:

```go
agent.AgentAddonOptions{
    HostedModeEnabled:  true,
    HostedModeInfoFunc: constants.GetHostedModeInfo,
}
```

Each resource manifest returned by the `AgentAddon` that must run on the hosting cluster must set
`addon.open-cluster-management.io/hosted-manifest-location: hosting`. Auto-discovery selects the
host; it does not opt in the add-on or mark its manifests.

## Configure automatic host discovery

Auto-discovery is disabled by default. Enable it for an add-on type on its
`ClusterManagementAddOn`:

```yaml
apiVersion: addon.open-cluster-management.io/v1beta1
kind: ClusterManagementAddOn
metadata:
  name: example
spec:
  hostedModeAutoDiscovery:
    mode: Enable
```

The target klusterlet must also report where its controllers run. For a klusterlet in Default or
Singleton mode, the reported value is `spec.clusterName`. For a klusterlet in Hosted or
SingletonHosted mode, set `deployOption.hosted.managementClusterName` to the name of the
`ManagedCluster` that hosts the agents:

```yaml
apiVersion: operator.open-cluster-management.io/v1
kind: Klusterlet
metadata:
  name: klusterlet
spec:
  clusterName: managed-cluster
  deployOption:
    mode: Hosted
    reportHostingCluster: Enable
    hosted:
      managementClusterName: hosting-cluster
```

Finally, request Hosted mode on the `ManagedClusterAddOn`:

```yaml
apiVersion: addon.open-cluster-management.io/v1beta1
kind: ManagedClusterAddOn
metadata:
  name: example
  namespace: managed-cluster
  annotations:
    addon.open-cluster-management.io/install-mode: Hosted
```

Discovery waits until all of the following are true:

1. The target reports a non-empty `hosting-cluster.open-cluster-management.io` ClusterClaim.
2. The reported hosting cluster is a ManagedCluster of the same hub.
3. The target and hosting clusters are selected by the same existing ManagedClusterSet.

The `default` and `global` ManagedClusterSets are not accepted as the security guard. A label that
names a set is also insufficient when the corresponding ManagedClusterSet object does not exist.

## Placement lifecycle

Discovery is deliberately one-shot. After the framework resolves a hosting cluster, later
ClusterClaim or ManagedClusterSet changes do not move the add-on. If the target later reports a
different host, the framework sets the `HostingClusterValidity=False` condition with reason
`HostingClusterMismatch`, while an existing deployment continues to run on the resolved host.

To resolve the add-on again after its hosting cluster changes, delete the `ManagedClusterAddOn`,
wait for its pre-delete hook and hosted ManifestWorks to finish cleanup, and then recreate it.

## Controller permissions

An add-on manager using auto-discovery needs these additional hub permissions:

```yaml
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclusters", "managedclustersets"]
  verbs: ["get", "list", "watch"]
```

The feature follows
[KEP-188: Hosted add-on follows klusterlet](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/188-hosted-addon-follow-klusterlet).
