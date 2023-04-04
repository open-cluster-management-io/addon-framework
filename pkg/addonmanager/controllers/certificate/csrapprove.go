package certificate

import (
	"context"
	"strings"

	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

var (
	// EnableV1Beta1CSRCompatibility is a condition variable that enables/disables
	// the compatibility with V1beta1 CSR api. If enabled, the CSR approver
	// controller wil watch and approve over the V1beta1 CSR api instead of V1.
	// Setting the variable to false will make the CSR signer controller strictly
	// requires V1 CSR api.
	//
	// The distinction between V1 and V1beta1 CSR is that the latter doesn't have
	// a "signerName" field which is used for discriminating external certificate
	// signers. With that being said, under V1beta1 CSR api once a CSR object is
	// approved, it will be immediately signed by the CSR signer controller from
	// kube-controller-manager. So the csr signer controller will be permanently
	// disabled to avoid conflict with Kubernetes' original CSR signer.
	//
	// TODO: Remove this condition gate variable after V1beta1 CSR api fades away
	//       in the Kubernetes community. The code blocks supporting V1beta1 CSR
	//       should also be removed.
	EnableV1Beta1CSRCompatibility = true
)

type CSR interface {
	*certificatesv1.CertificateSigningRequest | *certificatesv1beta1.CertificateSigningRequest
	GetLabels() map[string]string
	GetName() string
}

type CSRLister[T CSR] interface {
	Get(name string) (T, error)
}

type CSRApprover[T CSR] interface {
	approve(ctx context.Context, csr T) error
	isInTerminalState(csr T) bool
}

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController[T CSR] struct {
	agentAddons               map[string]agent.AgentAddon
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	csrLister                 CSRLister[T]
	approver                  CSRApprover[T]
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController[T CSR](
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	csrInformer cache.SharedIndexInformer,
	csrLister CSRLister[T],
	approver CSRApprover[T],
	agentAddons map[string]agent.AgentAddon,
) factory.Controller {
	c := &csrApprovingController[T]{
		agentAddons:               agentAddons,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		csrLister:                 csrLister,
		approver:                  approver,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				return []string{accessor.GetName()}
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if !strings.HasPrefix(accessor.GetName(), "addon") {
					return false
				}
				if len(accessor.GetLabels()) == 0 {
					return false
				}
				addonName := accessor.GetLabels()[addonv1alpha1.AddonLabelKey]
				if _, ok := agentAddons[addonName]; !ok {
					return false
				}
				return true
			},
			csrInformer).
		WithSync(c.sync).
		ToController("CSRApprovingController")
}

func (c *csrApprovingController[T]) sync(ctx context.Context, syncCtx factory.SyncContext, csrName string) error {
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)

	csr, err := c.csrLister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if c.approver.isInTerminalState(csr) {
		return nil
	}

	addonName := csr.GetLabels()[addonv1alpha1.AddonLabelKey]
	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		return nil
	}
	clusterName, ok := csr.GetLabels()[clusterv1.ClusterNameLabelKey]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// Get Addon
	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if err := c.approve(ctx, registrationOption, managedCluster, managedClusterAddon, csr); err != nil {
		return err
	}

	return nil
}

// CSRV1Approver implement CSRApprover interface
type CSRV1Approver struct {
	kubeClient kubernetes.Interface
}

func NewCSRV1Approver(client kubernetes.Interface) *CSRV1Approver {
	return &CSRV1Approver{kubeClient: client}
}

func (c *CSRV1Approver) isInTerminalState(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

func (c *CSRV1Approver) approve(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) error {
	csrCopy := csr.DeepCopy()
	// Auto approve the spoke cluster csr
	csrCopy.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:    certificatesv1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
	})
	_, err := c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csrCopy.Name, csrCopy, metav1.UpdateOptions{})
	return err
}

type CSRV1beta1Approver struct {
	kubeClient kubernetes.Interface
}

func NewCSRV1beta1Approver(client kubernetes.Interface) *CSRV1beta1Approver {
	return &CSRV1beta1Approver{kubeClient: client}
}

func (c *CSRV1beta1Approver) isInTerminalState(csr *certificatesv1beta1.CertificateSigningRequest) bool {
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			return true
		}
	}
	return false
}

