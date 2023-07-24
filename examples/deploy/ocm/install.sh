#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}

rm -rf _repo_ocm

echo "############  Cloning ocm repo"
git clone --depth 1 --branch main https://github.com/open-cluster-management-io/ocm.git _repo_ocm

cd _repo_ocm || {
  printf "cd failed, _repo_ocm does not exist"
  return 1
}

echo "############  Deploying operators"
make deploy-hub cluster-ip deploy-spoke-operator apply-spoke-cr
if [ $? -ne 0 ]; then
 echo "############  Failed to deploy"
 exit 1
fi

for i in {1..7}; do
  echo "############$i  Checking cluster-manager-registration-controller"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-hub get pods | grep cluster-manager-registration-controller | grep -c "Running")
  if [ "${RUNNING_POD}" -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the cluster-manager-registration-controller is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-hub get pods

    exit 1
  fi
  sleep 30
done

for i in {1..7}; do
  echo "############$i  Checking cluster-manager-registration-webhook"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-hub get pods | grep cluster-manager-registration-webhook | grep -c "Running")
  if [ "${RUNNING_POD}" -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the cluster-manager-registration-webhook is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-hub get pods
    exit 1
  fi
  sleep 30s
done

for i in {1..7}; do
  echo "############$i  Checking klusterlet-registration-agent"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-agent get pods | grep klusterlet-registration-agent | grep -c "Running")
  if [ ${RUNNING_POD} -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the klusterlet-registration-agent is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-agent get pods
    exit 1
  fi
  sleep 30
done

echo "############  All-in-one env is installed successfully!!"

echo "############  Cleanup"
cd ../ || exist
rm -rf _repo_ocm

echo "############  Finished installation!!!"
