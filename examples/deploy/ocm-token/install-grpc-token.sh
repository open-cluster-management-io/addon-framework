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

echo "############ Waiting for 5 pods in open-cluster-management-hub to be running (timeout 2m)..."
TIMEOUT=120
INTERVAL=5
ELAPSED=0
while true; do
  RUNNING_COUNT=$(${KUBECTL} get pods -n open-cluster-management-hub --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l | tr -d ' ')
  if [ "$RUNNING_COUNT" -ge 5 ] 2>/dev/null; then
    echo "############ All 5 pods in open-cluster-management-hub are running."
    break
  fi
  if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "############ Timed out waiting for pods in open-cluster-management-hub to be running. Running: ${RUNNING_COUNT}/5"
    ${KUBECTL} get pods -n open-cluster-management-hub
    exit 1
  fi
  echo "Waiting for pods... (${RUNNING_COUNT}/5 running, ${ELAPSED}s/${TIMEOUT}s)"
  sleep $INTERVAL
  ELAPSED=$((ELAPSED + INTERVAL))
done

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

echo "############ Waiting for managed cluster ${MANAGED_CLUSTER_NAME} to become available (timeout 2m)..."
TIMEOUT=120
INTERVAL=5
ELAPSED=0
while true; do
  STATUS=$(${KUBECTL} get managedcluster ${MANAGED_CLUSTER_NAME} -o jsonpath='{range .status.conditions[*]}{.type}{" "}{.status}{"\n"}{end}' 2>/dev/null | grep "ManagedClusterConditionAvailable" | awk '{print $2}')
  if [ "$STATUS" = "True" ]; then
    echo "############ Managed cluster ${MANAGED_CLUSTER_NAME} is available."
    break
  fi
  if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "############ Timed out waiting for managed cluster ${MANAGED_CLUSTER_NAME} to become available."
    ${KUBECTL} version
    ${KUBECTL} get managedcluster -o yaml
    ${KUBECTL} get pods -A
    ${KUBECTL} get pods -n open-cluster-management-agent | grep klusterlet-registration-agent | awk '{print $1}' | xargs ${KUBECTL} logs -n open-cluster-management-agent
    exit 1
  fi
  echo "Waiting... (${ELAPSED}s/${TIMEOUT}s)"
  sleep $INTERVAL
  ELAPSED=$((ELAPSED + INTERVAL))
done

echo "############  All-in-one env is installed successfully!!"
echo "############  Managed cluster name: ${MANAGED_CLUSTER_NAME}"
echo "############  Cluster registration driver: gRPC"
echo "############  Addon registration driver: Token"
