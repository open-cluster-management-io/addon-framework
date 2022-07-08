package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AddOnDeploymentConfig represents a deployment configuration for an add-on.
// AddOnDeploymentConfig is a cluster-scoped resource.
type AddOnDeploymentConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec represents a desired configuration for an add-on.
	// +required
	Spec AddOnDeploymentConfigSpec `json:"spec"`
}

type AddOnDeploymentConfigSpec struct {
	// CustomizedVariables is a list of name-value variables for the current add-on deployment.
	// The add-on implementation can use these variables to render the add-on deployment.
	// The default is an empty list.
	// +optional
	CustomizedVariables []CustomizedVariable `json:"customizedVariable,omitempty"`

	// NodePlacement enables explicit control over the scheduling of the add-on.
	// +optional
	NodePlacement NodePlacement `json:"nodePlacement,omitempty"`
}

// CustomizedVariable represents a customized variable for add-on deployment.
type CustomizedVariable struct {
	// Name of this variable. Must be a C_IDENTIFIER.
	Name string `json:"name"`

	// Value of this variable.
	Value string `json:"value"`
}

// NodePlacement describes node scheduling configuration for the pods.
type NodePlacement struct {
	// NodeSelector defines which Nodes the Pods are scheduled on. The default is an empty list.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations is attached by pods to tolerate any taint that matches
	// the triple <key,value,effect> using the matching operator <operator>.
	// The default is an empty list.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// AddOnDeploymentConfigList is a collection of add-on deployment config.
type AddOnDeploymentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of add-on deployment config.
	Items []AddOnDeploymentConfig `json:"items"`
}
