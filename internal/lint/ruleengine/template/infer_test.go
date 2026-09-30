// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package template_test

import (
	"testing"

	"github.com/openchoreo/openchoreo/internal/lint/parser"
	"github.com/openchoreo/openchoreo/internal/lint/ruleengine/template"
	"github.com/openchoreo/openchoreo/internal/lint/strdist"
)

func mustDoc(t *testing.T, yaml string) *parser.DocumentNode {
	t.Helper()
	docs, err := parser.ParseYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no documents parsed")
	}
	return docs[0]
}

// TestInferKind_PicksBestFit verifies a typo'd kind still validates against
// the closest schema (spec field names match, including fuzzy matches).
func TestInferKind_PicksBestFit(t *testing.T) {
	doc := mustDoc(t, `apiVersion: openchoreo.dev/v1alpha1
kind: Projectf
metadata:
  name: doclet
spec:
  deploymentPipelinveRef:
    name: standard
`)
	if got := template.InferKind(doc); got != "Project" {
		t.Fatalf("InferKind = %q, want Project", got)
	}
}

// TestInferKind_ExactMatch verifies an exact spec key match picks the kind.
func TestInferKind_ExactMatch(t *testing.T) {
	doc := mustDoc(t, `apiVersion: openchoreo.dev/v1alpha1
kind: Wrong
metadata:
  name: x
spec:
  componentType:
    name: deployment/service
`)
	if got := template.InferKind(doc); got != "Component" {
		t.Fatalf("InferKind = %q, want Component", got)
	}
}

// TestInferKind_NoSpec returns nothing when the doc has no spec mapping.
func TestInferKind_NoSpec(t *testing.T) {
	tests := []string{
		"apiVersion: openchoreo.dev/v1alpha1\nkind: Bogus\n",
		"apiVersion: openchoreo.dev/v1alpha1\nkind: Bogus\nmetadata:\n  name: x\n",
	}
	for _, src := range tests {
		if got := template.InferKind(mustDoc(t, src)); got != "" {
			t.Fatalf("template.InferKind(%q) = %q, want empty", src, got)
		}
	}
}

// TestInferKind_NoMatch returns nothing when nothing plausibly matches.
func TestInferKind_NoMatch(t *testing.T) {
	doc := mustDoc(t, `apiVersion: openchoreo.dev/v1alpha1
kind: Widget
metadata:
  name: x
spec:
  frobnicate: true
`)
	if got := template.InferKind(doc); got != "" {
		t.Fatalf("InferKind = %q, want empty", got)
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"deploymentPipelinveRef", "deploymentPipelineRef", 1},
		{"deploymentPipelineRef", "deploymentPipelineRef", 0},
		{"kitten", "sitting", 3},
	}
	for _, c := range cases {
		if got := strdist.Distance(c.a, c.b); got != c.want {
			t.Errorf("strdist.Distance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
