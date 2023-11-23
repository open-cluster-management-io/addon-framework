
# Getting Started

We have several AddOn examples for user to understand how Addon works and how to develop an AddOn.

- the [helloworld example](helloworld) is implemented using Go templates
- the [helloworld_helm example](helloworld_helm) is implemented using Helm Chart.
- the [helloworld_hosted example](helloworld_hosted) is implemented using Go templateds, and support running the agent
  deployment on a hosting cluster cluster.
- the [helloworld-template example](deploy/addon/helloworld-template) is implemented using the AddOnTemplate API, it
  is managed by the global addon-manager, so there is no dedicated addon-manager pod running on the hub cluster for it.
- the [kubernetes-dashboard](deploy/addon/kubernetes-dashboard) is another addon implemented using the AddOnTemplate API
  to install [a kubernetes dashboard](https://kubernetes.io/docs/tasks/access-application-cluster/web-ui-dashboard/)
  for a managed cluster.

You can get more details in the [docs](../docs).

## Prerequisites

These instructions assume:

- You have at least one running kubernetes cluster;
- You have already followed instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) and installed OCM successfully;
- At least one managed cluster has been imported and accepted;

## Deploy the example AddOns
Set environment variables.
```sh
export KUBECONFIG=</path/to/hub_cluster/kubeconfig>
```

Build the docker image to run the sample AddOn.
```sh
# get imagebuilder first
go get github.com/openshift/imagebuilder/cmd/imagebuilder@v1.2.3
export PATH=$PATH:$(go env GOPATH)/bin
# build image
make images
export EXAMPLE_IMAGE_NAME=<addon_image_name> # export EXAMPLE_IMAGE_NAME=quay.io/open-cluster-management/addon-examples:latest
```

If you are using kind, load image into kind hub cluster.
```sh
kind load docker-image $EXAMPLE_IMAGE_NAME --name <your-hub-cluster-name> # kind load docker-image  $EXAMPLE_IMAGE_NAME --name cluster1
```

And then deploy the example AddOns controller on hub cluster.
```sh
make deploy-helloworld
make deploy-helloworld-helm
make deploy-helloworld-hosted
```

**helloworld addon**

The helloworld AddOn controller will create one `ManagedClusterAddOn` for each managed cluster automatically to install
the helloworld agent on the managed cluster.

After a successful deployment, check on the managed cluster and see the helloworld AddOn agent has been deployed from
the hub cluster.
```sh
kubectl --kubeconfig </path/to/managed_cluster/kubeconfig> -n default get pods
NAME                               READY   STATUS    RESTARTS   AGE
helloworld-agent-b99d47f76-v2j6h   1/1     Running   0          53s
```

**helloworld_helm addon**

The helloworld_helm AddOn controller cannot create `ManagedClusterAddOn` automatically.

We can create a `ManagedClusterAddOn` in the managedCluster namespace on the Hub cluster to enable the installation of
the AddOn on the managed cluster.
```sh
export MANAGED_CLUSTER_NAME=<managed-cluster-name> && \
sed -e "s,cluster1,$MANAGED_CLUSTER_NAME," examples/deploy/addon-cr/helloworld_helm_addon_cr.yaml | \
kubectl apply -f -
```

We can check the helloworld_helm AddOn agent is deployed in the `installNamespace` on the managed cluster. 

**helloworld_hosted addon**

The helloworld_hosted AddOn controller also cannot create `ManagedClusterAddOn` automatically.

We can create a `ManagedClusterAddOn` in the managedCluster namespace on the Hub cluster to enable the installation of
the AddOn on the managed cluster.

Note: when installing the addon in Hosted mode, the klusterlet installation mode of the managed cluster should also be
in Hosted mode. here we should specify the HOSTING_CLUSTER_NAME, it should be a managed cluster of the hub and the same
hosting cluster of the klustelet.

```sh
export MANAGED_CLUSTER_NAME=<managed-cluster-name> && \
export HOSTING_CLUSTER_NAME=<hosting-cluster-name> && \
sed -e "s,cluster1,$MANAGED_CLUSTER_NAME," -e "s,hosting1,$HOSTING_CLUSTER_NAME," examples/deploy/addon-cr/helloworld_hosted_addon_cr.yaml | \
kubectl apply -f -
```

