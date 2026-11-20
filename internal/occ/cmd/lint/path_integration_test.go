// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPathsWithSpacesUnicodeAndPercent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project 日本語 # %20")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "valid #%.yaml", validComponent)
	home := authenticatedHome(t)
	for _, relative := range []bool{false, true} {
		file := filepath.Join(dir, "valid #%.yaml")
		if relative {
			var err error
			file, err = filepath.Rel(root, file)
			if err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(binPath, "lint", "vali", "-p", "1", file)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "NO_COLOR=1")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "OK No issues found") {
			t.Fatalf("relative=%v: %v\n%s", relative, err, output)
		}
	}
}
