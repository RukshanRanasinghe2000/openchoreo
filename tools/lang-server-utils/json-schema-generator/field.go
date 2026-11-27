// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "encoding/json"

const (
	schemaTypeObject = "object"
	schemaTypeString = "string"
)

// Field is a node in the generated minimal JSON schema.
type Field struct {
	Type       string            `json:"type,omitempty"`
	Required   bool              `json:"required"`
	Default    json.RawMessage   `json:"default,omitempty"`
	Enum       []string          `json:"enum,omitempty"`
	Pattern    string            `json:"pattern,omitempty"`
	MinLength  int               `json:"minLength,omitempty"`
	MaxLength  int               `json:"maxLength,omitempty"`
	Format     string            `json:"format,omitempty"`
	Minimum    *int64            `json:"minimum,omitempty"`
	Maximum    *int64            `json:"maximum,omitempty"`
	MinItems   int               `json:"minItems,omitempty"`
	MaxItems   int               `json:"maxItems,omitempty"`
	Preserve   bool              `json:"preserveUnknownFields,omitempty"`
	Properties map[string]*Field `json:"properties,omitempty"`
	Items      *Field            `json:"items,omitempty"`

	// omitRequired suppresses the "required" key (doc roots and array items).
	omitRequired bool
}

// marker is a single kubebuilder/+ marker parsed from a Go doc comment,
// e.g. name="kubebuilder:validation:MinLength", value="1".
type marker struct {
	name  string
	value string
}

func (f Field) MarshalJSON() ([]byte, error) {
	type node struct {
		Type       string            `json:"type,omitempty"`
		Required   *bool             `json:"required,omitempty"`
		Default    json.RawMessage   `json:"default,omitempty"`
		Enum       []string          `json:"enum,omitempty"`
		Pattern    string            `json:"pattern,omitempty"`
		MinLength  int               `json:"minLength,omitempty"`
		MaxLength  int               `json:"maxLength,omitempty"`
		Format     string            `json:"format,omitempty"`
		Minimum    *int64            `json:"minimum,omitempty"`
		Maximum    *int64            `json:"maximum,omitempty"`
		MinItems   int               `json:"minItems,omitempty"`
		MaxItems   int               `json:"maxItems,omitempty"`
		Preserve   bool              `json:"preserveUnknownFields,omitempty"`
		Properties map[string]*Field `json:"properties,omitempty"`
		Items      *Field            `json:"items,omitempty"`
	}
	n := node{
		Type:       f.Type,
		Default:    f.Default,
		Enum:       f.Enum,
		Pattern:    f.Pattern,
		MinLength:  f.MinLength,
		MaxLength:  f.MaxLength,
		Format:     f.Format,
		Minimum:    f.Minimum,
		Maximum:    f.Maximum,
		MinItems:   f.MinItems,
		MaxItems:   f.MaxItems,
		Preserve:   f.Preserve,
		Properties: f.Properties,
		Items:      f.Items,
	}
	if !f.omitRequired {
		r := f.Required
		n.Required = &r
	}
	return json.Marshal(n)
}
