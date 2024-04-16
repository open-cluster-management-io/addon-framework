SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/kustomize.mk \
	lib/tmp.mk \
)

export GOHOSTOS    ?=$(shell go env GOHOSTOS)
export GOHOSTARCH  ?=$(shell go env GOHOSTARCH)

# Tools for deploy
export KUBECONFIG ?= ./.kubeconfig
export MANAGED_CLUSTER_NAME ?= hub
export HOSTED_MANAGED_CLUSTER_NAME ?= managed
export HOSTED_MANAGED_KLUSTERLET_NAME ?= managed
export HOSTED_MANAGED_KUBECONFIG_SECRET_NAME ?= e2e-hosted-managed-kubeconfig

KUBECTL?=kubectl
KUSTOMIZE_VERSION:=4.5.5
PWD=$(shell pwd)

# Image URL to use all building/pushing image targets;
EXAMPLE_IMAGE ?= addon-examples
IMAGE_REGISTRY ?= quay.io/open-cluster-management
IMAGE_TAG ?= latest
export EXAMPLE_IMAGE_NAME ?= $(IMAGE_REGISTRY)/$(EXAMPLE_IMAGE):$(IMAGE_TAG)

GIT_HOST ?= open-cluster-management.io
BASE_DIR := $(shell basename $(PWD))
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for building the image and binding it as a prerequisite to target "images".
$(call build-image,$(EXAMPLE_IMAGE),$(IMAGE_REGISTRY)/$(EXAMPLE_IMAGE):$(IMAGE_TAG),./build/Dockerfile.example,.)

verify-gocilint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.54.2
	golangci-lint run --timeout=3m --modules-download-mode vendor ./...

verify: verify-gocilint

deploy-ocm:
	examples/deploy/ocm/install.sh

deploy-ocm-cloudevents:
	examples/deploy/ocm-cloudevents/install.sh

deploy-hosted-ocm:
	examples/deploy/hosted-ocm/install.sh

deploy-busybox: ensure-kustomize
	cp examples/deploy/addon/busybox/kustomization.yaml examples/deploy/addon/busybox/kustomization.yaml.tmp
	cd examples/deploy/addon/busybox && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/busybox | $(KUBECTL) apply -f -
	mv examples/deploy/addon/busybox/kustomization.yaml.tmp examples/deploy/addon/busybox/kustomization.yaml

deploy-helloworld: ensure-kustomize
	cp examples/deploy/addon/helloworld/kustomization.yaml examples/deploy/addon/helloworld/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld/kustomization.yaml.tmp examples/deploy/addon/helloworld/kustomization.yaml

deploy-helloworld-cloudevents: ensure-kustomize
	cp examples/deploy/addon/helloworld-cloudevents/kustomization.yaml examples/deploy/addon/helloworld-cloudevents/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-cloudevents && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-cloudevents | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-cloudevents/kustomization.yaml.tmp examples/deploy/addon/helloworld-cloudevents/kustomization.yaml

deploy-helloworld-helm: ensure-kustomize
	cp examples/deploy/addon/helloworld-helm/kustomization.yaml examples/deploy/addon/helloworld-helm/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-helm && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-helm | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-helm/kustomization.yaml.tmp examples/deploy/addon/helloworld-helm/kustomization.yaml

deploy-helloworld-hosted: ensure-kustomize
	cp examples/deploy/addon/helloworld-hosted/kustomization.yaml examples/deploy/addon/helloworld-hosted/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-hosted && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-hosted | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-hosted/kustomization.yaml.tmp examples/deploy/addon/helloworld-hosted/kustomization.yaml

deploy-helloworld-template: ensure-kustomize
	$(KUBECTL) create namespace $(MANAGED_CLUSTER_NAME) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	cp examples/deploy/addon/helloworld-template/kustomization.yaml examples/deploy/addon/helloworld-template/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-template && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-template | sed -e "s,cluster1,$(MANAGED_CLUSTER_NAME)," | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-template/kustomization.yaml.tmp examples/deploy/addon/helloworld-template/kustomization.yaml

deploy-kubernetes-dashboard: ensure-kustomize
	$(KUBECTL) create namespace $(MANAGED_CLUSTER_NAME) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	cp examples/deploy/addon/kubernetes-dashboard/kustomization.yaml examples/deploy/addon/kubernetes-dashboard/kustomization.yaml.tmp
	cd examples/deploy/addon/kubernetes-dashboard && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/kubernetes-dashboard | sed -e "s,cluster1,$(MANAGED_CLUSTER_NAME)," | $(KUBECTL) apply -f -
	mv examples/deploy/addon/kubernetes-dashboard/kustomization.yaml.tmp examples/deploy/addon/kubernetes-dashboard/kustomization.yaml

undeploy-addon:
	$(KUBECTL) delete -f examples/deploy/addon/helloworld-hosted/resources/helloworld_hosted_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/helloworld-helm/resources/helloworld_helm_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/helloworld/resources/helloworld_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/busybox/resources/busybox_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/helloworld-template/resources/cluster_management_addon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/kubernetes-dashboard/resources/cluster_management_addon.yaml --ignore-not-found

undeploy-busybox: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/busybox | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld-cloudevents: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-cloudevents | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld-helm: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-helm | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld-hosted: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-hosted | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld-template: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-template | $(KUBECTL) delete --ignore-not-found -f -

undeploy-kubernetes-dashboard: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/kubernetes-dashboard | $(KUBECTL) delete --ignore-not-found -f -

build-e2e:
	go test -c ./test/e2e

test-e2e: build-e2e deploy-ocm deploy-helloworld deploy-helloworld-helm
	./e2e.test -test.v -ginkgo.v

build-e2e-cloudevents:
	go test -c ./test/e2ecloudevents

test-e2e-cloudevents: build-e2e-cloudevents deploy-ocm-cloudevents deploy-helloworld-cloudevents
	./e2ecloudevents.test -test.v -ginkgo.v

build-hosted-e2e:
	go test -c ./test/e2ehosted

test-hosted-e2e: build-hosted-e2e deploy-hosted-ocm deploy-helloworld-hosted
	./e2ehosted.test -test.v -ginkgo.v

include ./test/integration-test.mk
