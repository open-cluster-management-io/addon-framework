package addonfactory

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func schemaWithDescription(desc string) apiextensionsv1.JSONSchemaProps {
	return apiextensionsv1.JSONSchemaProps{Description: desc}
}

func TestRemoveDescriptionV1(t *testing.T) {
	cases := []struct {
		name  string
		input *apiextensionsv1.JSONSchemaProps
		check func(t *testing.T, p *apiextensionsv1.JSONSchemaProps)
	}{
		{
			name:  "nil schema does not panic",
			input: nil,
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {},
		},
		{
			name:  "top level description is cleared",
			input: &apiextensionsv1.JSONSchemaProps{Description: "top"},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				if p.Description != "" {
					t.Errorf("expected empty description, got %q", p.Description)
				}
			},
		},
		{
			name: "items schema is recursed into",
			input: &apiextensionsv1.JSONSchemaProps{
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{
					Schema: &apiextensionsv1.JSONSchemaProps{Description: "item"},
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				if p.Items.Schema.Description != "" {
					t.Errorf("expected items.schema description cleared, got %q", p.Items.Schema.Description)
				}
			},
		},
		{
			name: "items json schemas slice is recursed into",
			input: &apiextensionsv1.JSONSchemaProps{
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{
					JSONSchemas: []apiextensionsv1.JSONSchemaProps{
						schemaWithDescription("a"),
						schemaWithDescription("b"),
					},
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				for i, s := range p.Items.JSONSchemas {
					if s.Description != "" {
						t.Errorf("expected items.jsonSchemas[%d] description cleared, got %q", i, s.Description)
					}
				}
			},
		},
		{
			name: "allOf oneOf anyOf not are recursed into",
			input: &apiextensionsv1.JSONSchemaProps{
				AllOf: []apiextensionsv1.JSONSchemaProps{schemaWithDescription("allof")},
				OneOf: []apiextensionsv1.JSONSchemaProps{schemaWithDescription("oneof")},
				AnyOf: []apiextensionsv1.JSONSchemaProps{schemaWithDescription("anyof")},
				Not:   &apiextensionsv1.JSONSchemaProps{Description: "not"},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				if p.AllOf[0].Description != "" {
					t.Errorf("expected allOf description cleared, got %q", p.AllOf[0].Description)
				}
				if p.OneOf[0].Description != "" {
					t.Errorf("expected oneOf description cleared, got %q", p.OneOf[0].Description)
				}
				if p.AnyOf[0].Description != "" {
					t.Errorf("expected anyOf description cleared, got %q", p.AnyOf[0].Description)
				}
				if p.Not.Description != "" {
					t.Errorf("expected not description cleared, got %q", p.Not.Description)
				}
			},
		},
		{
			name: "properties map is recursed into and preserved",
			input: &apiextensionsv1.JSONSchemaProps{
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"spec": schemaWithDescription("spec desc"),
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				spec, ok := p.Properties["spec"]
				if !ok {
					t.Fatal("expected spec key to survive trimming")
				}
				if spec.Description != "" {
					t.Errorf("expected properties[spec] description cleared, got %q", spec.Description)
				}
			},
		},
		{
			name: "pattern properties map is recursed into and preserved",
			input: &apiextensionsv1.JSONSchemaProps{
				PatternProperties: map[string]apiextensionsv1.JSONSchemaProps{
					"^x-": schemaWithDescription("pattern desc"),
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				pp, ok := p.PatternProperties["^x-"]
				if !ok {
					t.Fatal("expected pattern property key to survive trimming")
				}
				if pp.Description != "" {
					t.Errorf("expected patternProperties description cleared, got %q", pp.Description)
				}
			},
		},
		{
			name: "additional properties schema is recursed into",
			input: &apiextensionsv1.JSONSchemaProps{
				AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{
					Schema: &apiextensionsv1.JSONSchemaProps{Description: "additional"},
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				if p.AdditionalProperties.Schema.Description != "" {
					t.Errorf("expected additionalProperties.schema description cleared, got %q", p.AdditionalProperties.Schema.Description)
				}
			},
		},
		{
			name: "additional properties with nil schema does not panic",
			input: &apiextensionsv1.JSONSchemaProps{
				AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{Allows: true},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {},
		},
		{
			name: "dependencies map is recursed into and preserved",
			input: &apiextensionsv1.JSONSchemaProps{
				Dependencies: apiextensionsv1.JSONSchemaDependencies{
					"dep": apiextensionsv1.JSONSchemaPropsOrStringArray{
						Schema: &apiextensionsv1.JSONSchemaProps{Description: "dep desc"},
					},
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				dep, ok := p.Dependencies["dep"]
				if !ok {
					t.Fatal("expected dependency key to survive trimming")
				}
				if dep.Schema.Description != "" {
					t.Errorf("expected dependencies[dep].schema description cleared, got %q", dep.Schema.Description)
				}
			},
		},
		{
			name: "additional items schema is recursed into",
			input: &apiextensionsv1.JSONSchemaProps{
				AdditionalItems: &apiextensionsv1.JSONSchemaPropsOrBool{
					Schema: &apiextensionsv1.JSONSchemaProps{Description: "additional items"},
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				if p.AdditionalItems.Schema.Description != "" {
					t.Errorf("expected additionalItems.schema description cleared, got %q", p.AdditionalItems.Schema.Description)
				}
			},
		},
		{
			name: "definitions map is recursed into and preserved",
			input: &apiextensionsv1.JSONSchemaProps{
				Definitions: apiextensionsv1.JSONSchemaDefinitions{
					"def": schemaWithDescription("def desc"),
				},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				def, ok := p.Definitions["def"]
				if !ok {
					t.Fatal("expected definition key to survive trimming")
				}
				if def.Description != "" {
					t.Errorf("expected definitions[def] description cleared, got %q", def.Description)
				}
			},
		},
		{
			name: "external docs description is cleared",
			input: &apiextensionsv1.JSONSchemaProps{
				ExternalDocs: &apiextensionsv1.ExternalDocumentation{Description: "external", URL: "https://example.com"},
			},
			check: func(t *testing.T, p *apiextensionsv1.JSONSchemaProps) {
				if p.ExternalDocs.Description != "" {
					t.Errorf("expected externalDocs description cleared, got %q", p.ExternalDocs.Description)
				}
				if p.ExternalDocs.URL != "https://example.com" {
					t.Errorf("expected externalDocs url preserved, got %q", p.ExternalDocs.URL)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			removeDescriptionV1(c.input)
			c.check(t, c.input)
		})
	}
}

func TestTrimCRDv1Description(t *testing.T) {
	crd := &apiextensionsv1.CustomResourceDefinition{
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Description: "v1 schema",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": schemaWithDescription("v1 spec"),
							},
						},
					},
				},
				{
					Name:   "v2",
					Schema: nil,
				},
			},
		},
	}

	trimCRDv1Description(crd)

	v1Schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
	if v1Schema.Description != "" {
		t.Errorf("expected v1 top level description cleared, got %q", v1Schema.Description)
	}
	if v1Schema.Properties["spec"].Description != "" {
		t.Errorf("expected v1 spec property description cleared, got %q", v1Schema.Properties["spec"].Description)
	}
	if crd.Spec.Versions[1].Schema != nil {
		t.Errorf("expected v2 schema to remain nil")
	}
}

func TestTrimCRDDescription(t *testing.T) {
	crd := &apiextensionsv1.CustomResourceDefinition{
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Description: "trimmed"},
					},
				},
			},
		},
	}
	other := &unstructured.Unstructured{}
	other.SetName("some-object")

	result := trimCRDDescription([]runtime.Object{crd, other})

	if len(result) != 2 {
		t.Fatalf("expected 2 objects returned, got %d", len(result))
	}
	resultCRD, ok := result[0].(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		t.Fatalf("expected first object to remain a CustomResourceDefinition, got %T", result[0])
	}
	if resultCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Description != "" {
		t.Errorf("expected CRD description trimmed, got %q", resultCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Description)
	}
	resultOther, ok := result[1].(*unstructured.Unstructured)
	if !ok {
		t.Fatalf("expected second object to remain unstructured, got %T", result[1])
	}
	if resultOther.GetName() != "some-object" {
		t.Errorf("expected non-CRD object passed through unchanged, got name %q", resultOther.GetName())
	}
}
