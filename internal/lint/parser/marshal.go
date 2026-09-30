// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// CleanNode wraps a *yaml.Node and produces a compact JSON
// representation for debugging / LSP responses.
type CleanNode struct {
	Node *yaml.Node
}

// MarshalJSON produces a clean JSON representation of a yaml.Node,
// showing only Kind, Tag, Value, Line/Column, and recursively
// cleaning child Content nodes.
func (cn CleanNode) MarshalJSON() ([]byte, error) {
	if cn.Node == nil {
		return []byte("null"), nil
	}

	n := cn.Node

	result := map[string]interface{}{
		"kind":   yamlKindName(n.Kind),
		"line":   n.Line,
		"column": n.Column,
	}

	if n.Value != "" {
		result["value"] = n.Value
	}

	if n.Tag != "" {
		result["tag"] = n.Tag
	}

	switch n.Kind {
	case yaml.MappingNode:
		m := make(map[string]interface{})
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			m[key.Value] = CleanNode{val}.nodeJSON()
		}
		result["entries"] = m

	case yaml.SequenceNode:
		var items []interface{}
		for _, child := range n.Content {
			items = append(items, CleanNode{child}.nodeJSON())
		}
		result["items"] = items

	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			result["document"] = CleanNode{n.Content[0]}.nodeJSON()
		}

	case yaml.AliasNode:
		if n.Alias != nil {
			result["alias"] = CleanNode{n.Alias}.nodeJSON()
		}
	}

	return json.Marshal(result)
}

func (cn CleanNode) nodeJSON() interface{} {
	if cn.Node == nil {
		return nil
	}

	n := cn.Node
	result := map[string]interface{}{
		"kind": yamlKindName(n.Kind),
	}

	if n.Value != "" {
		result["value"] = n.Value
	}

	switch n.Kind {
	case yaml.MappingNode:
		m := make(map[string]interface{})
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			m[key.Value] = CleanNode{val}.nodeJSON()
		}
		result["entries"] = m

	case yaml.SequenceNode:
		var items []interface{}
		for _, child := range n.Content {
			items = append(items, CleanNode{child}.nodeJSON())
		}
		result["items"] = items
	}

	return result
}

// MarshalJSON produces a clean JSON representation of DocumentNode,
// excluding the raw Root yaml.Node and exposing only the AST fields.
func (d *DocumentNode) MarshalJSON() ([]byte, error) {
	type fieldJSON struct {
		Key   string    `json:"key"`
		Range Range     `json:"range"`
		Node  CleanNode `json:"node"`
	}

	fields := make(map[string]fieldJSON, len(d.Fields))
	for k, f := range d.Fields {
		fields[k] = fieldJSON{
			Key:   f.Key,
			Range: f.Range,
			Node:  CleanNode{f.ValueNode},
		}
	}

	return json.Marshal(&struct {
		APIVersion    string               `json:"apiVersion"`
		Kind          string               `json:"kind"`
		Metadata      MetadataNode         `json:"metadata"`
		Spec          CleanNode            `json:"spec,omitempty"`
		Status        CleanNode            `json:"status,omitempty"`
		Fields        map[string]fieldJSON `json:"fields"`
		DocumentIndex int                  `json:"documentIndex"`
		Range         Range                `json:"range"`
	}{
		APIVersion:    d.APIVersion,
		Kind:          d.Kind,
		Metadata:      d.Metadata,
		Spec:          CleanNode{d.Spec},
		Status:        CleanNode{d.Status},
		Fields:        fields,
		DocumentIndex: d.DocumentIndex,
		Range:         d.Range,
	})
}

// MarshalJSON produces a clean JSON representation of MetadataNode.
func (m MetadataNode) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name        string    `json:"name"`
		Namespace   string    `json:"namespace,omitempty"`
		Labels      CleanNode `json:"labels,omitempty"`
		Annotations CleanNode `json:"annotations,omitempty"`
	}{
		Name:        m.Name,
		Namespace:   m.Namespace,
		Labels:      CleanNode{m.Labels},
		Annotations: CleanNode{m.Annotations},
	})
}

// MarshalJSON produces a clean JSON representation of Field.
func (f *Field) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Key   string    `json:"key"`
		Range Range     `json:"range"`
		Value CleanNode `json:"value"`
	}{
		Key:   f.Key,
		Range: f.Range,
		Value: CleanNode{f.ValueNode},
	})
}

// MarshalJSON produces a clean JSON representation of Position.
func (p Position) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	}{
		Line:   p.Line,
		Column: p.Column,
	})
}

// MarshalJSON produces a clean JSON representation of Range.
func (r Range) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Start Position `json:"start"`
		End   Position `json:"end"`
	}{
		Start: r.Start,
		End:   r.End,
	})
}

func yamlKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return "unknown"
	}
}
