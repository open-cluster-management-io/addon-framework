package addonfactory

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
)

func TestGetAddOnDeploymentConfigValues(t *testing.T) {
	cases := []struct {
		name           string
		toValuesFuncs  []AddOnDeploymentConfigToValuesFunc
		addOnObjs      []runtime.Object
		expectedValues Values
	}{
		{
			name: "no addon Deployment configs",
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "config.test",
								Resource: "testconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "testConfig",
							},
						},
					}
					return addon
				}(),
			},
			expectedValues: Values{},
		},
		{
			name:          "mutiple addon deployment configs",
			toValuesFuncs: []AddOnDeploymentConfigToValuesFunc{ToAddOnDeploymentConfigValues},
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config1",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config2",
							},
						},
					}
					return addon
				}(),
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config1",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{Name: "Test", Value: "test1"},
						},
					},
				},
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config2",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{Name: "Test", Value: "test2"},
						},
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							NodeSelector: map[string]string{"test": "test"},
						},
					},
				},
			},
			expectedValues: Values{
				"Test":         "test2",
				"NodeSelector": map[string]string{"test": "test"},
				"Tolerations":  []corev1.Toleration{},
			},
		},
		{
			name:          "to addon node placement",
			toValuesFuncs: []AddOnDeploymentConfigToValuesFunc{ToAddOnNodePlacementValues},
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config",
							},
						},
					}
					return addon
				}(),
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							NodeSelector: nodeSelector,
							Tolerations:  tolerations,
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{"nodeSelector": map[string]interface{}{"kubernetes.io/os": "linux"}},
				"tolerations": []interface{}{
					map[string]interface{}{"effect": "NoExecute", "key": "foo", "operator": "Exists"},
				},
			},
		},
		{
			name:          "multiple toValuesFuncs",
			toValuesFuncs: []AddOnDeploymentConfigToValuesFunc{ToAddOnNodePlacementValues, ToAddOnCustomizedVariableValues},
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config",
							},
						},
					}
					return addon
				}(),
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							NodeSelector: nodeSelector,
							Tolerations:  tolerations,
						},
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{
								Name:  "managedKubeConfigSecret",
								Value: "external-managed-kubeconfig",
							},
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{"nodeSelector": map[string]interface{}{"kubernetes.io/os": "linux"}},
				"tolerations": []interface{}{
					map[string]interface{}{"effect": "NoExecute", "key": "foo", "operator": "Exists"},
				},
				"managedKubeConfigSecret": "external-managed-kubeconfig",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addOnObjs...)

			getter := NewAddOnDeploymentConfigGetter(fakeAddonClient)

			addOn, ok := c.addOnObjs[0].(*addonapiv1alpha1.ManagedClusterAddOn)
			if !ok {
				t.Errorf("expected addon object, but failed")
			}

			values, err := GetAddOnDeploymentConfigValues(getter, c.toValuesFuncs...)(nil, addOn)
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}

			if !equality.Semantic.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, values)
			}
		})
	}
}

func TestToImageOverrideValuesFunc(t *testing.T) {
	cases := []struct {
		name           string
		imageKey       string
		imageValue     string
		config         addonapiv1alpha1.AddOnDeploymentConfig
		expectedValues Values
		expectedErr    error
	}{
		{
			name:       "no nested imagekey",
			imageKey:   "image",
			imageValue: "a/b/c:v1",
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Source: "a/b",
							Mirror: "x/y",
						},
					},
				},
			},
			expectedValues: Values{
				"image": "x/y/c:v1",
			},
		},
		{
			name:       "nested imagekey",
			imageKey:   "global.imageOverride.image",
			imageValue: "a/b/c:v1",
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Source: "a",
							Mirror: "x",
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{
					"imageOverride": map[string]interface{}{
						"image": "x/b/c:v1",
					},
				},
			},
		},
		{
			name:       "empty imagekey",
			imageKey:   "",
			imageValue: "a/b/c:v1",
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Source: "a",
							Mirror: "x",
						},
					},
				},
			},
			expectedErr: fmt.Errorf("imageKey is empty"),
		},
		{
			name:       "empty image",
			imageKey:   "global.imageOverride.image",
			imageValue: "",
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Source: "a",
							Mirror: "x",
						},
					},
				},
			},
			expectedErr: fmt.Errorf("image is empty"),
		},
		{
			name:       "source not match",
			imageKey:   "global.imageOverride.image",
			imageValue: "a/b/c",
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Source: "b",
							Mirror: "y",
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{
					"imageOverride": map[string]interface{}{
						"image": "a/b/c",
					},
				},
			},
		},
		{
			name:       "source empty",
			imageKey:   "global.imageOverride.image",
			imageValue: "a/b/c:v1",
			config: addonapiv1alpha1.AddOnDeploymentConfig{
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Mirror: "y",
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{
					"imageOverride": map[string]interface{}{
						"image": "y/c:v1",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			values, err := ToImageOverrideValuesFunc(c.imageKey, c.imageValue)(c.config)
			if err != nil {
				if c.expectedErr == nil || !strings.EqualFold(err.Error(), c.expectedErr.Error()) {
					t.Errorf("expected error %v, but got error %s", c.expectedErr, err)
				}
			} else {
				if c.expectedErr != nil {
					t.Errorf("expected error %v, but got no error", c.expectedErr)
				}
			}

			if !equality.Semantic.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, values)
			}
		})
	}
}

