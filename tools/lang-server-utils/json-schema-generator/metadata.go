// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

// Metadata overlays for each kind category. Every subfield is optional;
// labels and annotations are free-form maps, so they preserve unknown keys.
func metadataNamespacedUser() *Field {
	return &Field{
		Type:     schemaTypeObject,
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: schemaTypeString, Required: false},
			"namespace":         {Type: schemaTypeString, Required: false},
			"uid":               {Type: schemaTypeString, Required: false},
			"creationTimestamp": {Type: schemaTypeString, Required: false},
			"labels":            {Type: schemaTypeObject, Required: false, Preserve: true},
			"annotations":       {Type: schemaTypeObject, Required: false, Preserve: true},
		},
	}
}

func metadataNamespacedWorkflow() *Field {
	return &Field{
		Type:     schemaTypeObject,
		Required: false,
		Properties: map[string]*Field{
			"name":        {Type: schemaTypeString, Required: false},
			"namespace":   {Type: schemaTypeString, Required: false},
			"labels":      {Type: schemaTypeObject, Required: false, Preserve: true},
			"annotations": {Type: schemaTypeObject, Required: false, Preserve: true},
		},
	}
}

func metadataClusterDefault() *Field {
	return &Field{
		Type:     schemaTypeObject,
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: schemaTypeString, Required: false},
			"uid":               {Type: schemaTypeString, Required: false},
			"creationTimestamp": {Type: schemaTypeString, Required: false},
			"annotations":       {Type: schemaTypeObject, Required: false, Preserve: true},
		},
	}
}

func metadataClusterProjectType() *Field {
	return &Field{
		Type:     schemaTypeObject,
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: schemaTypeString, Required: false},
			"uid":               {Type: schemaTypeString, Required: false},
			"creationTimestamp": {Type: schemaTypeString, Required: false},
			"labels":            {Type: schemaTypeObject, Required: false, Preserve: true},
			"annotations":       {Type: schemaTypeObject, Required: false, Preserve: true},
		},
	}
}

func metadataClusterWorkflow() *Field {
	return &Field{
		Type:     schemaTypeObject,
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: schemaTypeString, Required: false},
			"uid":               {Type: schemaTypeString, Required: false},
			"creationTimestamp": {Type: schemaTypeString, Required: false},
			"labels":            {Type: schemaTypeObject, Required: false, Preserve: true},
			"annotations":       {Type: schemaTypeObject, Required: false, Preserve: true},
		},
	}
}
