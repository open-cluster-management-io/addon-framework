package utils

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// DeploymentProber is to check the addon status based on status
// of the agent deployment status
type DeploymentProber struct {
	deployments []types.NamespacedName
}

func NewDeploymentProber(deployments ...types.NamespacedName) *agent.HealthProber {
	probeFields := []agent.ProbeField{}
	for _, deploy := range deployments {
		mc := DeploymentWellKnowManifestConfig(deploy.Namespace, deploy.Name)
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: mc.ResourceIdentifier,
			ProbeRules:         mc.FeedbackRules,
		})
	}
	return &agent.HealthProber{
		Type: agent.HealthProberTypeWork,
		WorkProber: &agent.WorkHealthProber{
			ProbeFields:   probeFields,
			HealthChecker: DeploymentAvailabilityHealthChecker,
		},
	}
}

func NewAllDeploymentsProber() *agent.HealthProber {
	probeFields := []agent.ProbeField{
		{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      "*",
				Namespace: "*",
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		},
	}

	return &agent.HealthProber{
		Type: agent.HealthProberTypeWork,
		WorkProber: &agent.WorkHealthProber{
			ProbeFields:   probeFields,
			HealthChecker: DeploymentAvailabilityHealthChecker,
		},
	}
}

func (d *DeploymentProber) ProbeFields() []agent.ProbeField {
	probeFields := []agent.ProbeField{}
	for _, deploy := range d.deployments {
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     appsv1.GroupName,
				Resource:  "deployments",
				Name:      deploy.Name,
				Namespace: deploy.Namespace,
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		})
	}
	return probeFields
}

// Deprecated: use DeploymentAvailabilityHealthChecker instead.
func DeploymentAvailabilityHealthCheck(identifier workapiv1.ResourceIdentifier,
	result workapiv1.StatusFeedbackResult) error {
	return checkWorkloadAvailabilityHealth(identifier, result)
}

// Deprecated: use DeploymentAvailabilityHealthChecker instead.
func AllDeploymentsAvailabilityHealthCheck(results []agent.FieldResult,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	if len(results) < 2 {
		return fmt.Errorf("all deployments are not available")
	}

	for _, result := range results {
		if err := checkWorkloadAvailabilityHealth(result.ResourceIdentifier, result.FeedbackResult); err != nil {
			return err
		}
	}
	return nil
}

func DeploymentAvailabilityHealthChecker(results []agent.FieldResult,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	return WorkloadAvailabilityHealthChecker(results, cluster, addon)
}

func WorkloadAvailabilityHealthChecker(results []agent.FieldResult,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	for _, result := range results {
		if err := checkWorkloadAvailabilityHealth(result.ResourceIdentifier, result.FeedbackResult); err != nil {
			return err
		}
	}
	return nil
}

func checkWorkloadAvailabilityHealth(identifier workapiv1.ResourceIdentifier,
	result workapiv1.StatusFeedbackResult) error {
	// only support deployments and daemonsets for now
	if identifier.Resource != "deployments" && identifier.Resource != "daemonsets" {
		return fmt.Errorf("unsupported resource type %s", identifier.Resource)
	}
	if identifier.Group != appsv1.GroupName {
		return fmt.Errorf("unsupported resource group %s", identifier.Group)
	}

	if len(result.Values) == 0 {
		return fmt.Errorf("no values are probed for %s %s/%s",
			identifier.Resource, identifier.Namespace, identifier.Name)
	}

	readyReplicas := -1
	desiredNumberReplicas := -1
	for _, value := range result.Values {
		// for deployment
		if value.Name == "ReadyReplicas" {
			readyReplicas = int(*value.Value.Integer)
		}
		if value.Name == "Replicas" {
			desiredNumberReplicas = int(*value.Value.Integer)
		}

		// for daemonset
		if value.Name == "NumberReady" {
			readyReplicas = int(*value.Value.Integer)
		}
		if value.Name == "DesiredNumberScheduled" {
			desiredNumberReplicas = int(*value.Value.Integer)
		}
	}

	if readyReplicas == -1 {
		return fmt.Errorf("readyReplica is not probed")
	}
	if desiredNumberReplicas == -1 {
		return fmt.Errorf("desiredNumberReplicas is not probed")
	}

	switch identifier.Resource {
	case "deployments":
		if desiredNumberReplicas == 0 || readyReplicas >= 1 {
			return nil
		}
	case "daemonsets":
		if readyReplicas == desiredNumberReplicas && readyReplicas > -1 {
			return nil
		}
	}

	return fmt.Errorf("desiredNumberReplicas is %d but readyReplica is %d for %s %s/%s",
		desiredNumberReplicas, readyReplicas, identifier.Resource, identifier.Namespace, identifier.Name)
}

