// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const referenceURI = "openchoreo://ctx/namespaces/default/component/app.yaml"

func completeReferences(t *testing.T, s *Server, ctx, ns string, gen uint64, kinds []string, resources ...clusterLocation) {
	t.Helper()
	if resources == nil {
		resources = []clusterLocation{}
	}
	b, err := json.Marshal(locationSnapshot{ContextID: &ctx, Namespace: &ns, Generation: &gen, CompleteKinds: kinds, Resources: resources})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.locations.update(b); err != nil {
		t.Fatal(err)
	}
}
func referenceErrors(s *Server, uri, text string) []protocol.Diagnostic {
	var out []protocol.Diagnostic
	for _, d := range s.DiagnosticsForText(uri, text) {
		if d.Code != nil && d.Code.Value == missingReferenceCode {
			out = append(out, d)
		}
	}
	return out
}
func TestReferenceDiagnosticAndQuickFix(t *testing.T) {
	s := NewServer()
	resource := target("ComponentType", "auto-scale", "default")
	resource.WorkloadType = "deployment"
	completeReferences(t, s, "ctx", "default", 1, []string{"ComponentType"}, resource)
	text := definitionSource("ComponentType", `'deployment/auto-scal'`, "default")
	s.OpenDocument(referenceURI, text, 7)
	diags := referenceErrors(s, referenceURI, text)
	if len(diags) != 1 || *diags[0].Severity != protocol.DiagnosticSeverityError || diags[0].Message != "ComponentType 'deployment/auto-scal' was not found." {
		t.Fatalf("%+v", diags)
	}
	rng := diags[0].Range
	line := strings.Split(text, "\n")[rng.Start.Line]
	if line[rng.Start.Character:rng.End.Character] != "deployment/auto-scal" {
		t.Fatalf("wrong range: %+v in %s", rng, line)
	}
	got, err := s.CodeAction(nil, &protocol.CodeActionParams{TextDocument: protocol.TextDocumentIdentifier{URI: referenceURI}, Range: rng, Context: protocol.CodeActionContext{Diagnostics: diags}})
	if err != nil {
		t.Fatal(err)
	}
	var fix *protocol.CodeAction
	for _, a := range got.([]protocol.CodeAction) {
		if a.Title == "Change to deployment/auto-scale" {
			copy := a
			fix = &copy
		}
	}
	if fix == nil || !*fix.IsPreferred {
		t.Fatalf("missing fix: %+v", got)
	}
	edit := fix.Edit.DocumentChanges[0].(protocol.TextDocumentEdit)
	if *edit.TextDocument.Version != 7 {
		t.Fatal("edit is not version guarded")
	}
	token := edit.Edits[0].(protocol.TextEdit)
	updated := line[:rng.Start.Character] + token.NewText + line[rng.End.Character:]
	if updated != "    name: 'deployment/auto-scale'" {
		t.Fatal(updated)
	}
	corrected := strings.Replace(text, "auto-scal'", "auto-scale'", 1)
	if d := referenceErrors(s, referenceURI, corrected); len(d) != 0 {
		t.Fatalf("valid name flagged: %+v", d)
	}
}
func TestReferenceReadinessAndSnapshotLifecycle(t *testing.T) {
	s := NewServer()
	text := definitionSource("ComponentType", "deployment/missing", "default")
	for _, tc := range []struct {
		ctx, ns string
		kinds   []string
		want    int
	}{
		{"ctx", "default", nil, 0},               // refresh clear or legacy client
		{"ctx", "default", []string{"Trait"}, 0}, // failed ComponentType list
		{"ctx", "other", []string{"ComponentType"}, 0},
		{"ctx", "default", []string{"ComponentType"}, 1},  // empty successful list
		{"next", "default", []string{"ComponentType"}, 0}, // stale source context
	} {
		completeReferences(t, s, tc.ctx, tc.ns, 1, tc.kinds)
		if got := len(referenceErrors(s, referenceURI, text)); got != tc.want {
			t.Fatalf("%+v: got %d", tc, got)
		}
	}
	completeReferences(t, s, "ctx", "default", 2, []string{"ComponentType"})
	if err := s.locations.update([]byte(`{"contextId":"ctx","generation":3,"resources":[],"completeKinds":[""]}`)); err == nil {
		t.Fatal("accepted malformed readiness")
	}
	if len(referenceErrors(s, referenceURI, text)) != 1 {
		t.Fatal("malformed snapshot deleted valid state")
	}
	if err := s.locations.update([]byte(`{"contextId":null,"namespace":null,"generation":3,"resources":[]}`)); err != nil {
		t.Fatal(err)
	}
	if len(referenceErrors(s, referenceURI, text)) != 0 {
		t.Fatal("logout retained error")
	}
}
func TestReferenceScopeAndLocalPrecedence(t *testing.T) {
	s := NewServer()
	root := t.TempDir()
	if err := s.indexer.IndexWorkspace(root); err != nil {
		t.Fatal(err)
	}
	local := fmt.Sprintf("apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: service, namespace: default}\nspec: {workloadType: deployment}\n")
	if err := s.IndexFile(definitionURI(filepath.Join(root, "type.yaml")), []byte(local)); err != nil {
		t.Fatal(err)
	}
	remote := target("ComponentType", "service", "other")
	remote.WorkloadType = "deployment"
	trait := target("Trait", "service", "default")
	completeReferences(t, s, "ctx", "default", 1, []string{"ComponentType", "Trait"}, remote, trait)
	text := definitionSource("ComponentType", "deployment/service", "default")
	if len(referenceErrors(s, referenceURI, text)) != 0 {
		t.Fatal("local target did not take precedence")
	}
	text = definitionSource("ComponentType", "statefulset/service", "default")
	if len(referenceErrors(s, referenceURI, text)) != 1 {
		t.Fatal("wrong workload accepted")
	}
	text = definitionSource("ComponentType", "service", "other")
	// Virtual URI and YAML disagree: no diagnostic based on unrelated data.
	if len(referenceErrors(s, referenceURI, text)) != 0 {
		t.Fatal("mismatched source namespace checked")
	}
	text = definitionSource("ComponentType", "deployment/servic", "default")
	refs := s.referenceChecks(referenceURI, text)
	if len(refs) != 1 || len(refs[0].candidates) != 1 || refs[0].candidates[0] != "deployment/service" {
		t.Fatalf("bad candidate scope: %+v", refs)
	}
	// The same local reference works from a normal workspace file without feeds.
	s2 := NewServer()
	if err := s2.indexer.IndexWorkspace(root); err != nil {
		t.Fatal(err)
	}
	if err := s2.IndexFile(definitionURI(filepath.Join(root, "type.yaml")), []byte(local)); err != nil {
		t.Fatal(err)
	}
	if len(referenceErrors(s2, definitionURI(filepath.Join(root, "app.yaml")), text)) != 1 {
		t.Fatal("local-only diagnostic missing")
	}
}
func TestReferenceRanking(t *testing.T) {
	got := referenceMatches("deployment/servic", []string{"deployment/service", "deployment/serving", "completely-unrelated"})
	if len(got) != 2 || got[0].name != "deployment/service" {
		t.Fatalf("%+v", got)
	}
	if len(referenceMatches("abc", []string{"unrelated"})) != 0 {
		t.Fatal("offered unrelated name")
	}
	got = referenceMatches("a", []string{"A", "a"})
	if len(got) != 2 || got[0].name != "A" || got[0].score != got[1].score {
		t.Fatalf("tie order: %+v", got)
	}
}
func TestReferenceFilteringAndUTF16(t *testing.T) {
	s := NewServer()
	completeReferences(t, s, "ctx", "default", 1, []string{"ComponentType", "Trait"})
	for _, name := range []string{`"deployment/missing"`, `'deployment/missing'`, `deployment/missing`} {
		text := definitionSource("ComponentType", name, "default") + "  parameters:\n    reference: {kind: Trait, name: user-value}\n  environmentConfigs:\n    name: ordinary\n  template:\n    ref: {kind: Trait, name: template-value}\n"
		if d := referenceErrors(s, referenceURI, text); len(d) != 1 {
			t.Fatalf("%s: %+v", name, d)
		}
	}
	for _, name := range []string{`"${parameters.type}"`, `""`, `|\n      deployment/missing`} {
		if d := referenceErrors(s, referenceURI, definitionSource("ComponentType", name, "default")); len(d) != 0 {
			t.Fatalf("dynamic or incomplete scalar checked: %+v", d)
		}
	}
	text := strings.Replace(definitionSource("ComponentType", "missing", "default"), "    name: missing", `    other: "😀", name: missing`, 1)
	// An actual flow mapping puts a non-BMP character before the reference.
	text = strings.Replace(text, "  componentType:\n    kind: ComponentType\n    other: \"😀\", name: missing", "  componentType: {other: \"😀\", kind: ComponentType, name: \"missing\"}", 1)
	d := referenceErrors(s, referenceURI, text)
	if len(d) != 1 {
		t.Fatalf("unicode diagnostic: %+v", d)
	}
	line := strings.Split(text, "\n")[d[0].Range.Start.Line]
	// The single non-BMP rune occupies two UTF-16 units but four UTF-8 bytes.
	if int(d[0].Range.Start.Character) != strings.Index(line, "missing")-2 {
		t.Fatalf("incorrect UTF-16 range: %+v in %s", d[0].Range, line)
	}
}
func TestReferenceReleaseScopingAndDeduplication(t *testing.T) {
	s := NewServer()
	a, b := target("ComponentRelease", "app-v1", "default"), target("ComponentRelease", "other-v1", "default")
	a.ComponentName = "app"
	b.ComponentName = "other"
	completeReferences(t, s, "ctx", "default", 1, []string{"ComponentRelease"}, a, a, b)
	text := "apiVersion: openchoreo.dev/v1alpha1\nkind: ReleaseBinding\nmetadata: {name: binding}\nspec:\n  owner: {componentName: app}\n  releaseName: app-v2\n"
	refs := s.referenceChecks(referenceURI, text)
	if len(refs) != 1 || !refs[0].missing || len(refs[0].candidates) != 1 || refs[0].candidates[0] != "app-v1" {
		t.Fatalf("%+v", refs)
	}
}
func TestReferenceConcurrentUpdates(t *testing.T) {
	s := NewServer()
	text := definitionSource("ComponentType", "missing", "default")
	s.OpenDocument(referenceURI, text, 1)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				completeReferences(t, s, "ctx", "default", 1, []string{"ComponentType"})
				_ = referenceErrors(s, referenceURI, text)
				_ = s.referenceActions(referenceURI, text, 1, protocol.Range{End: protocol.Position{Line: 20}})
			}
		}()
	}
	wg.Wait()
}
func TestReferenceQuickFixOverJSONRPC(t *testing.T) {
	s := NewServer()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	server := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewBufferedStream(left, jsonrpc2.VSCodeObjectCodec{}), jsonrpc2.HandlerWithError(func(ctx context.Context, c *jsonrpc2.Conn, r *jsonrpc2.Request) (any, error) {
		v, _, _, err := s.server.Handler.Handle(&glsp.Context{Method: r.Method, Params: *r.Params, Notify: func(method string, params any) { _ = c.Notify(ctx, method, params) }})
		return v, err
	}))
	defer server.Close()
	client := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewBufferedStream(right, jsonrpc2.VSCodeObjectCodec{}), jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) { return nil, nil }))
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var initialized protocol.InitializeResult
	if err := client.Call(ctx, "initialize", &protocol.InitializeParams{}, &initialized); err != nil {
		t.Fatal(err)
	}
	defer s.StopWatcher()
	text := definitionSource("ComponentType", "servic", "default")
	s.OpenDocument(referenceURI, text, 4)
	id, ns, gen := "ctx", "default", uint64(1)
	snapshot := locationSnapshot{ContextID: &id, Namespace: &ns, Generation: &gen, CompleteKinds: []string{"ComponentType"}, Resources: []clusterLocation{target("ComponentType", "service", "default")}}
	if err := client.Call(ctx, resourceLocationsMethod, snapshot, nil); err != nil {
		t.Fatal(err)
	}
	var actions json.RawMessage
	p := protocol.CodeActionParams{TextDocument: protocol.TextDocumentIdentifier{URI: referenceURI}, Range: protocol.Range{Start: protocol.Position{Line: 6}, End: protocol.Position{Line: 6, Character: 20}}}
	if err := client.Call(ctx, "textDocument/codeAction", p, &actions); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(actions), `"title":"Change to service"`) || !strings.Contains(string(actions), `"code":"unresolved-resource-reference"`) || !strings.Contains(string(actions), `"version":4`) {
		t.Fatalf("invalid action on wire: %s", actions)
	}
	snapshot.CompleteKinds = nil
	snapshot.Resources = []clusterLocation{}
	if err := client.Call(ctx, resourceLocationsMethod, snapshot, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Call(ctx, "textDocument/codeAction", p, &actions); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(actions), `"title":"Change to service"`) {
		t.Fatal("stale fix after clear")
	}
}

