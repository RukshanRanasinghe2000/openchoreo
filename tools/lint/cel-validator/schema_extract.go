// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
	apiextschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/schema"
)

// SchemaPair holds the two structural schemas that type the parameters and
// environmentConfigs CEL variables for a document.
type SchemaPair struct {
	// Parameters is the structural schema for spec.parameters.openAPIV3Schema.
	Parameters *apiextschema.Structural

	// EnvironmentConfigs is the structural schema for
	// spec.environmentConfigs.openAPIV3Schema.
	EnvironmentConfigs *apiextschema.Structural
}

// ExtractSchemas walks a document's spec mapping and resolves the
// openAPIV3Schema sections that type the parameters / environmentConfigs CEL
// variables.
//
// It works on the raw *yaml.Node instead of decoding a Go resource so the
// linter can still run when the document is otherwise invalid — a strict
// decode would abort first. The task layout is uniform across the CEL-bearing
// kinds: ComponentType, Trait and ResourceType (and their cluster variants)
// all carry the sections at spec.parameters / spec.environmentConfigs.
func ExtractSchemas(spec *yaml.Node) (SchemaPair, error) {
	pair := SchemaPair{}

	params, err := resolveSection(spec, fieldParameters, fieldOpenAPIV3Schema)
	if err != nil {
		return pair, fmt.Errorf("parameters schema: %w", err)
	}
	pair.Parameters = params

	envConfigs, err := resolveSection(spec, fieldEnvironmentConfigs, fieldOpenAPIV3Schema)
	if err != nil {
		return pair, fmt.Errorf("environmentConfigs schema: %w", err)
	}
	pair.EnvironmentConfigs = envConfigs

	return pair, nil
}

// resolveSection finds spec.<section>.<key> in the spec mapping and converts it
// to a structural schema. It returns (nil, nil) when either intermediate key is
// absent so a missing schema degrades to the caller's empty-object fallback.
func resolveSection(spec *yaml.Node, section, key string) (*apiextschema.Structural, error) {
	if spec == nil || spec.Kind != yaml.MappingNode {
		return nil, nil
	}
	sectionNode := mappingValue(spec, section)
	if sectionNode == nil || sectionNode.Kind != yaml.MappingNode {
		return nil, nil
	}
	keyNode := mappingValue(sectionNode, key)
	if keyNode == nil {
		return nil, nil
	}

	raw, err := nodeToJSONBytes(keyNode)
	if err != nil {
		return nil, err
	}

	sectionData := &v1alpha1.SchemaSection{
		OpenAPIV3Schema: &runtime.RawExtension{Raw: raw},
	}
	return schema.ResolveSectionToStructural(sectionData)
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// nodeToJSONBytes decodes a YAML node into a JSON byte slice so it can be fed
// to the Kubernetes schema converters, which only ingest JSON.
func nodeToJSONBytes(node *yaml.Node) ([]byte, error) {
	var v any
	if err := node.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode %T node: %w", node.Kind, err)
	}
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
