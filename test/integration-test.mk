ENSURE_ENVTEST_SCRIPT := https://raw.githubusercontent.com/open-cluster-management-io/sdk-go/main/ci/envtest/ensure-envtest.sh

.PHONY: envtest-setup
envtest-setup:
	$(eval export KUBEBUILDER_ASSETS=$(shell curl -fsSL $(ENSURE_ENVTEST_SCRIPT) | bash))
	@echo "KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)"

clean-integration-test:
	rm -rf $(TEST_TMP)/kubebuilder
	$(RM) ./integration.test
	$(RM) ./kube-integration.test
	$(RM) ./cloudevents-integration.test
	$(RM) ./v1alpha1-kube-integration.test
	$(RM) ./v1alpha1-cloudevents-integration.test
.PHONY: clean-integration-test

clean: clean-integration-test

test-kube-integration: envtest-setup
	go test -c ./test/integration/kube -o ./kube-integration.test
	./kube-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-kube-integration

test-cloudevents-integration: envtest-setup
	go test -c ./test/integration/cloudevents -o ./cloudevents-integration.test
	./cloudevents-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-cloudevents-integration

test-v1alpha1-kube-integration: envtest-setup
	go test -c ./test/integration/v1alpha1_kube -o ./v1alpha1-kube-integration.test
	./v1alpha1-kube-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-v1alpha1-kube

test-v1alpha1-cloudevents-integration: envtest-setup
	go test -c ./test/integration/v1alpha1_cloudevents -o ./v1alpha1-cloudevents-integration.test
	./v1alpha1-cloudevents-integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-v1alpha1-cloudevents

test-integration: test-kube-integration test-cloudevents-integration test-v1alpha1-kube-integration test-v1alpha1-cloudevents-integration
.PHONY: test-integration
