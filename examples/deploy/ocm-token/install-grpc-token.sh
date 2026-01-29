#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}


BUILD_DIR="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
DEPLOY_DIR="$(dirname "$BUILD_DIR")"
EXAMPLE_DIR="$(dirname "$DEPLOY_DIR")"
REPO_DIR="$(dirname "$EXAMPLE_DIR")"
WORK_DIR="${REPO_DIR}/_output"
CLUSTERADM="clusteradm"

export PATH=$PATH:${WORK_DIR}/bin

echo "############  Install clusteradm"
go install open-cluster-management.io/clusteradm/cmd/clusteradm@main

echo "############ Init hub with gRPC registration driver and auto-approval"
${CLUSTERADM} init \
  --wait \
  --bundle-version=latest \
  --registration-drivers="csr,grpc" \
  --grpc-server="cluster-manager-grpc-server.open-cluster-management-hub.svc:8090" \
  --feature-gates=ManagedClusterAutoApproval=true \
  --auto-approved-grpc-identities="system:serviceaccount:open-cluster-management:agent-registration-bootstrap"

# Get the join token and hub apiserver
joincmd=$(${CLUSTERADM} get token | grep clusteradm)
token=$(echo ${joincmd} | sed -n 's/.*--hub-token \([^ ]*\).*/\1/p')
hubapi=$(echo ${joincmd} | sed -n 's/.*--hub-apiserver \([^ ]*\).*/\1/p')

echo "############ Join hub to itself as managed cluster ${MANAGED_CLUSTER_NAME} using gRPC driver and token addon driver"
${CLUSTERADM} join \
  --hub-token ${token} \
  --hub-apiserver ${hubapi} \
  --cluster-name ${MANAGED_CLUSTER_NAME} \
  --registration-auth grpc \
  --grpc-server "cluster-manager-grpc-server.open-cluster-management-hub.svc:8090" \
  --addon-kubeclient-registration-auth token \
  --force-internal-endpoint-lookup \
  --wait \
  --bundle-version=latest

echo "############ Cluster auto-approved, checking status..."
${KUBECTL} get managedcluster ${MANAGED_CLUSTER_NAME}

echo "############  All-in-one env is installed successfully!!"
echo "############  Managed cluster name: ${MANAGED_CLUSTER_NAME}"
echo "############  Cluster registration driver: gRPC"
echo "############  Addon registration driver: Token"
