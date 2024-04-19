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

echo "############  Deploy mqtt broker"
${KUBECTL} apply -f ${DEPLOY_DIR}/mqtt/mqtt-broker.yaml

echo "############  Configure the work-agent"
${KUBECTL} -n open-cluster-management scale --replicas=0 deployment/klusterlet

cat << EOF | ${KUBECTL} -n open-cluster-management-agent apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: work-driver-config
stringData:
  config.yaml: |
    brokerHost: mosquitto.mqtt:1883
    topics:
      sourceEvents: sources/addon-manager/clusters/${MANAGED_CLUSTER_NAME}/sourceevents
      agentEvents: sources/addon-manager/clusters/${MANAGED_CLUSTER_NAME}/agentevents
      agentBroadcast: clusters/${MANAGED_CLUSTER_NAME}/agentbroadcast
EOF

# patch klusterlet-work-agent deployment to use mqtt as workload source driver
${KUBECTL} -n open-cluster-management-agent patch deployment/klusterlet-work-agent --type=json \
    -p='[
        {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--cloudevents-client-codecs=manifestbundle"},
        {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--cloudevents-client-id=work-agent-$(POD_NAME)"},
        {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--workload-source-driver=mqtt"},
        {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--workload-source-config=/var/run/secrets/hub/config.yaml"}
    ]'

${KUBECTL} -n open-cluster-management-agent patch deployment/klusterlet-work-agent --type=json \
    -p='[{"op": "add", "path": "/spec/template/spec/volumes/-", "value": {"name": "workdriverconfig","secret": {"secretName": "work-driver-config"}}}]'

${KUBECTL} -n open-cluster-management-agent patch deployment/klusterlet-work-agent --type=json \
    -p='[{"op": "add", "path": "/spec/template/spec/containers/0/volumeMounts/-", "value": {"name": "workdriverconfig","mountPath": "/var/run/secrets/hub"}}]'

${KUBECTL} -n open-cluster-management-agent scale --replicas=1 deployment/klusterlet-work-agent
${KUBECTL} -n open-cluster-management-agent rollout status deployment/klusterlet-work-agent --timeout=120s
${KUBECTL} -n open-cluster-management-agent get pod -l app=klusterlet-manifestwork-agent

# TODO: add live probe for the work-agent to check if it is connected to the mqtt broker
isRunning=false
for i in {1..20}; do
    if ${KUBECTL} -n open-cluster-management-agent logs deployment/klusterlet-work-agent | grep "subscribing to topics"; then
        echo "klusterlet-work-agent is subscribing to topics from mqtt broker"
        isRunning=true
        break
    fi
    sleep 12
done

if [ "$isRunning" = false ]; then
    echo "timeout waiting for klusterlet-work-agent to subscribe to topics from mqtt broker"
    exit 1
fi
