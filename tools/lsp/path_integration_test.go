// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/internal/pathutil"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestPathIdentityIndexNavigationAndOverlay(t *testing.T) {
	root := filepath.Join(t.TempDir(), "日本語 workspace # %20")
	other := t.TempDir()
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "Type # %.yaml")
	source := filepath.Join(root, "Component.yaml")
	typeText := "apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: service}\nspec: {}\n"
	component := definitionSource("ComponentType", "service", "default")
	for p, text := range map[string]string{target: typeText, source: component, filepath.Join(other, "unrelated.yaml"): strings.Replace(typeText, "service", "other", 1)} {
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer()
	t.Cleanup(s.StopWatcher)
	s.workspaceRoots = []string{root, other}
	if err := s.indexer.IndexWorkspaces(s.workspaceRoots); err != nil {
		t.Fatal(err)
	}
	sourceURI, targetURI := pathutil.FileURI(source), pathutil.FileURI(target)
	// Both localhost and alternate escape casing are legal client spellings.
	alias := strings.Replace(sourceURI, "file:///", "file://localhost/", 1)
	if got := callDefinition(t, s, alias, component, "service"); len(got) != 1 || got[0].URI != targetURI || got[0].Range.Start.Line != 2 {
		t.Fatalf("definition: %+v", got)
	}
	// Opened content replaces the disk definition, regardless of URI spelling.
	targetAlias := strings.Replace(targetURI, "file:///", "file://localhost/", 1)
	unsaved := strings.Replace(typeText, "service", "unsaved", 1)
	s.OpenDocument(targetAlias, unsaved, 2)
	if err := s.indexer.UpdateFile(targetAlias, []byte(unsaved)); err != nil {
		t.Fatal(err)
	}
	if s.updateIndexedFile(target) {
		t.Fatal("watcher overwrote an unsaved buffer")
	}
	if got := s.GetDocument(targetURI); got == nil || got.Text != unsaved || got.URI != targetAlias {
		t.Fatalf("overlay identity lost: %+v", got)
	}
	if got := s.indexer.FindDefinition("ComponentType", "service"); len(got) != 0 {
		t.Fatalf("stale disk definition: %+v", got)
	}
	if got := s.indexer.FindScopedDefinition("ComponentType", "default", "unsaved", sourceURI); len(got) != 1 || got[0].URI != targetAlias {
		t.Fatalf("unsaved definition: %+v", got)
	}
	if got := s.indexer.FindScopedDefinition("ComponentType", "default", "other", sourceURI); len(got) != 0 {
		t.Fatal("multi-root scope leaked")
	}
	// Diagnostic caches use the same identity but publication keeps the client URI.
	s.diagnosticsForCached(targetAlias, unsaved)
	var publication *diagnosticPublication
	ctx := &glsp.Context{Notify: func(method string, params any) {
		if method == protocol.ServerTextDocumentPublishDiagnostics {
			publication = params.(*diagnosticPublication)
		}
	}}
	s.publishDiagnostics(ctx, targetURI, 2, nil)
	if publication == nil || publication.URI != targetAlias {
		t.Fatalf("client URI changed: %+v", publication)
	}
	s.CloseDocument(targetURI)
	if s.GetDocument(targetAlias) != nil {
		t.Fatal("close left an alias open")
	}
	if !s.updateIndexedFile(target) {
		t.Fatal("close did not restore disk state")
	}
	s.OpenDocument(targetAlias, unsaved, 3)
	if s.GetDocument(targetURI).Version != 3 {
		t.Fatal("reopen did not restore identity")
	}
}

func TestPathWatchRenameDeleteAndValidationLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project space # %")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "environment #%.yaml")
	text := demoEnvironmentYAML
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	uri := pathutil.FileURI(p)
	s := NewServer()
	t.Cleanup(s.StopWatcher)
	s.workspaceRoots = []string{root}
	if err := s.indexer.IndexWorkspace(root); err != nil {
		t.Fatal(err)
	}
	alias := strings.Replace(uri, "file:///", "file://localhost/", 1)
	s.OpenDocument(alias, text, 1)
	diags := []protocol.Diagnostic{{Message: "manual finding"}}
	s.publishPostRenderFindings(alias, p, []string{p}, nil, nil)
	s.postRenderByURI = map[string][]protocol.Diagnostic{pathutil.DocumentKey(uri): diags}
	if got := s.mergePostRenderDiagnostics(alias, nil); len(got) != 1 {
		t.Fatal("manual findings lost across equivalent URI")
	}
	s.CloseDocument(uri)
	if got := s.mergePostRenderDiagnostics(alias, nil); len(got) != 1 {
		t.Fatal("closing a saved document cleared findings")
	}
	renamed := filepath.Join(root, "renamed 日本語.yaml")
	if err := os.Rename(p, renamed); err != nil {
		t.Fatal(err)
	}
	if !s.updateIndexedFile(p) || !s.updateIndexedFile(renamed) {
		t.Fatal("rename did not replace index entry")
	}
	if _, ok := s.indexer.SourceContent(alias); ok {
		t.Fatal("renamed file retained stale alias")
	}
	if _, ok := s.indexer.SourceContent(pathutil.FileURI(renamed)); !ok {
		t.Fatal("renamed target not indexed")
	}
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if !s.updateIndexedFile(renamed) {
		t.Fatal("delete was not detected")
	}
	if s.indexer.FileCount() != 0 {
		t.Fatal("deleted file retained index entries")
	}
	s.invalidatePostRenderForEdit(uri)
	if len(s.postRenderByURI) != 0 {
		t.Fatal("delete retained obsolete manual findings")
	}
}

func TestMalformedWorkspaceAndVirtualPathsNeverBecomeLocal(t *testing.T) {
	bad := "file:///bad%zz"
	virtual := "openchoreo://ctx/namespaces/default/component/name.yaml"
	roots := resolveWorkspaceRoots(&protocol.InitializeParams{WorkspaceFolders: []protocol.WorkspaceFolder{{URI: bad, Name: "bad"}, {URI: virtual, Name: "virtual"}}})
	if len(roots) != 0 {
		t.Fatalf("non-filesystem roots accepted: %v", roots)
	}
	s := NewServer()
	if s.postRenderRoot("") != "" {
		t.Fatal("invalid path became CWD scope")
	}
	if got := s.runPostRenderValidation(bad); got.Errors != 1 || !strings.Contains(got.Message, "invalid document URI") {
		t.Fatalf("malformed URI not controlled: %+v", got)
	}
	s.OpenDocument(virtual, "kind: Component", 1)
	if s.GetDocument(virtual) == nil || fileURIToPath(virtual) != "" {
		t.Fatal("virtual document was treated as a file")
	}
}
