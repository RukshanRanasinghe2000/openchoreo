// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const clusterValidationType = `apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
metadata: {name: web, namespace: default}
spec:
  workloadType: deployment
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        count: {type: integer, default: 1}
  preRenderValidations:
    - rule: '${parameters.count > 0}'
      message: count must be positive
  postRenderValidations:
    - target: {group: '', version: v1, kind: ConfigMap}
      rule: '${false}'
      message: cluster post-render marker
  resources:
    - id: config
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata: {name: '${metadata.name}', namespace: '${metadata.namespace}'}
        data: {purpose: test}
`
const clusterValidationComponent = `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata: {name: app, namespace: default}
spec:
  owner: {projectName: demo}
  componentType: {kind: ComponentType, name: deployment/web}
  parameters: {count: 1}
`
const clusterValidationWorkload = `apiVersion: openchoreo.dev/v1alpha1
kind: Workload
metadata: {name: app-workload, namespace: default}
spec:
  owner: {projectName: demo, componentName: app}
  container: {image: 'nginx:1.25'}
`

func clusterValidationFixture(t *testing.T) (*Server, clusterValidationRequest) {
	t.Helper()
	s := NewServer()
	completeReferences(t, s, "ctx", "default", 1, nil)
	return s, clusterValidationRequest{ContextID: "ctx", Namespace: "default", Generation: 1, Documents: []clusterValidationDocument{
		{URI: "openchoreo://ctx/namespaces/default/component-type/web.yaml", Text: clusterValidationType},
		{URI: "openchoreo://ctx/namespaces/default/component/app.yaml", Text: clusterValidationComponent},
		{URI: "openchoreo://ctx/namespaces/default/workload/app-workload.yaml", Text: clusterValidationWorkload},
	}}
}
func runCluster(t *testing.T, s *Server, r clusterValidationRequest) postRenderResult {
	t.Helper()
	result, err := s.ExecuteCommand(nil, &protocol.ExecuteCommandParams{Command: clusterValidationCommand, Arguments: []any{r}})
	if err != nil {
		t.Fatal(err)
	}
	return result.(postRenderResult)
}
func TestClusterValidationPreAndPostRender(t *testing.T) {
	s, r := clusterValidationFixture(t)
	got := runCluster(t, s, r)
	if !strings.Contains(strings.Join(got.Details, "\n"), "cluster post-render marker") {
		t.Fatalf("post-render did not run: %+v", got)
	}
	if got.Files != 3 || got.Errors == 0 {
		t.Fatalf("bad summary: %+v", got)
	}
	for uri, diags := range s.clusterValidation.byURI {
		if !strings.HasPrefix(uri, "openchoreo://ctx/") || len(diags) == 0 {
			t.Fatalf("fabricated target: %s", uri)
		}
	}
	for _, doc := range r.Documents {
		if s.GetDocument(doc.URI) != nil {
			t.Fatal("snapshot created an open document")
		}
	}
	if len(s.indexer.DefinitionSnapshot(r.Documents[1].URI)) != 0 {
		t.Fatal("cluster input leaked into local index")
	}
	r.Documents[1].Text = strings.Replace(clusterValidationComponent, "count: 1", "count: -1", 1)
	got = runCluster(t, s, r)
	if !strings.Contains(strings.Join(got.Details, "\n"), "count must be positive") {
		t.Fatalf("pre-render did not run: %+v", got)
	}
}
func TestClusterValidationMalformedAndStaleSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*clusterValidationRequest)
	}{
		{"context", func(r *clusterValidationRequest) { r.ContextID = "other" }},
		{"namespace", func(r *clusterValidationRequest) { r.Namespace = "other" }},
		{"generation", func(r *clusterValidationRequest) { r.Generation = 0 }},
		{"file URI", func(r *clusterValidationRequest) { r.Documents[0].URI = "file:///fake.yaml" }},
		{"wrong identity", func(r *clusterValidationRequest) {
			r.Documents[0].Text = strings.Replace(clusterValidationType, "name: web", "name: other", 1)
		}},
		{"wrong kind", func(r *clusterValidationRequest) {
			r.Documents[0].Text = strings.Replace(clusterValidationType, "kind: ComponentType", "kind: Trait", 1)
		}},
		{"duplicate", func(r *clusterValidationRequest) { r.Documents = append(r.Documents, r.Documents[0]) }},
		{"version", func(r *clusterValidationRequest) { v := int(1) << 40; r.Documents[0].Version = &v }},
		{"missing resources", func(r *clusterValidationRequest) { r.Documents = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := clusterValidationFixture(t)
			tc.change(&r)
			if _, err := s.runClusterValidation([]any{r}); err == nil {
				t.Fatal("accepted malformed snapshot")
			}
			if len(s.clusterValidation.byURI) != 0 {
				t.Fatal("published invalid input")
			}
		})
	}
}
func TestClusterValidationUnsavedAndLifecycle(t *testing.T) {
	s, r := clusterValidationFixture(t)
	uri := r.Documents[1].URI
	s.OpenDocument(uri, clusterValidationComponent, 4)
	if _, err := s.runClusterValidation([]any{r}); err == nil {
		t.Fatal("accepted remote bytes over an open editor")
	}
	version := 4
	r.Documents[1].Version = &version
	_ = runCluster(t, s, r)
	s.postRenderByURI = map[string][]protocol.Diagnostic{"file:///local.yaml": {{Message: "local marker"}}}
	if err := s.UpdateDocument(uri, strings.Replace(clusterValidationComponent, "count: 1", "count: 2", 1), 5); err != nil {
		t.Fatal(err)
	}
	s.invalidatePostRenderForEdit(uri)
	if len(s.clusterValidation.byURI) != 0 {
		t.Fatal("edit retained cluster findings")
	}
	if len(s.postRenderByURI) != 1 {
		t.Fatal("cluster edit cleared local results")
	}
	s.OpenDocument(uri, clusterValidationComponent, 4)
	_ = runCluster(t, s, r)
	s.invalidateClusterValidation("")
	if len(s.clusterValidation.byURI) != 0 {
		t.Fatal("refresh retained findings")
	}
	if len(s.mergePostRenderDiagnostics(uri, nil)) != 0 {
		t.Fatal("stale diagnostics merged")
	}
}
func TestClusterValidationSyntaxEmptyAndMissingNamespace(t *testing.T) {
	s, r := clusterValidationFixture(t)
	r.Documents[1].Text = "apiVersion: openchoreo.dev/v1alpha1\nkind: Component\nspec: [broken"
	if got := runCluster(t, s, r); !strings.Contains(strings.Join(got.Details, "\n"), "yaml:") {
		t.Fatalf("missing syntax diagnostic: %+v", got)
	}
	r.Documents = []clusterValidationDocument{}
	if got := runCluster(t, s, r); got.Files != 0 || !strings.Contains(got.Message, "No renderable") {
		t.Fatal(got)
	}
	// Omitted metadata.namespace inherits the real virtual URI namespace.
	s, r = clusterValidationFixture(t)
	for i := range r.Documents {
		r.Documents[i].Text = strings.ReplaceAll(r.Documents[i].Text, ", namespace: default", "")
	}
	got := runCluster(t, s, r)
	if !strings.Contains(strings.Join(got.Details, "\n"), "cluster post-render marker") {
		t.Fatalf("namespace inheritance failed: %+v", got)
	}
}
func TestClusterValidationNotificationInvalidatesResults(t *testing.T) {
	s, r := clusterValidationFixture(t)
	_ = runCluster(t, s, r)
	raw, _ := json.Marshal(locationSnapshot{ContextID: &r.ContextID, Namespace: &r.Namespace, Generation: &r.Generation, Resources: []clusterLocation{}})
	_, _, _, err := s.server.Handler.Handle(&glsp.Context{Method: resourceLocationsMethod, Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.clusterValidation.byURI) != 0 {
		t.Fatal("feed refresh did not invalidate validation results")
	}
}
func TestClusterValidationConcurrentInvalidation(t *testing.T) {
	s, r := clusterValidationFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				_, _ = s.runClusterValidation([]any{r})
				s.invalidateClusterValidation("")
				_ = s.mergePostRenderDiagnostics(r.Documents[0].URI, nil)
			}
		}()
	}
	wg.Wait()
}
