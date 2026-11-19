// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func definitionURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
func definitionSource(kind, name, namespace string) string {
	return fmt.Sprintf("apiVersion: openchoreo.dev/v1alpha1\nkind: Component\nmetadata: {name: app, namespace: %s}\nspec:\n  componentType:\n    kind: %s\n    name: %s\n", namespace, kind, name)
}
func callDefinition(t *testing.T, s *Server, uri, text, needle string) []protocol.Location {
	t.Helper()
	s.OpenDocument(uri, text, 1)
	lines := strings.Split(text, "\n")
	line, col := 0, 0
	for i, l := range lines {
		if j := strings.Index(l, needle); j >= 0 {
			line = i
			col = j + 1
			break
		}
	}
	got, err := s.Definition(&glsp.Context{}, &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: uint32(line), Character: uint32(col)}}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		return nil
	}
	return got.([]protocol.Location)
}
func putLocations(t *testing.T, s *Server, contextID string, generation uint64, resources ...clusterLocation) {
	t.Helper()
	ns := "default"
	if resources == nil {
		resources = []clusterLocation{}
	}
	b, _ := json.Marshal(locationSnapshot{ContextID: &contextID, Namespace: &ns, Generation: &generation, Resources: resources})
	if err := s.locations.update(b); err != nil {
		t.Fatal(err)
	}
}
func target(kind, name, namespace string) clusterLocation {
	typ := map[string]string{"ComponentType": "component-type", "ClusterComponentType": "cluster-component-type", "Trait": "trait", "ClusterTrait": "cluster-trait", "ComponentRelease": "component-release"}[kind]
	path := "/" + typ + "/" + name + ".yaml"
	if namespace != "" {
		path = "/namespaces/" + namespace + path
	}
	return clusterLocation{Kind: kind, Name: name, Namespace: namespace, URI: "openchoreo://ctx" + path}
}
func TestDefinitionLocalAndClusterPriority(t *testing.T) {
	for _, kind := range []string{"ComponentType", "ClusterComponentType", "Trait", "ClusterTrait"} {
		t.Run(kind, func(t *testing.T) {
			s := NewServer()
			root := t.TempDir()
			if err := s.indexer.IndexWorkspace(root); err != nil {
				t.Fatal(err)
			}
			ns := "default"
			remoteNS := ns
			if strings.HasPrefix(kind, "Cluster") {
				remoteNS = ""
			}
			remote := target(kind, "service", remoteNS)
			putLocations(t, s, "ctx", 1, remote)
			source := definitionSource(kind, "service", ns)
			if strings.Contains(kind, "Trait") {
				source = fmt.Sprintf("apiVersion: openchoreo.dev/v1alpha1\nkind: Component\nmetadata: {name: app, namespace: default}\nspec:\n  traits:\n  - kind: %s\n    name: service\n", kind)
			}
			virtual := "openchoreo://ctx/namespaces/default/component/app.yaml"
			got := callDefinition(t, s, virtual, source, "service")
			if len(got) != 1 || got[0].URI != remote.URI || got[0].Range.Start != got[0].Range.End {
				t.Fatalf("remote: %+v", got)
			}
			localPath := filepath.Join(root, "type # %.yaml")
			content := fmt.Sprintf("apiVersion: openchoreo.dev/v1alpha1\nkind: %s\nmetadata:\n  name: service\n  namespace: default\nspec: {}\n", kind)
			if err := os.WriteFile(localPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.indexer.IndexWorkspace(root); err != nil {
				t.Fatal(err)
			}
			local := definitionURI(localPath)
			for _, uri := range []string{definitionURI(filepath.Join(root, "component.yaml")), virtual} {
				got = callDefinition(t, s, uri, source, "service")
				if len(got) != 1 || got[0].URI != local || got[0].Range.Start.Line != 3 {
					t.Fatalf("local priority: %+v", got)
				}
			}
		})
	}
}
func TestDefinitionIdentityAndSnapshots(t *testing.T) {
	s := NewServer()
	r := target("ComponentType", "service", "default")
	putLocations(t, s, "ctx", 1, r)
	text := definitionSource("ComponentType", "service", "default")
	source := "openchoreo://ctx/namespaces/default/component/app.yaml"
	for _, uri := range []string{"openchoreo://other/namespaces/default/component/app.yaml", "openchoreo:/namespaces/default/component/app.yaml", "https://ctx/app.yaml"} {
		if got := callDefinition(t, s, uri, text, "service"); len(got) != 0 {
			t.Fatal(uri, got)
		}
	}
	for _, tt := range []struct{ k, n string }{{"Trait", "default"}, {"ComponentType", "other"}} {
		if got := callDefinition(t, s, definitionURI(filepath.Join(t.TempDir(), "source.yaml")), definitionSource(tt.k, "service", tt.n), "service"); len(got) != 0 {
			t.Fatal(got)
		}
	}
	for _, bad := range []string{`null`, `{}`, `{"generation":2,"resources":[]}`, `{"contextId":"ctx","generation":2,"resources":[{"kind":"ComponentType","name":"service","namespace":"other","uri":"openchoreo://ctx/namespaces/default/component-type/service.yaml"}]}`, `{"contextId":"ctx","generation":2,"resources":[{"kind":"ComponentType","name":"service","uri":"file:///fake.yaml"}]}`} {
		err := s.locations.update([]byte(bad))
		// A null context with an empty list is a valid logout snapshot.
		if bad == `{"generation":2,"resources":[]}` {
			if err != nil {
				t.Fatal(err)
			}
			putLocations(t, s, "ctx", 3, r)
			continue
		}
		if err == nil {
			t.Fatalf("accepted malformed %s", bad)
		}
	}
	if got := callDefinition(t, s, source, text, "service"); len(got) != 1 {
		t.Fatal("bad updates removed valid snapshot", got)
	}
	if err := s.locations.update([]byte(`{"contextId":"ctx","generation":0,"resources":[]}`)); err == nil {
		t.Fatal("accepted stale generation")
	}
	putLocations(t, s, "ctx", 4)
	if got := callDefinition(t, s, source, text, "service"); len(got) != 0 {
		t.Fatal("clear failed", got)
	}
	putLocations(t, s, "other", 5)
	if got := callDefinition(t, s, source, text, "service"); len(got) != 0 {
		t.Fatal("context change retained navigation", got)
	}
}
func TestDefinitionScopedReleasesAndDuplicates(t *testing.T) {
	s := NewServer()
	first := target("ComponentRelease", "r1", "default")
	first.ComponentName = "app"
	other := target("ComponentRelease", "r2", "default")
	other.ComponentName = "other"
	putLocations(t, s, "ctx", 1, first, first, other)
	text := "apiVersion: openchoreo.dev/v1alpha1\nkind: ReleaseBinding\nmetadata: {name: b}\nspec:\n  owner: {componentName: app}\n  releaseName: r1\n"
	source := "openchoreo://ctx/namespaces/default/release-binding/b.yaml"
	got := callDefinition(t, s, source, text, "r1")
	if len(got) != 1 || got[0].URI != first.URI {
		t.Fatal(got)
	}
	if got := callDefinition(t, s, source, strings.Replace(text, "r1", "r2", 1), "r2"); len(got) != 0 {
		t.Fatal("other component", got)
	}
}
func TestDefinitionMultiRootAndOpenedVirtualRange(t *testing.T) {
	s := NewServer()
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		if err := os.WriteFile(filepath.Join(root, "type.yaml"), []byte("apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: service}\nspec: {}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.indexer.IndexWorkspaces(roots); err != nil {
		t.Fatal(err)
	}
	text := definitionSource("ComponentType", "service", "default")
	for _, root := range roots {
		got := callDefinition(t, s, definitionURI(filepath.Join(root, "app.yaml")), text, "service")
		if len(got) != 1 || got[0].URI != definitionURI(filepath.Join(root, "type.yaml")) {
			t.Fatal(got)
		}
	}
	putLocations(t, s, "ctx", 1, target("ComponentType", "service", "default"))
	got := callDefinition(t, s, "openchoreo://ctx/namespaces/default/component/app.yaml", text, "service")
	if len(got) != 2 {
		t.Fatal("virtual source must not guess a workspace", got)
	}
	s = NewServer()
	r := target("ComponentType", "service", "default")
	putLocations(t, s, "ctx", 1, r)
	s.OpenDocument(r.URI, "apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata:\n  name: service\nspec: {}", 1)
	got = callDefinition(t, s, "openchoreo://ctx/namespaces/default/component/app.yaml", text, "service")
	if len(got) != 1 || got[0].Range.Start.Line != 3 {
		t.Fatal(got)
	}
}
func TestDefinitionConcurrentUpdates(t *testing.T) {
	s := NewServer()
	r := target("ComponentType", "service", "default")
	putLocations(t, s, "ctx", 1, r)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				putLocations(t, s, "ctx", 1, r)
				_ = callDefinition(t, s, "openchoreo://ctx/namespaces/default/component/app.yaml", definitionSource("ComponentType", "service", "default"), "service")
			}
		}()
	}
	wg.Wait()
}
func TestDefinitionNotificationOverJSONRPC(t *testing.T) {
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
	id, ns, gen := "ctx", "default", uint64(1)
	snapshot := locationSnapshot{ContextID: &id, Namespace: &ns, Generation: &gen, Resources: []clusterLocation{target("ComponentType", "service", "default")}}
	if err := client.Notify(ctx, resourceLocationsMethod, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := client.Call(ctx, resourceLocationsMethod, snapshot, nil); err != nil {
		t.Fatal(err)
	}
	source := "openchoreo://ctx/namespaces/default/component/app.yaml"
	s.OpenDocument(source, definitionSource("ComponentType", "service", "default"), 1)
	var initialized protocol.InitializeResult
	if err := client.Call(ctx, "initialize", &protocol.InitializeParams{}, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.Capabilities.DefinitionProvider != true {
		t.Fatal("definition capability missing")
	}
	if err := client.Notify(ctx, "initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	var got []protocol.Location
	params := protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: source}, Position: protocol.Position{Line: 6, Character: 12}}}
	if err := client.Call(ctx, "textDocument/definition", params, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URI != snapshot.Resources[0].URI {
		t.Fatal(got)
	}

}

func TestDefinitionImplicitComponentTypePrefersSpecificKind(t *testing.T) {
	s := NewServer()
	root := t.TempDir()
	if err := s.indexer.IndexWorkspace(root); err != nil {
		t.Fatal(err)
	}
	targetURI := definitionURI(filepath.Join(root, "type.yaml"))
	for uri, text := range map[string]string{
		targetURI: "apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: service}\nspec: {}",
		definitionURI(filepath.Join(root, "unrelated.yaml")): "apiVersion: openchoreo.dev/v1alpha1\nkind: Component\nmetadata: {name: service}\nspec: {}",
	} {
		if err := s.indexer.UpdateFile(uri, []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	text := strings.Replace(definitionSource("ComponentType", "service", "default"), "    kind: ComponentType\n", "", 1)
	got := callDefinition(t, s, definitionURI(filepath.Join(root, "app.yaml")), text, "service")
	if len(got) != 1 || got[0].URI != targetURI {
		t.Fatalf("implicit componentType resolved a less-specific kind: %+v", got)
	}
}