func (c *CSRV1beta1Approver) approve(ctx context.Context, csr *certificatesv1beta1.CertificateSigningRequest) error {
	csrCopy := csr.DeepCopy()
	// Auto approve the spoke cluster csr
	csrCopy.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type:    certificatesv1beta1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
	})
	_, err := c.kubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, csrCopy, metav1.UpdateOptions{})
	return err
}

func (c *csrApprovingController[T]) approve(
	ctx context.Context,
	registrationOption *agent.RegistrationOption,
	managedCluster *clusterv1.ManagedCluster,
	managedClusterAddon *addonv1alpha1.ManagedClusterAddOn,
	csr T) error {

	v1CSR := unsafeConvertV1beta1CSRToV1CSR[T](csr)
	if registrationOption.CSRApproveCheck == nil {
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check not defined", csr.GetName())
		return nil
	}
	approve := registrationOption.CSRApproveCheck(managedCluster, managedClusterAddon, v1CSR)
	if !approve {
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check fails", csr.GetName())
		return nil
	}

	return c.approver.approve(ctx, csr)
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeConvertV1beta1CSRToV1CSR[T CSR](csr T) *certificatesv1.CertificateSigningRequest {
	switch obj := any(csr).(type) {
	case *certificatesv1.CertificateSigningRequest:
		return obj
	case *certificatesv1beta1.CertificateSigningRequest:
		v1CSR := &certificatesv1.CertificateSigningRequest{
			TypeMeta: metav1.TypeMeta{
				APIVersion: certificatesv1.SchemeGroupVersion.String(),
				Kind:       "CertificateSigningRequest",
			},
			ObjectMeta: *obj.ObjectMeta.DeepCopy(),
			Spec: certificatesv1.CertificateSigningRequestSpec{
				Request:           obj.Spec.Request,
				ExpirationSeconds: obj.Spec.ExpirationSeconds,
				Usages:            unsafeCovertV1beta1KeyUsageToV1KeyUsage(obj.Spec.Usages),
				Username:          obj.Spec.Username,
				UID:               obj.Spec.UID,
				Groups:            obj.Spec.Groups,
				Extra:             unsafeCovertV1beta1ExtraValueToV1ExtraValue(obj.Spec.Extra),
			},
			Status: certificatesv1.CertificateSigningRequestStatus{
				Certificate: obj.Status.Certificate,
				Conditions:  unsafeCovertV1beta1ConditionsToV1Conditions(obj.Status.Conditions),
			},
		}
		if obj.Spec.SignerName != nil {
			v1CSR.Spec.SignerName = *obj.Spec.SignerName
		}
	}
	return nil
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeCovertV1beta1KeyUsageToV1KeyUsage(usages []certificatesv1beta1.KeyUsage) []certificatesv1.KeyUsage {
	v1Usages := make([]certificatesv1.KeyUsage, len(usages))
	for i := range usages {
		v1Usages[i] = certificatesv1.KeyUsage(usages[i])
	}
	return v1Usages
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeCovertV1beta1ExtraValueToV1ExtraValue(
	extraValues map[string]certificatesv1beta1.ExtraValue) map[string]certificatesv1.ExtraValue {
	v1Values := make(map[string]certificatesv1.ExtraValue)
	for k := range extraValues {
		v1Values[k] = certificatesv1.ExtraValue(extraValues[k])
	}
	return v1Values
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeCovertV1beta1ConditionsToV1Conditions(
	conditions []certificatesv1beta1.CertificateSigningRequestCondition,
) []certificatesv1.CertificateSigningRequestCondition {
	v1Conditions := make([]certificatesv1.CertificateSigningRequestCondition, len(conditions))
	for i := range conditions {
		v1Conditions[i] = certificatesv1.CertificateSigningRequestCondition{
			Type:               certificatesv1.RequestConditionType(conditions[i].Type),
			Status:             conditions[i].Status,
			Reason:             conditions[i].Reason,
			Message:            conditions[i].Message,
			LastTransitionTime: conditions[i].LastTransitionTime,
			LastUpdateTime:     conditions[i].LastUpdateTime,
		}
	}
	return v1Conditions
}
