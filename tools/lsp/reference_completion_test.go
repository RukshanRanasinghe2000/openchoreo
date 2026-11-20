// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openchoreo/openchoreo/internal/pathutil"
	completion "github.com/openchoreo/openchoreo/tools/lsp/code-completion"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func referenceSource(kind string) string {
	return "apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: type}\nspec:\n  allowedTraits:\n    - kind: " + kind + "\n      name: "
}
func referenceParams(uri, text string) *protocol.CompletionParams {
	lines := strings.Split(text, "\n")
	return &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: uint32(len(lines) - 1), Character: uint32(len(lines[len(lines)-1]))}}}
}
func referenceLabels(list protocol.CompletionList) []string {
	var labels []string
	for _, item := range list.Items {
		if data, ok := item.Data.(map[string]any); ok && data["openchoreoResourceReference"] == true {
			labels = append(labels, item.Label)
		}
	}
	sort.Strings(labels)
	return labels
}
func TestResourceReferenceNamesPreserveWorkspaceNamespaceAndContext(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for path, resource := range map[string]struct{ kind, name, ns string }{
		filepath.Join(a, "cluster.yaml"):   {"ClusterTrait", "health-probes", ""},
		filepath.Join(b, "unrelated.yaml"): {"ClusterTrait", "other-root", ""},
		filepath.Join(a, "default.yaml"):   {"Trait", "default-trait", "default"},
		filepath.Join(a, "other.yaml"):     {"Trait", "other-trait", "other"},
	} {
		text := fmt.Sprintf("apiVersion: openchoreo.dev/v1alpha1\nkind: %s\nmetadata: {name: %s, namespace: %q}\nspec: {}\n", resource.kind, resource.name, resource.ns)
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer()
	if err := s.indexer.IndexWorkspaces([]string{a, b}); err != nil {
		t.Fatal(err)
	}
	putLocations(t, s, "ctx", 1)
	if err := s.cluster.update("openchoreo/updateResources", []byte(`{"ClusterTrait":["health-probes","live-only"],"Trait":["live-default"]}`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		uri, kind string
		want      []string
	}{
		{pathutil.FileURI(filepath.Join(a, "type.yaml")), "ClusterTrait", []string{"health-probes", "live-only"}},
		{pathutil.FileURI(filepath.Join(a, "type.yaml")), "Trait", []string{"default-trait", "live-default"}},
		{"openchoreo://ctx/namespaces/other/component-type/type.yaml", "Trait", []string{"other-trait"}},
		{"openchoreo://old/namespaces/other/component-type/type.yaml", "Trait", []string{"other-trait"}},
	} {
		text := referenceSource(tc.kind)
		s.OpenDocument(tc.uri, text, 1)
		result, err := s.Completion(nil, referenceParams(tc.uri, text))
		if err != nil {
			t.Fatal(err)
		}
		labels := referenceLabels(result.(protocol.CompletionList))
		if strings.Join(labels, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%s kind=%s: %v want %v", tc.uri, tc.kind, labels, tc.want)
		}
	}
}
func TestResourceReferenceCompletionsOverJSONRPC(t *testing.T) {
	s := NewServer()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	server := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewBufferedStream(left, jsonrpc2.VSCodeObjectCodec{}), jsonrpc2.HandlerWithError(func(ctx context.Context, c *jsonrpc2.Conn, r *jsonrpc2.Request) (any, error) {
		result, _, _, err := s.server.Handler.Handle(&glsp.Context{Method: r.Method, Params: *r.Params, Notify: func(method string, params any) { _ = c.Notify(ctx, method, params) }})
		return result, err
	}))
	defer server.Close()
	client := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewBufferedStream(right, jsonrpc2.VSCodeObjectCodec{}), jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) { return nil, nil }))
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	uri := "openchoreo://ctx/cluster-component-type/type.yaml"
	text := referenceSource("ClusterTrait")
	s.OpenDocument(uri, text, 1)
	var initialized protocol.InitializeResult
	if err := client.Call(ctx, "initialize", &protocol.InitializeParams{}, &initialized); err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{{"health-probes", "health-probes", "logging"}, {"replacement"}, {}} {
		payload := map[string]any{"ClusterTrait": names, "Trait": []string{"unrelated"}}
		if err := client.Notify(ctx, "openchoreo/updateResources", payload); err != nil {
			t.Fatal(err)
		}
		// A call through the same dispatch provides a notification processing barrier.
		if err := client.Call(ctx, "openchoreo/updateResources", payload, nil); err != nil {
			t.Fatal(err)
		}
		var list protocol.CompletionList
		if err := client.Call(ctx, "textDocument/completion", referenceParams(uri, text), &list); err != nil {
			t.Fatal(err)
		}
		want := map[string]bool{}
		for _, name := range names {
			want[name] = true
		}
		got := referenceLabels(list)
		if len(got) != len(want) {
			t.Fatalf("wire completion snapshot: %v, want %v", got, names)
		}
		for _, label := range got {
			if !want[label] {
				t.Fatal("unrelated kind leaked", label)
			}
		}
	}
}
func TestResourceReferenceConcurrentFeedUpdates(t *testing.T) {
	s := NewServer()
	text := referenceSource("ClusterTrait")
	uri := "openchoreo://ctx/cluster-component-type/type.yaml"
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if err := s.cluster.update("openchoreo/updateResources", []byte(`{"ClusterTrait":["health-probes"]}`)); err != nil {
					t.Error(err)
				}
				idx := s.clusterCompletionIndex(uri, text)
				lines := strings.Split(text, "\n")
				result, err := completion.HandleWithIndex(text, len(lines), len(lines[len(lines)-1])+1, idx, uri)
				if err != nil {
					t.Error(err)
					continue
				}
				data, _ := json.Marshal(result)
				if !containsLabel(data, "health-probes") {
					t.Errorf("concurrent snapshot lost resource: %s", data)
				}
			}
		}()
	}
	wg.Wait()
}