func TestGetAgentImageValues(t *testing.T) {
	cases := []struct {
		name                  string
		imageKey              string
		imageValue            string
		cluster               *clusterv1.ManagedCluster
		addon                 *addonapiv1alpha1.ManagedClusterAddOn
		addonDeploymentConfig []runtime.Object
		expectedValues        Values
		expectedError         string
	}{
		{
			name:       "no nested imagekey",
			imageKey:   "image",
			imageValue: "a/b/c:v1",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"x/y","source":"a/b"}]}`,
					},
				},
			},
			expectedValues: Values{
				"image": "x/y/c:v1",
			},
		},
		{
			name:       "nested imagekey",
			imageKey:   "global.imageOverride.image",
			imageValue: "a/b/c:v1",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"x","source":"a"}]}`,
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{
					"imageOverride": map[string]interface{}{
						"image": "x/b/c:v1",
					},
				},
			},
		},
		{
			name:       "empty imagekey",
			imageKey:   "",
			imageValue: "a/b/c:v1",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"x","source":"a"}]}`,
					},
				},
			},
			expectedError: "imageKey is empty",
		},
		{
			name:       "empty image",
			imageKey:   "global.imageOverride.image",
			imageValue: "",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"x","source":"a"}]}`,
					},
				},
			},
			expectedError: "image is empty",
		},
		{
			name:       "source not match",
			imageKey:   "global.imageOverride.image",
			imageValue: "a/b/c",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"x","source":"b"}]}`,
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{
					"imageOverride": map[string]interface{}{
						"image": "a/b/c",
					},
				},
			},
		},
		{
			name:       "source empty",
			imageKey:   "global.imageOverride.image",
			imageValue: "a/b/c:v1",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"y"}]}`,
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{
					"imageOverride": map[string]interface{}{
						"image": "y/c:v1",
					},
				},
			},
		},
		{
			name:       "annotation invalid",
			imageKey:   "image",
			imageValue: "a/b/c:v1",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":`,
					},
				},
			},
			expectedValues: Values{
				"image": "a/b/c:v1",
			},
			expectedError: "unexpected end of JSON input",
		},
		{
			name:       "addon deployment config takes precedence",
			imageKey:   "image",
			imageValue: "a/b/c:v1",
			expectedValues: Values{
				"image": "y/b/c:v1",
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"x","source":"a"}]}`,
					},
				},
			},
			addon: func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: "cluster1",
							Name:      "config1",
						},
					},
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: "cluster1",
							Name:      "config2",
						},
					},
				}
				return addon
			}(),
			addonDeploymentConfig: []runtime.Object{
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config1",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{Name: "Test", Value: "test1"},
						},
					},
				},
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config2",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						Registries: []addonapiv1alpha1.ImageMirror{
							{
								Source: "a",
								Mirror: "y",
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var addonObjects []runtime.Object
			if c.addon != nil {
				addonObjects = append(addonObjects, c.addon)
			}
			addonObjects = append(addonObjects, c.addonDeploymentConfig...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(addonObjects...)
			getter := NewAddOnDeploymentConfigGetter(fakeAddonClient)
			addon := addontesting.NewAddon("test", "cluster1")
			if c.addon != nil {
				addon = c.addon
			}
			values, err := GetAgentImageValues(getter, c.imageKey, c.imageValue)(c.cluster, addon)
			if err != nil || len(c.expectedError) > 0 {
				assert.ErrorContains(t, err, c.expectedError, "expected error: %v, got: %v", c.expectedError, err)
			}

			if !equality.Semantic.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, values)
			}
		})
	}
}
