// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package strdist_test

import (
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/strdist"
)

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"Project", "project", 0},
		{"deploymentPipelinveRef", "deploymentPipelineRef", 1},
		{"parametrs", "parameters", 1},
		{"Projectf", "Project", 1},
		{"", "kind", 4},
		{"kind", "", 4},
		{"kitten", "sitting", 3},
		{"DeploymentPipelin", "DeploymentPipeline", 1},
	}
	for _, c := range cases {
		if got := strdist.Distance(c.a, c.b); got != c.want {
			t.Errorf("strdist.Distance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestClosest(t *testing.T) {
	cands := []string{"parameters", "type", "deploymentPipelineRef"}

	if got, dist, ok := strdist.Closest("parametrs", cands, 3); !ok || got != "parameters" || dist != 1 {
		t.Errorf("strdist.Closest(parametrs) = %q, %d, %v", got, dist, ok)
	}
	// A case-only difference is a match at distance 0.
	if got, dist, ok := strdist.Closest("PARAMETERS", cands, 3); !ok || got != "parameters" || dist != 0 {
		t.Errorf("strdist.Closest(PARAMETERS) = %q, %d, %v", got, dist, ok)
	}
	// Beyond the limit: no match.
	if _, _, ok := strdist.Closest("deploymentPipelineReferences", cands, 3); ok {
		t.Error("expected no match beyond the limit")
	}
	// Empty typo: no match.
	if _, _, ok := strdist.Closest("", cands, 3); ok {
		t.Error("expected no match for an empty typo")
	}
}

func TestClosest_TieIsRefused(t *testing.T) {
	// "type" and "tue" are both one edit from "tap".
	if got, _, ok := strdist.Closest("tap", []string{"type", "tue"}, 3); ok {
		t.Errorf("expected an ambiguous match to be refused, got %q", got)
	}
}

func TestClosest_PrefersTheNearerCandidate(t *testing.T) {
	got, dist, ok := strdist.Closest("Projectf", []string{"Project", "ProjectReleaseBinding"}, 3)
	if !ok || got != "Project" || dist != 1 {
		t.Errorf("strdist.Closest(Projectf) = %q, %d, %v", got, dist, ok)
	}
}
