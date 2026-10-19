// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lint_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLintDoesNotBootstrapContext pins the property that makes the linter usable
// in CI and in a container: `occ lint` validates local files and nothing else, so
// it must not try to set up an OpenChoreo context first.
//
// occ bootstraps a context for commands that talk to a control plane, which
// means writing $HOME/.openchoreo/config and usually stopping to log in. Forcing
// that on validation would break the two places a linter matters most: a
// pre-commit hook and a clean CI job, neither of which has a context. The
// SkipContextBootstrapAnnotation is what suppresses it, and this is the test that
// would notice if that annotation stopped working.
func TestLintDoesNotBootstrapContext(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	writeFixture(t, dir, "valid.yaml", validComponent)

	t.Setenv("HOME", home)
	// Point the linter's own template-version store somewhere disposable too,
	// so the test never reads or writes the developer's real config.
	t.Setenv("OPENCHOREO_TOOLKIT_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	code, stdout, stderr := runCLI(t, "lint", "vali", filepath.Join(dir, "valid.yaml"))
	if code != 0 {
		t.Fatalf("exit %d, want 0 for a valid resource\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// The strongest form of the assertion: nothing at all was created under HOME.
	if _, err := os.Stat(filepath.Join(home, ".openchoreo")); !os.IsNotExist(err) {
		t.Fatalf("occ lint created a context directory on a clean machine (stat err = %v)", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read HOME: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("occ lint wrote into HOME: %v", entries)
	}
}

// TestLintExitCodes pins the three-way contract that CI gates depend on: 0 for a
// clean resource, 1 for findings, 2 for a usage error. Collapsing 1 and 2 - which
// an unstructured exit does - makes "the linter found problems" and "you typed
// the command wrong" indistinguishable to a pipeline.
func TestLintExitCodes(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "valid.yaml", validComponent)
	writeFixture(t, dir, "invalid.yaml", invalidComponent)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"clean", []string{"lint", "vali", filepath.Join(dir, "valid.yaml")}, 0},
		{"findings", []string{"lint", "vali", filepath.Join(dir, "invalid.yaml")}, 1},
		{"usage error", []string{"lint", "vali"}, 2},
		{"unknown flag", []string{"lint", "vali", "--nope", filepath.Join(dir, "valid.yaml")}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCHOREO_TOOLKIT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
			code, stdout, stderr := runCLI(t, tc.args...)
			if code != tc.want {
				t.Fatalf("occ %v exited %d, want %d\nstdout:\n%s\nstderr:\n%s",
					tc.args, code, tc.want, stdout, stderr)
			}
		})
	}
}
