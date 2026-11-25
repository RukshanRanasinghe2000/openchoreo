// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Opt in because this test runs the real generators and compiler. Everything
// generated is stored in temporary directories; no source files are changed.
func TestReleaseIntegration(t *testing.T) {
	if os.Getenv("OCC_RELEASE_INTEGRATION") != "1" {
		t.Skip("set OCC_RELEASE_INTEGRATION=1 for real generation, compilation and LSP verification")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	c, err := newCoordinator(root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	before, err := inventory(filepath.Join(c.root, "tools", "templates"))
	if err != nil {
		t.Fatal(err)
	}
	versionBefore, err := os.ReadFile(filepath.Join(c.root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	currentSnapshot := filepath.Join(temporary, "current-snapshot")
	if err := c.generate(currentSnapshot); err != nil {
		t.Fatal(err)
	}
	buildSource := filepath.Join(temporary, "demonstration-source")
	if err := c.prepare(currentSnapshot, buildSource); err != nil {
		t.Fatal(err)
	}
	version := os.Getenv("OCC_RELEASE_TEST_VERSION")
	if version == "" {
		version = "1.5.0"
	}
	writeTestFile(t, filepath.Join(buildSource, "VERSION"), version+"\n")
	demo, err := newCoordinator(buildSource, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// This disposable working copy has no .git. Read provenance from the
	// original checkout without initializing Git or committing fixture files.
	actualRunner := demo.run
	demo.run = func(dir string, env map[string]string, name string, args ...string) ([]byte, error) {
		if name == "git" {
			dir = c.root
		}
		return actualRunner(dir, env, name, args...)
	}
	snapshotPath := filepath.Join(temporary, "demonstration-snapshot")
	if err := demo.generate(snapshotPath); err != nil {
		t.Fatal(err)
	}
	first, err := inventory(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := demo.generate(snapshotPath); err != nil {
		t.Fatal(err)
	}
	second, err := inventory(snapshotPath)
	if err != nil || !maps.Equal(first, second) {
		t.Fatal("real schema/CEL generation is not deterministic", err)
	}
	output := filepath.Join(temporary, "dist")
	platforms := runtime.GOOS + "/" + runtime.GOARCH + " " + os.Getenv("OCC_RELEASE_TEST_PLATFORMS")
	if err := demo.build(snapshotPath, platforms, output); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(output, runtime.GOOS, runtime.GOARCH, "occ")
	if runtime.GOOS == windowsOS {
		executable := binary + ".exe"
		if err := copyFile(binary, executable, 0o755); err != nil {
			t.Fatal(err)
		}
		binary = executable
	}
	smokeReleaseBinary(t, binary)
	after, err := inventory(filepath.Join(c.root, "tools", "templates"))
	if err != nil || !maps.Equal(before, after) {
		t.Fatal("release build changed historical templates in the checkout")
	}
	versionAfter, err := os.ReadFile(filepath.Join(c.root, "VERSION"))
	if err != nil || string(versionBefore) != string(versionAfter) {
		t.Fatal("release demonstration changed the source VERSION file")
	}
	t.Logf("Verified occ %s with only its release templates; platforms: %s", version, strings.TrimSpace(platforms))
}
