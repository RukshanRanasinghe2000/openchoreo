// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/internal/pathutil"
)

func TestEscapedURIIndexLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "schema workspace 日本語 # %20")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "type name #%.yaml")
	text := []byte("apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: original}\nspec: {workloadType: deployment}\n")
	if err := os.WriteFile(path, text, 0600); err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer()
	if err := idx.IndexWorkspace(root); err != nil {
		t.Fatal(err)
	}
	uri := pathutil.FileURI(path)
	alias := strings.Replace(uri, "file:///", "file://localhost/", 1)
	updated := []byte(strings.Replace(string(text), "original", "edited", 1))
	if err := idx.UpdateFile(alias, updated); err != nil {
		t.Fatal(err)
	}
	if idx.FileCount() != 1 || len(idx.FindDefinition("ComponentType", "original")) != 0 {
		t.Fatal("alias created a second index entry")
	}
	if got := idx.FindScopedDefinition("ComponentType", "default", "deployment/edited", uri); len(got) != 1 || got[0].URI != alias {
		t.Fatalf("scoped navigation: %+v", got)
	}
	if data, ok := idx.SourceContent(uri); !ok || string(data) != string(updated) {
		t.Fatal("alias overlay was not authoritative")
	}
	names := idx.FileNames()
	found := false
	for _, name := range names {
		if name == "type name #%" {
			found = true
		}
	}
	if !found {
		t.Fatalf("filename remained URI-escaped: %v", names)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	idx.RemoveFile(uri)
	if idx.FileCount() != 0 || len(idx.FindDefinition("ComponentType", "edited")) != 0 {
		t.Fatal("deleted parent retained resource locations")
	}
}