func TestReferenceInitializationWaitsForClusterFeeds(t *testing.T) {
	s := NewServer()
	if _, err := s.Initialize(&glsp.Context{}, &protocol.InitializeParams{InitializationOptions: map[string]any{"clusterReferenceFeeds": true}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := s.indexer.IndexWorkspace(root); err != nil {
		t.Fatal(err)
	}
	uri := definitionURI(filepath.Join(root, "app.yaml"))
	text := definitionSource("ComponentType", "deployment/missing", "default")
	if len(referenceErrors(s, uri, text)) != 0 {
		t.Fatal("reported missing cluster reference before first feed")
	}
	completeReferences(t, s, "ctx", "default", 1, []string{"ComponentType"})
	if len(referenceErrors(s, uri, text)) != 1 {
		t.Fatal("complete empty feed did not enable validation")
	}
}

func TestReferenceActionsTiesAndLimit(t *testing.T) {
	s := NewServer()
	completeReferences(t, s, "ctx", "default", 1, []string{"Trait"}, target("Trait", "service-a", "default"), target("Trait", "service-b", "default"), target("Trait", "service-d", "default"), target("Trait", "service-e", "default"), target("Trait", "service-c", "other"))
	text := "apiVersion: openchoreo.dev/v1alpha1\nkind: Component\nmetadata: {name: app}\nspec:\n  traits: [{kind: Trait, name: service-c}]\n"
	actions := s.referenceActions(referenceURI, text, 1, protocol.Range{})
	if len(actions) != 3 {
		t.Fatalf("expected three scoped suggestions, got %+v", actions)
	}
	for i, action := range actions {
		if *action.IsPreferred {
			t.Fatal("tied score was preferred")
		}
		if action.Title != []string{"Change to service-a", "Change to service-b", "Change to service-d"}[i] {
			t.Fatalf("unstable tie order: %s", action.Title)
		}
	}
}
