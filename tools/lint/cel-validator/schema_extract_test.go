// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func mustParseSpec(t *testing.T, yamlText string) *yaml.Node {
	t.Helper()
	root := &yaml.Node{}
	if err := yaml.Unmarshal([]byte(yamlText), root); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0] // the actual mapping node
	}
	return mappingValue(root, "spec")
}

func TestExtractSchemas(t *testing.T) {
	spec := mustParseSpec(t, `
apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
spec:
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        port:
          type: integer
        replicas:
          type: integer
  environmentConfigs:
    openAPIV3Schema:
      type: object
      properties:
        size:
          type: string
`)

	pair, err := ExtractSchemas(spec)
	if err != nil {
		t.Fatalf("ExtractSchemas: %v", err)
	}
	if pair.Parameters == nil {
		t.Fatal("Parameters schema should be resolved")
	}
	if _, ok := pair.Parameters.Properties["port"]; !ok {
		t.Fatalf("Parameters.Properties should include port, got %v", pair.Parameters.Properties)
	}
	if pair.EnvironmentConfigs == nil {
		t.Fatal("EnvironmentConfigs schema should be resolved")
	}
	if _, ok := pair.EnvironmentConfigs.Properties["size"]; !ok {
		t.Fatalf("EnvironmentConfigs.Properties should include size, got %v", pair.EnvironmentConfigs.Properties)
	}
}

func TestExtractSchemasMissingSections(t *testing.T) {
	spec := mustParseSpec(t, `
apiVersion: openchoreo.dev/v1alpha1
kind: Trait
spec:
  creates:
  - template: {}
`)

	pair, err := ExtractSchemas(spec)
	if err != nil {
		t.Fatalf("ExtractSchemas: %v", err)
	}
	if pair.Parameters != nil || pair.EnvironmentConfigs != nil {
		t.Fatalf("expected nil schemas when sections absent, got %+v", pair)
	}
}

func TestExtractSchemasNilSpec(t *testing.T) {
	pair, err := ExtractSchemas(nil)
	if err != nil {
		t.Fatalf("ExtractSchemas(nil): %v", err)
	}
	if pair.Parameters != nil || pair.EnvironmentConfigs != nil {
		t.Fatalf("expected nil schemas for nil spec, got %+v", pair)
	}
}

func TestExtractSchemasMalformedSchema(t *testing.T) {
	spec := mustParseSpec(t, `
apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
spec:
  parameters:
    openAPIV3Schema: not-an-object
`)

	pair, err := ExtractSchemas(spec)
	if err == nil {
		t.Fatalf("malformed openAPIV3Schema should error, got pair %+v", pair)
	}
}
