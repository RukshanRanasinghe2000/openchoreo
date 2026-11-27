// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

func TestEmbeddedInventoryIgnoresWorkingDirectoryOverrides(t *testing.T) {
	t.Chdir(t.TempDir())
	manifest := []byte(`{"latest":"not-embedded","versions":["not-embedded"]}`)
	if err := os.WriteFile("template-version.json", manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := EmbeddedInventory("1.5.0-rc.1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "1.5.0-rc.1" || info.Latest == "" || len(info.Versions) == 0 || len(info.Files) == 0 {
		t.Fatalf("invalid inventory: %+v", info)
	}
	if info.Latest == "not-embedded" {
		t.Fatal("inventory read a filesystem override instead of embedded templates")
	}
	for name, expected := range info.Files {
		data, err := FS.ReadFile(name)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != expected {
			t.Fatalf("incorrect embedded digest for %s: %v", name, err)
		}
	}
}
