// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package codeaction

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/schema"
)

func TestBottomLineEmptyContainers(t *testing.T) {
	for _, kind := range []yaml.Kind{yaml.MappingNode, yaml.SequenceNode, yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode} {
		if got := bottomLine(&yaml.Node{Kind: kind, Line: 5}); got != 4 {
			t.Fatalf("kind %v: %d", kind, got)
		}
	}
}
func TestInsertSiblingAfterEmptyMapping(t *testing.T) {
	for _, text := range []string{"spec: {}\n", "traits: []\n"} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatal(err)
		}
		edit := insertSiblingEdit(strings.Split(text, "\n"), doc.Content[0], "", "added", &schema.FieldSchema{Type: "string"})
		if edit == nil || edit.Range.Start.Line != 0 || !strings.Contains(edit.NewText, "added:") {
			t.Fatalf("unexpected insertion: %+v", edit)
		}
	}
}

func TestSchemaFixesPreservesSharedSchema(t *testing.T) {
	kind := &schema.FieldSchema{Type: "string"}
	root := &schema.FieldSchema{Type: "object", Properties: map[string]*schema.FieldSchema{"kind": kind}}
	_ = schemaFixes("file:///test.yaml", "kind: Compnent", root, nil)
	if len(kind.Enum) != 0 || root.Properties["kind"] != kind {
		t.Fatal("code actions mutated the shared validation schema")
	}
}
