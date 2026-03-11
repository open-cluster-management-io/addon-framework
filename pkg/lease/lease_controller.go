package lease

import (
	"context"
	"net/http"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	leaseUpdateJitterFactor     = 0.25
	defaultLeaseDurationSeconds = 60
)

// LeaseUpdater is to update lease with certain period
type LeaseUpdater interface {
	// Start starts a goroutine to update lease
	Start(ctx context.Context)

	// WithHubLeaseConfig sets the lease config on hub cluster. It allows LeaseUpdater to create/update
	// addon lease on hub cluster when resource 'Lease' is not available on managed cluster.
	WithHubLeaseConfig(config *rest.Config, clusterName string) LeaseUpdater
}

// leaseUpdater update lease of with given name and namespace
type leaseUpdater struct {
	kubeClient           kubernetes.Interface
	leaseName            string
	leaseNamespace       string
	leaseDurationSeconds int32
	clusterName          string
	hubKubeClient        kubernetes.Interface
	healthCheckFuncs     []func() bool
}

func NewLeaseUpdater(
	kubeClient kubernetes.Interface,
	leaseName, leaseNamespace string,
	healthCheckFuncs ...func() bool,
) LeaseUpdater {
	return &leaseUpdater{
		kubeClient:           kubeClient,
		leaseName:            leaseName,
		leaseNamespace:       leaseNamespace,
		leaseDurationSeconds: defaultLeaseDurationSeconds,
		healthCheckFuncs:     healthCheckFuncs,
	}
}

func (r *leaseUpdater) Start(ctx context.Context) {
	wait.JitterUntilWithContext(ctx, r.reconcile, time.Duration(r.leaseDurationSeconds)*time.Second, leaseUpdateJitterFactor, true)
}

func (r *leaseUpdater) WithHubLeaseConfig(config *rest.Config, clusterName string) LeaseUpdater {
	hubClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Errorf("Failed to build hub kube client %v", err)
	} else {
		r.hubKubeClient = hubClient
	}
	r.clusterName = clusterName

	return r
}

func (r *leaseUpdater) updateLease(ctx context.Context, namespace string, client kubernetes.Interface) error {
	lease, err := client.CoordinationV1().Leases(namespace).Get(ctx, r.leaseName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		// create lease
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.leaseName,
				Namespace: namespace,
			},
			Spec: coordinationv1.LeaseSpec{
				LeaseDurationSeconds: &r.leaseDurationSeconds,
				RenewTime: &metav1.MicroTime{
					Time: time.Now(),
				},
			},
		}
		if _, err := client.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		// update lease
		lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
		if _, err = client.CoordinationV1().Leases(namespace).Update(context.TODO(), lease, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (r *leaseUpdater) reconcile(ctx context.Context) {
	for _, f := range r.healthCheckFuncs {
		if !f() {
			// IF a healthy check fails, do not update lease.
			return
		}
	}
	// Update lease on managed cluster at first, it returns in valid, it means lease is not supported yet
	// and fallback to use hub lease.
	err := r.updateLease(ctx, r.leaseNamespace, r.kubeClient)
	if errors.IsNotFound(err) && r.hubKubeClient != nil {
		if err := r.updateLease(ctx, r.clusterName, r.hubKubeClient); err != nil {
			klog.Errorf("Failed to update lease %s/%s: %v on hub", r.clusterName, r.leaseNamespace, err)
		}
		return
	}

	if err != nil {
		klog.Errorf("Failed to update lease %s/%s: %v on managed cluster", r.leaseName, r.leaseNamespace, err)
	}
}

// CheckAddonPodFunc checks whether the agent pod is running and ready.
// A pod must be both in the Running phase and have the Ready condition set to True
// to be considered healthy. This prevents the lease from being updated when the pod
// is running but not yet ready to serve (e.g., readiness probe is failing).
func CheckAddonPodFunc(podGetter corev1client.PodsGetter, namespace, labelSelector string) func() bool {
	return func() bool {
		pods, err := podGetter.Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			klog.Errorf("Failed to get pods in namespace %s with label selector %s: %v", namespace, labelSelector, err)
			return false
		}

		// If one of the pods is running and ready, we think the agent is serving.
		for i := range pods.Items {
			if pods.Items[i].Status.Phase == corev1.PodRunning && isPodReady(&pods.Items[i]) {
				return true
			}
		}

		return false
	}
}

// isPodReady checks whether a pod has the Ready condition set to True.
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// CheckManagedClusterHealthFunc checks the health status of the cluster api server
func CheckManagedClusterHealthFunc(managedClusterDiscoveryClient discovery.DiscoveryInterface) func() bool {
	return func() bool {
		statusCode := 0
		_ = managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/livez").Do(context.TODO()).StatusCode(&statusCode)
		if statusCode == http.StatusOK {
			return true
		}

		// for backward compatible, the livez endpoint is supported from Kubernetes 1.16, so if the livez is not found or
		// forbidden, the healthz endpoint will be used.
		if statusCode == http.StatusNotFound || statusCode == http.StatusForbidden {
			_ = managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/healthz").Do(context.TODO()).StatusCode(&statusCode)
			if statusCode == http.StatusOK {
				return true
			}
		}
		return false
	}
}
