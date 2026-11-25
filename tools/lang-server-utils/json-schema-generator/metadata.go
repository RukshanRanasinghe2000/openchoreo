// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

// Metadata overlays for each kind category. Every subfield is optional;
// labels and annotations are free-form maps, so they preserve unknown keys.
func metadataNamespacedUser() *Field {
	return &Field{
		Type:     "object",
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: "string", Required: false},
			"namespace":         {Type: "string", Required: false},
			"uid":               {Type: "string", Required: false},
			"creationTimestamp": {Type: "string", Required: false},
			"labels":            {Type: "object", Required: false, Preserve: true},
			"annotations":       {Type: "object", Required: false, Preserve: true},
		},
	}
}

func metadataNamespacedWorkflow() *Field {
	return &Field{
		Type:     "object",
		Required: false,
		Properties: map[string]*Field{
			"name":        {Type: "string", Required: false},
			"namespace":   {Type: "string", Required: false},
			"labels":      {Type: "object", Required: false, Preserve: true},
			"annotations": {Type: "object", Required: false, Preserve: true},
		},
	}
}

func metadataClusterDefault() *Field {
	return &Field{
		Type:     "object",
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: "string", Required: false},
			"uid":               {Type: "string", Required: false},
			"creationTimestamp": {Type: "string", Required: false},
			"annotations":       {Type: "object", Required: false, Preserve: true},
		},
	}
}

func metadataClusterProjectType() *Field {
	return &Field{
		Type:     "object",
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: "string", Required: false},
			"uid":               {Type: "string", Required: false},
			"creationTimestamp": {Type: "string", Required: false},
			"labels":            {Type: "object", Required: false, Preserve: true},
			"annotations":       {Type: "object", Required: false, Preserve: true},
		},
	}
}

func metadataClusterWorkflow() *Field {
	return &Field{
		Type:     "object",
		Required: false,
		Properties: map[string]*Field{
			"name":              {Type: "string", Required: false},
			"uid":               {Type: "string", Required: false},
			"creationTimestamp": {Type: "string", Required: false},
			"labels":            {Type: "object", Required: false, Preserve: true},
			"annotations":       {Type: "object", Required: false, Preserve: true},
		},
	}
}
