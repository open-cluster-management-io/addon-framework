TEST_TMP :=/tmp

export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin

K8S_VERSION ?=1.30.0
KB_TOOLS_ARCHIVE_NAME :=kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := $(TEST_TMP)/$(KB_TOOLS_ARCHIVE_NAME)

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif
.PHONY: ensure-kubebuilder-tools

clean-integration-test:
	$(RM) '$(KB_TOOLS_ARCHIVE_PATH)'
	rm -rf $(TEST_TMP)/kubebuilder
	$(RM) ./integration.test
.PHONY: clean-integration-test

clean: clean-integration-test

test-kube-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/kube -o ./kube-integration.test
	./kube-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-kube-integration

test-cloudevents-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/cloudevents -o ./cloudevents-integration.test
	./cloudevents-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-cloudevents-integration

test-integration: test-kube-integration
.PHONY: test-integration
