// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
	post "github.com/openchoreo/openchoreo/tools/lint/post-render-validator"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func lifecycleOpen(t *testing.T, s *Server, uri, text string) []protocol.Diagnostic {
	t.Helper()
	var result []protocol.Diagnostic
	ctx := &glsp.Context{Notify: func(method string, params any) {
		if method == protocol.ServerTextDocumentPublishDiagnostics {
			result = params.(*diagnosticPublication).Diagnostics
		}
	}}
	if err := s.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: uri, Text: text, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return result
}
func lifecycleClose(t *testing.T, s *Server, uri string) []protocol.Diagnostic {
	t.Helper()
	var result []protocol.Diagnostic
	ctx := &glsp.Context{Notify: func(method string, params any) {
		if method == protocol.ServerTextDocumentPublishDiagnostics {
			result = params.(*diagnosticPublication).Diagnostics
		}
	}}
	if err := s.DidClose(ctx, &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}}); err != nil {
		t.Fatal(err)
	}
	return result
}
func missingWorkloadDiagnostic(diags []protocol.Diagnostic) *protocol.Diagnostic {
	for _, d := range diags {
		if strings.Contains(d.Message, "no Workload for Component") {
			copy := d
			return &copy
		}
	}
	return nil
}
func TestClusterProblemsNavigationPreservesAllFindings(t *testing.T) {
	s, r := clusterValidationFixture(t)
	r.Documents = r.Documents[:2] // No Workload, including for an unopened Component.
	_ = runCluster(t, s, r)
	uri := r.Documents[1].URI
	diag := missingWorkloadDiagnostic(s.clusterValidation.byURI[uri])
	if diag == nil {
		t.Fatal("missing workload finding")
	}
	line := strings.Split(clusterValidationComponent, "\n")[diag.Range.Start.Line]
	if line[diag.Range.Start.Character:diag.Range.End.Character] != "app" {
		t.Fatalf("not under metadata.name: %+v", diag)
	}
	count := len(s.clusterValidation.byURI)
	// Opening a result and replacing a clean preview must preserve the whole run.
	for i := 0; i < 2; i++ {
		if d := missingWorkloadDiagnostic(lifecycleOpen(t, s, uri, clusterValidationComponent)); d == nil {
			t.Fatal("open erased finding")
		}
		if d := missingWorkloadDiagnostic(lifecycleClose(t, s, uri)); d == nil {
			t.Fatal("close erased finding")
		}
		if len(s.clusterValidation.byURI) != count {
			t.Fatal("preview close cleared other findings")
		}
	}
	if len(s.indexer.DefinitionSnapshot(uri)) != 0 {
		t.Fatal("virtual resource entered filesystem index")
	}
}
func TestClusterProblemsNormalizeAndRebaseLoadedYAML(t *testing.T) {
	s, r := clusterValidationFixture(t)
	r.Documents = r.Documents[:2]
	_ = runCluster(t, s, r)
	uri := r.Documents[1].URI
	loaded := strings.Replace(clusterValidationComponent, "metadata: {name: app, namespace: default}", "# provider formatting\nmetadata:\n  name: 'app'", 1)
	diag := missingWorkloadDiagnostic(lifecycleOpen(t, s, uri, loaded))
	if diag == nil {
		t.Fatal("format or implicit namespace invalidated findings")
	}
	line := strings.Split(loaded, "\n")[diag.Range.Start.Line]
	if line[diag.Range.Start.Character:diag.Range.End.Character] != "app" {
		t.Fatalf("range was not rebased: %+v in %s", diag.Range, line)
	}
	if missingWorkloadDiagnostic(lifecycleClose(t, s, uri)) == nil {
		t.Fatal("normalized close cleared finding")
	}
	changed := strings.Replace(loaded, "count: 1", "count: 2", 1)
	lifecycleOpen(t, s, uri, changed)
	if len(s.clusterValidation.byURI) != 0 {
		t.Fatal("meaningful provider change retained findings")
	}
}
func TestClusterProblemsDiscardAndSaveValidatedEdits(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "discard", true: "saved"}[saved], func(t *testing.T) {
			s, r := clusterValidationFixture(t)
			r.Documents = r.Documents[:2]
			uri := r.Documents[1].URI
			lifecycleOpen(t, s, uri, clusterValidationComponent)
			edited := strings.Replace(clusterValidationComponent, "count: 1", "count: 2", 1)
			if err := s.UpdateDocument(uri, edited, 2); err != nil {
				t.Fatal(err)
			}
			version := 2
			r.Documents[1].Text = edited
			r.Documents[1].Version = &version
			_ = runCluster(t, s, r)
			if saved {
				if err := s.DidSave(nil, &protocol.DidSaveTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}}); err != nil {
					t.Fatal(err)
				}
			}
			lifecycleClose(t, s, uri)
			if saved && len(s.clusterValidation.byURI) == 0 {
				t.Fatal("saved preview close cleared findings")
			}
			if !saved && len(s.clusterValidation.byURI) != 0 {
				t.Fatal("discard retained unsaved validation")
			}
		})
	}
}
func TestLocalProblemsOpenCloseAndDiscard(t *testing.T) {
	s := NewServer()
	root := t.TempDir()
	s.workspaceRoots = []string{root}
	componentPath := filepath.Join(root, "component.yaml")
	uri := pathToDocumentURI(componentPath)
	if err := os.WriteFile(componentPath, []byte(clusterValidationComponent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "type.yaml"), []byte(clusterValidationType), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteCommand(nil, &protocol.ExecuteCommandParams{Command: postRenderCommand, Arguments: []any{uri}}); err != nil {
		t.Fatal(err)
	}
	original := missingWorkloadDiagnostic(s.postRenderByURI[uri])
	if original == nil {
		t.Fatal("missing local workload error")
	}
	if original.Range.End.Character-original.Range.Start.Character != 3 {
		t.Fatalf("not a visible name range: %+v", original.Range)
	}
	for i := 0; i < 2; i++ {
		if missingWorkloadDiagnostic(lifecycleOpen(t, s, uri, clusterValidationComponent)) == nil {
			t.Fatal("local open cleared finding")
		}
		if missingWorkloadDiagnostic(lifecycleClose(t, s, uri)) == nil {
			t.Fatal("local close cleared finding")
		}
	}
	lifecycleOpen(t, s, uri, clusterValidationComponent)
	edited := strings.Replace(clusterValidationComponent, "count: 1", "count: 2", 1)
	if err := s.UpdateDocument(uri, edited, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteCommand(nil, &protocol.ExecuteCommandParams{Command: postRenderCommand, Arguments: []any{uri}}); err != nil {
		t.Fatal(err)
	}
	lifecycleClose(t, s, uri)
	if len(s.postRenderByURI) != 0 {
		t.Fatal("discarded local edit retained results")
	}
}

func TestMissingWorkloadRangeInMultiDocumentYAML(t *testing.T) {
	text := clusterValidationComponent + "---\n" + strings.Replace(clusterValidationComponent, "name: app,", "name: second,", 1)
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	name := yamlMappingValue(yamlMappingValue(docs[1].Root, "metadata"), "name")
	diag := postRenderDiagnosticForText(post.Finding{Message: "no Workload for Component demo/second was found", Line: name.Line, Column: name.Column}, text)
	line := strings.Split(text, "\n")[diag.Range.Start.Line]
	if line[diag.Range.Start.Character:diag.Range.End.Character] != "second" {
		t.Fatalf("wrong YAML document: %+v", diag.Range)
	}
}
