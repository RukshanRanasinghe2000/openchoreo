// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentFilesCompleteAndDeterministic(t *testing.T) {
	directory := t.TempDir()
	if err := generateForJob(directory); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != len(environmentFiles()) {
		t.Fatalf("unexpected output count: %d, %v", len(entries), err)
	}
	before := make(map[string]string)
	for _, file := range environmentFiles() {
		data, err := os.ReadFile(filepath.Join(directory, file))
		if err != nil {
			t.Fatal(err)
		}
		var desc EnvDesc
		if err := json.Unmarshal(data, &desc); err != nil || desc.Kind == "" || len(desc.Variables) == 0 {
			t.Fatalf("invalid CEL environment %s: %v", file, err)
		}
		before[file] = string(data)
	}
	if err := generateForJob(directory); err != nil {
		t.Fatal(err)
	}
	for name, expected := range before {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(data) != expected {
			t.Fatalf("non-deterministic generation of %s: %v", name, err)
		}
	}
}
