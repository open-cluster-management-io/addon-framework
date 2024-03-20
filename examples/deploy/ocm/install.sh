#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}


BUILD_DIR="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
DEPLOY_DIR="$(dirname "$BUILD_DIR")"
EXAMPLE_DIR="$(dirname "$DEPLOY_DIR")"
REPO_DIR="$(dirname "$EXAMPLE_DIR")"
WORK_DIR="${REPO_DIR}/_output"
CLUSTERADM="${WORK_DIR}/bin/clusteradm"

export PATH=$PATH:${WORK_DIR}/bin

echo "############  Download clusteradm"
mkdir -p "${WORK_DIR}/bin"
wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_${GOHOSTOS}_${GOHOSTARCH}.tar.gz | sudo tar -xvz -C ${WORK_DIR}/bin/
chmod +x "${CLUSTERADM}"

echo "############ Init hub"
${CLUSTERADM} init --wait --bundle-version=latest
joincmd=$(${CLUSTERADM} get token | grep clusteradm)

echo "############ Init agent as cluster1"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --bundle-version=latest | sed "s/<cluster_name>/${MANAGED_CLUSTER_NAME}/g")

echo "############ Accept join of cluster1"
${CLUSTERADM} accept --clusters ${MANAGED_CLUSTER_NAME} --wait

echo "############  All-in-one env is installed successfully!!"
