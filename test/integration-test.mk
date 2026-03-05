TEST_TMP :=/tmp

export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin

K8S_VERSION ?=1.30.0
SETUP_ENVTEST := $(shell go env GOPATH)/bin/setup-envtest

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
ifeq "" "$(wildcard $(SETUP_ENVTEST))"
	$(info Installing setup-envtest into '$(SETUP_ENVTEST)')
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.22
endif
	ENVTEST_K8S_PATH=$$($(SETUP_ENVTEST) use $(K8S_VERSION) --bin-dir $(KUBEBUILDER_ASSETS) -p path); \
	if [ -z "$$ENVTEST_K8S_PATH" ]; then \
		echo "Error: setup-envtest returned empty path"; \
		exit 1; \
	fi; \
	if [ ! -d "$$ENVTEST_K8S_PATH" ]; then \
		echo "Error: setup-envtest path does not exist: $$ENVTEST_K8S_PATH"; \
		exit 1; \
	fi; \
	cp -r $$ENVTEST_K8S_PATH/* $(KUBEBUILDER_ASSETS)/
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif
.PHONY: ensure-kubebuilder-tools

clean-integration-test:
	rm -rf $(TEST_TMP)/kubebuilder
	$(RM) ./integration.test
	$(RM) ./kube-integration.test
	$(RM) ./cloudevents-integration.test
	@if [ -f "$(TEST_TMP)/managedclusteraddons.crd.yaml.backup" ]; then \
		echo "Cleaning up CRD backup..."; \
		rm -f "$(TEST_TMP)/managedclusteraddons.crd.yaml.backup"; \
	fi
.PHONY: clean-integration-test

clean: clean-integration-test

update-crd-storage-version:
	@echo "Updating CRD storage version to v1beta1 since no conversion webhook in integration test..."
	@bash hack/fix-crd-storage-version.sh
.PHONY: update-crd-storage-version

restore-crd-storage-version:
	@echo "Restoring original CRD..."
	@if [ -f "$(TEST_TMP)/managedclusteraddons.crd.yaml.backup" ]; then \
		cp "$(TEST_TMP)/managedclusteraddons.crd.yaml.backup" \
		   "vendor/open-cluster-management.io/api/addon/v1beta1/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml"; \
		rm -f "$(TEST_TMP)/managedclusteraddons.crd.yaml.backup"; \
		echo "✓ CRD restored and backup removed"; \
	else \
		echo "✓ No backup found, CRD already in original state"; \
	fi
.PHONY: restore-crd-storage-version

test-kube-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/kube -o ./kube-integration.test
	./kube-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-kube-integration

test-cloudevents-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/cloudevents -o ./cloudevents-integration.test
	./cloudevents-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-cloudevents-integration

test-integration: update-crd-storage-version test-kube-integration
	@$(MAKE) restore-crd-storage-version
.PHONY: test-integration