Then create a `helloworldhosted-managed-kubeconfig` secret containing the kubeconfig of the managed cluster in the
`installNamespace` on the hosting cluster:

```sh
oc create secret generic helloworldhosted-managed-kubeconfig -n <installNamespace> --from-file=kubeconfig=<managed-cluster-kubeconfig-file>
```

We can check the helloworld_hosted AddOn agent is deployed in the `installNamespace` on the hosting cluster.

**helloworld_template addon**

```sh
MANAGED_CLUSTER_NAME=<managed-cluster-name> make deploy-helloworld-template
```

**kubernetes-dashboard addon**

```sh
MANAGED_CLUSTER_NAME=<managed-cluster-name> make deploy-kubernetes-dashboard
```

## Configure the example add-ons

The helloworld add-on supports configuring the nodeSelector and tolerations for its agent with `AddOnDeploymentConfig` and the helloworld_helm add-on supports configuring image and imagePullPolicy for its agent with `ConfigMap` and also supports configuring node selector and tolerations for its agent.

## Congfig the helloworld add-on

The helloworld add-on supported configuration types can be listed from the `supportedConfigs` field in its `ClusterManagementAddOn`
```sh
kubectl get clustermanagementaddons helloworld -ojsonpath='{.spec.supportedConfigs}'
```

To configure the helloworld add-on agent

1. Create a `AddOnDeploymentConfig` with nodeSelector and tolerations, there is an example in `examples/deploy/addon-config/addondeploymentconfig.yaml`

2. Apply the `AddOnDeploymentConfig` to a namespace, for example, to our managed cluster namespace
```sh
kubectl -n cluster1 apply -f examples/deploy/addon-config/addondeploymentconfig.yaml
```

3. Reference this configuration to helloworld add-on, for example
```sh
kubectl -n cluster1 patch managedclusteraddons helloworld --type='json' -p='[{\"op\":\"add\", \"path\":\"/spec/configs\", \"value\":[{\"group\":\"addon.open-cluster-management.io\",\"resource\":\"addondeploymentconfigs\",\"namespace\":\"cluster1\",\"name\":\"deploy-config\"}]}]'
```

Then the helloworld add-on agent will be configured with this configuration.

## Congfig the helloworld_helm add-on

The helloworld_helm add-on supported configuration types can be listed from the `supportedConfigs` field in its `ClusterManagementAddOn`
```sh
kubectl get clustermanagementaddons helloworldhelm -ojsonpath='{.spec.supportedConfigs}'
```

To configure the helloworld_helm add-on agent

1. Create a `AddOnDeploymentConfig` with nodeSelector and tolerations, there is an example in `examples/deploy/addon-config/addondeploymentconfig.yaml`

2. Create a `ConfigMap` with image and imagePullPolicy, there is an example in `examples/deploy/addon-config/configmap.yaml`

2. Apply these two configurations to a namespace, for example, to our managed cluster namespace
```sh
kubectl -n cluster1 apply -f examples/deploy/addon-config/addondeploymentconfig.yaml
kubectl -n cluster1 apply -f examples/deploy/addon-config/configmap.yaml
```

3. Reference these two configurations to helloworld_helm add-on, for example
```sh
kubectl -n cluster1 patch managedclusteraddons helloworldhelm --type='json' -p='[{\"op\":\"add\", \"path\":\"/spec/configs\", \"value\":[{\"group\":\"addon.open-cluster-management.io\",\"resource\":\"addondeploymentconfigs\",\"namespace\":\"cluster1\",\"name\":\"deploy-config\"},{\"resource\":\"configmaps\",\"namespace\":\"cluster1\",\"name\":\"image-config\"}]}]'
```

Then the helloworld_helm add-on agent will be configured with these two configurations.

## Clean up
Undeploy managedClusterAddons firstly.
```sh
make undeploy-addon
```

Undeploy example AddOn controllers from hub cluster after all managedClusterAddons are deleted.
```sh
make undeploy-helloworld
make undeploy-helloworld-helm
make undeploy-helloworld-hosted
make undeploy-helloworld-template
make undeploy-kubernetes-dashboard
```

Remove the AddOn CR from hub cluster. It will undeploy the AddOn agent from the managed cluster as well.
```sh
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> delete managedclusteraddons -n <managed_cluster_name> helloworld
```

Follow instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) to uninstall OCM if necessary;
