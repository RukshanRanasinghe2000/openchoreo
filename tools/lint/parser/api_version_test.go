// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package parser_test

import (
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

func TestHasOpenChoreoAPIVersion(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"exact v1alpha1", "apiVersion: openchoreo.dev/v1alpha1\nkind: Component\n", true},
		{"future version", "apiVersion: openchoreo.dev/v2\nkind: Component\n", true},
		{"quoted value", `apiVersion: "openchoreo.dev/v1alpha1"` + "\n", true},
		{"single-quoted", `apiVersion: 'openchoreo.dev/v1alpha1'` + "\n", true},
		{"bare group", "apiVersion: openchoreo.dev\nkind: Component\n", true},
		{"leading whitespace", "  apiVersion: openchoreo.dev/v1alpha1\n", true},
		{"k8s apiVersion", "apiVersion: apps/v1\nkind: Deployment\n", false},
		{"unknown apiVersion", "apiVersion: custom.io/v1\nkind: Component\n", false},
		{"no apiVersion", "kind: Component\nmetadata:\n  name: demo\n", false},
		{"empty text", "", false},
		{"commented apiVersion only", "# apiVersion: openchoreo.dev/v1alpha1\nkind: Component\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.HasOpenChoreoAPIVersion([]byte(tt.data)); got != tt.want {
				t.Errorf("HasOpenChoreoAPIVersion(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestIsOpenChoreoAPIVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"openchoreo.dev/v1alpha1", true},
		{"openchoreo.dev/v2", true},
		{"openchoreo.dev", true},
		{"openchoreo.dev/", true},
		{"apps/v1", false},
		{"openchoreo.fake/v1", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := parser.IsOpenChoreoAPIVersion(tt.version); got != tt.want {
			t.Errorf("IsOpenChoreoAPIVersion(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}