func FilterDeployments(objects []runtime.Object) []*appsv1.Deployment {
	deployments := []*appsv1.Deployment{}
	for _, obj := range objects {
		deployment, err := ConvertToDeployment(obj)
		if err != nil {
			continue
		}
		deployments = append(deployments, deployment)
	}
	return deployments
}

type WorkloadMetadata struct {
	schema.GroupResource
	types.NamespacedName
	DeploymentSpec *DeploymentSpec
}

type DeploymentSpec struct {
	Replicas int32
}

func FilterWorkloads(objects []runtime.Object) []WorkloadMetadata {
	workloads := []WorkloadMetadata{}
	for _, obj := range objects {
		deployment, err := ConvertToDeployment(obj)
		if err == nil {
			// deployment replicas defaults to 1
			// https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#replicas
			var deploymentReplicas int32 = 1
			if deployment.Spec.Replicas != nil {
				deploymentReplicas = *deployment.Spec.Replicas
			}
			workloads = append(workloads, WorkloadMetadata{
				GroupResource: schema.GroupResource{
					Group:    appsv1.GroupName,
					Resource: "deployments",
				},
				NamespacedName: types.NamespacedName{
					Namespace: deployment.Namespace,
					Name:      deployment.Name,
				},
				DeploymentSpec: &DeploymentSpec{
					Replicas: deploymentReplicas,
				},
			})
		}
		daemonset, err := ConvertToDaemonSet(obj)
		if err == nil {
			workloads = append(workloads, WorkloadMetadata{
				GroupResource: schema.GroupResource{
					Group:    appsv1.GroupName,
					Resource: "daemonsets",
				},
				NamespacedName: types.NamespacedName{
					Namespace: daemonset.Namespace,
					Name:      daemonset.Name,
				},
			})
		}
	}
	return workloads
}

func ConvertToDeployment(obj runtime.Object) (*appsv1.Deployment, error) {
	if deployment, ok := obj.(*appsv1.Deployment); ok {
		return deployment, nil
	}

	return ConvertTo[appsv1.Deployment](obj, appsv1.GroupName, "Deployment")
}

func DeploymentWellKnowManifestConfig(namespace, name string) workapiv1.ManifestConfigOption {
	return WellKnowManifestConfig(appsv1.GroupName, "deployments", namespace, name)
}

func WellKnowManifestConfig(group, resources, namespace, name string) workapiv1.ManifestConfigOption {
	return workapiv1.ManifestConfigOption{
		ResourceIdentifier: workapiv1.ResourceIdentifier{
			Group:     group,
			Resource:  resources,
			Name:      name,
			Namespace: namespace,
		},
		FeedbackRules: []workapiv1.FeedbackRule{
			{
				Type: workapiv1.WellKnownStatusType,
			},
		},
	}
}

func FilterDaemonSets(objects []runtime.Object) []*appsv1.DaemonSet {
	daemonsets := []*appsv1.DaemonSet{}
	for _, obj := range objects {
		daemonset, err := ConvertToDaemonSet(obj)
		if err != nil {
			continue
		}
		daemonsets = append(daemonsets, daemonset)
	}
	return daemonsets
}

func ConvertToDaemonSet(obj runtime.Object) (*appsv1.DaemonSet, error) {
	if daemonSet, ok := obj.(*appsv1.DaemonSet); ok {
		return daemonSet, nil
	}

	return ConvertTo[appsv1.DaemonSet](obj, appsv1.GroupName, "DaemonSet")
}

func DaemonSetWellKnowManifestConfig(namespace, name string) workapiv1.ManifestConfigOption {
	return WellKnowManifestConfig(appsv1.GroupName, "daemonsets", namespace, name)
}
func ConvertTo[T any](obj runtime.Object, group, kind string) (*T, error) {
	// Verify GroupVersionKind matches expected values
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Group != group || gvk.Kind != kind {
		return nil, fmt.Errorf("not %s object, %v", kind, obj.GetObjectKind())
	}

	// Handle unstructured conversion
	uobj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("not unstructured object, %v", obj.GetObjectKind())
	}

	// Create new instance of target type and convert
	target := new(T)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uobj.Object, target); err != nil {
		return nil, err
	}
	return target, nil
}
