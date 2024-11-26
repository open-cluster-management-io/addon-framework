package kube

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	deploymentJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default"
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`

	deploymentZeroReplicasJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment-no-replica",
			"namespace": "default"
		},
		"spec": {
			"replicas": 0,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`

	daemonSetJson = `{
		"apiVersion": "apps/v1",
		"kind": "DaemonSet",
		"metadata": {
			"name": "nginx-ds",
			"namespace": "default"
		},
		"spec": {
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`

	mchJson = `{
    "apiVersion": "operator.open-cluster-management.io/v1",
    "kind": "MultiClusterHub",
    "metadata": {
        "name": "multiclusterhub",
        "namespace": "open-cluster-management"
    },
    "spec": {
        "separateCertificateManagement": false
    }
}`
)

var _ = ginkgo.Describe("Agent deploy", func() {
	var managedClusterName string
	var err error
	var manifestWorkName string
	var cma *addonapiv1alpha1.ClusterManagementAddOn
	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		manifestWorkName = fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testAddonImpl.name))

		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cma = newClusterManagementAddon(testAddonImpl.name)
		cma, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(),
			cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			testAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent when cma is managed by self successfully", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
		}

		// Create ManagedClusterAddOn
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied", ObservedGeneration: work.Generation})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied) {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			if cond := meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing); cond != nil {
				return fmt.Errorf("expected no addon progressing condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work to available so addon becomes available
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		// set observed
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable", ObservedGeneration: work.Generation})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionAvailable) {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			if cond := meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing); cond != nil {
				return fmt.Errorf("expected no addon progressing condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// do nothing if cluster is deleting and addon is not deleted
		cluster, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		cluster.SetFinalizers([]string{"cluster.open-cluster-management.io/api-resource-cleanup"})
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(context.Background(), cluster, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		time.Sleep(5 * time.Second) // wait 5 seconds to sync
		gomega.Eventually(func() error {
			_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent and get available with prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testAddonImpl.prober = utils.NewDeploymentProber(types.NamespacedName{Name: "nginx-deployment", Namespace: "default"})

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if len(work.Spec.ManifestConfigs) != 1 {
				return fmt.Errorf("Unexpected number of work manifests configuration")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)

		// update work status to a wrong feedback status
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update to the correct condition
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
	ginkgo.It("Should deploy agents and get available with prober func including wildcard", func() {
		deploy1 := &unstructured.Unstructured{}
		err := deploy1.UnmarshalJSON([]byte(deploymentJson))
		deploy1.SetName("deployment1")
		deploy1.SetNamespace("ns1")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		deploy2 := &unstructured.Unstructured{}
		err = deploy2.UnmarshalJSON([]byte(deploymentJson))
		deploy2.SetName("deployment2")
		deploy2.SetNamespace("ns2")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{deploy1, deploy2}
		testAddonImpl.prober = utils.NewAllDeploymentsProber()

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 2 {
				return fmt.Errorf("unexpected number of work manifests")
			}

			if len(work.Spec.ManifestConfigs) != 1 {
				return fmt.Errorf("unexpected number of work manifests configuration")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)

		// update work status to a wrong feedback status
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "deployment1",
						Namespace: "ns1",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   1,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "deployment2",
						Namespace: "ns2",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionFalse,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("unexpected addon available condition, %#v", addon.Status)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update to the correct condition
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "deployment1",
						Namespace: "ns1",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   1,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "deployment2",
						Namespace: "ns2",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent and get available with deployment availability prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		objZeroReplicaDeployment := &unstructured.Unstructured{}
		err = objZeroReplicaDeployment.UnmarshalJSON([]byte(deploymentZeroReplicasJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj, objZeroReplicaDeployment}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeDeploymentAvailability,
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 2 {
				return fmt.Errorf("Unexpected number of work manifests: %d", len(work.Spec.Workload.Manifests))
			}

			if len(work.Spec.ManifestConfigs) != 2 {
				return fmt.Errorf("Unexpected number of work manifests configuration: %d", len(work.Spec.ManifestConfigs))
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[1].Raw, []byte(deploymentZeroReplicasJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[1].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)

		// update work status to a wrong feedback status
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update to the correct condition
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent and get available with workload availability prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		objDaemonSet := &unstructured.Unstructured{}
		err = objDaemonSet.UnmarshalJSON([]byte(daemonSetJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj, objDaemonSet}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWorkloadAvailability,
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).
				Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 2 {
				return fmt.Errorf("Unexpected number of work manifests: %d", len(work.Spec.Workload.Manifests))
			}

			if len(work.Spec.ManifestConfigs) != 2 {
				return fmt.Errorf("Unexpected number of work manifests configuration: %d",
					len(work.Spec.ManifestConfigs))
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[1].Raw, []byte(daemonSetJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[1].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).
			Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)

		// update work status to a wrong feedback status
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReplicasTest",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		meta.SetStatusCondition(&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).
			UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update to the correct condition
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).
			Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "daemonsets",
						Name:      "nginx-ds",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "NumberReady",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
							{
								Name: "DesiredNumberScheduled",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).
			UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow trigger externally", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update object of addon and trigger the reconcile manually.
		newObj := obj.DeepCopy()
		err = unstructured.SetNestedField(newObj.Object, int64(2), "spec", "replicas")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{newObj}
		addonManager.Trigger(managedClusterName, testAddonImpl.name)

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			newDeploymentJson, err := newObj.MarshalJSON()
			if err != nil {
				return err
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, newDeploymentJson) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent and get deletion options", func() {
		deployObj := &unstructured.Unstructured{}
		err := deployObj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		mchObj := &unstructured.Unstructured{}
		err = mchObj.UnmarshalJSON([]byte(mchJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		mchObj.SetAnnotations(map[string]string{addonapiv1alpha1.DeletionOrphanAnnotationKey: ""})
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{deployObj, mchObj}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		createManagedClusterAddOnwithOwnerRefs(managedClusterName, addon, cma)

		var work *workapiv1.ManifestWork
		gomega.Eventually(func() error {
			work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Expect(len(work.Spec.Workload.Manifests)).Should(gomega.Equal(2))
		gomega.Expect(work.Spec.DeleteOption).ShouldNot(gomega.Equal(nil))
		gomega.Expect(work.Spec.DeleteOption.PropagationPolicy).Should(gomega.Equal(workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan))
		gomega.Expect(work.Spec.DeleteOption.SelectivelyOrphan).ShouldNot(gomega.Equal(nil))
		gomega.Expect(len(work.Spec.DeleteOption.SelectivelyOrphan.OrphaningRules)).Should(gomega.Equal(1))
		gomega.Expect(work.Spec.DeleteOption.SelectivelyOrphan.OrphaningRules[0].Group).Should(gomega.Equal("operator.open-cluster-management.io"))
		gomega.Expect(work.Spec.DeleteOption.SelectivelyOrphan.OrphaningRules[0].Resource).Should(gomega.Equal("multiclusterhubs"))
		gomega.Expect(work.Spec.DeleteOption.SelectivelyOrphan.OrphaningRules[0].Namespace).Should(gomega.Equal("open-cluster-management"))
		gomega.Expect(work.Spec.DeleteOption.SelectivelyOrphan.OrphaningRules[0].Name).Should(gomega.Equal("multiclusterhub"))
	})
})
