// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
	post "github.com/openchoreo/openchoreo/tools/lint/post-render-validator"
	pre "github.com/openchoreo/openchoreo/tools/lint/pre-render-validator"
	"github.com/openchoreo/openchoreo/tools/lint/targetschema"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

const clusterValidationCommand = "openchoreo.runClusterValidation"

type clusterValidationDocument struct {
	URI     string `json:"uri"`
	Text    string `json:"text"`
	Version *int   `json:"version,omitempty"`
}
type clusterValidationRequest struct {
	ContextID   string                      `json:"contextId"`
	Namespace   string                      `json:"namespace"`
	Generation  uint64                      `json:"generation"`
	ProjectName string                      `json:"projectName,omitempty"`
	ActiveURI   string                      `json:"activeUri,omitempty"`
	Documents   []clusterValidationDocument `json:"documents"`
}
type clusterValidationState struct {
	mu     sync.Mutex
	epoch  uint64
	inputs map[string]string
	byURI  map[string][]protocol.Diagnostic
}

func (s *Server) clearClusterValidation() []string {
	s.clusterValidation.mu.Lock()
	defer s.clusterValidation.mu.Unlock()
	s.clusterValidation.epoch++
	uris := keysOfDiagnosticMap(s.clusterValidation.byURI)
	s.clusterValidation.byURI = nil
	s.clusterValidation.inputs = nil
	return uris
}
func (s *Server) invalidateClusterValidation(uri string) {
	if uri != "" {
		s.clusterValidation.mu.Lock()
		_, affected := s.clusterValidation.inputs[uri]
		s.clusterValidation.mu.Unlock()
		if !affected {
			return
		}
	}
	s.republishStructural(s.clearClusterValidation(), "")
}
func (s *Server) mergeClusterValidation(uri string, diags []protocol.Diagnostic) []protocol.Diagnostic {
	s.clusterValidation.mu.Lock()
	cached := append([]protocol.Diagnostic(nil), s.clusterValidation.byURI[uri]...)
	s.clusterValidation.mu.Unlock()
	return append(append([]protocol.Diagnostic(nil), diags...), cached...)
}
func (s *Server) clusterValidationContextCurrent(r clusterValidationRequest) bool {
	s.locations.mu.RLock()
	defer s.locations.mu.RUnlock()
	snap := s.locations.snapshot
	return snap.ContextID != nil && *snap.ContextID == r.ContextID && snap.Namespace != nil && *snap.Namespace == r.Namespace && snap.Generation != nil && *snap.Generation == r.Generation
}

// validateClusterSnapshot checks resource identity before changing any diagnostic
// state. All inputs are real provider-backed URIs; none are filesystem paths.
func (s *Server) validateClusterSnapshot(r clusterValidationRequest) error {
	if r.ContextID == "" || r.Namespace == "" || r.Documents == nil || !s.clusterValidationContextCurrent(r) {
		return fmt.Errorf("cluster validation snapshot is stale or missing a context/namespace; refresh resources and retry")
	}
	seen := map[string]bool{}
	identities := map[string]bool{}
	for _, d := range r.Documents {
		u, err := url.Parse(d.URI)
		if err != nil || u.Scheme != "openchoreo" || u.Host != r.ContextID || u.User != nil || u.Fragment != "" || (u.RawQuery != "" && u.RawQuery != "readonly") {
			return fmt.Errorf("invalid cluster validation resource URI")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if !(len(parts) == 2 || len(parts) == 4 && parts[0] == "namespaces" && parts[1] == r.Namespace) || !strings.HasSuffix(u.Path, ".yaml") {
			return fmt.Errorf("resource URI is outside the validation namespace")
		}
		if seen[d.URI] {
			return fmt.Errorf("duplicate resource URI in validation snapshot")
		}
		seen[d.URI] = true
		if d.Version != nil && (*d.Version < math.MinInt32 || *d.Version > math.MaxInt32) {
			return fmt.Errorf("invalid document version")
		}
		if open := s.GetDocument(d.URI); open != nil && (d.Version == nil || *d.Version != open.Version || d.Text != open.Text) {
			return fmt.Errorf("Explorer document changed; run validation again")
		}
		docs, err := parser.ParseYAML([]byte(d.Text))
		if err != nil {
			continue
		} // syntax errors are findings, not transport failures
		if len(docs) != 1 || !parser.IsOpenChoreoAPIVersion(docs[0].APIVersion) {
			return fmt.Errorf("cluster validation expects one OpenChoreo resource per URI")
		}
		doc := docs[0]
		identity := doc.Kind + "/" + doc.Metadata.Name
		if identities[identity] {
			return fmt.Errorf("duplicate resource identity in validation snapshot")
		}
		identities[identity] = true
		name := strings.TrimSuffix(parts[len(parts)-1], ".yaml")
		if parts[len(parts)-2] != clusterResourceURIType(doc.Kind) || doc.Metadata.Name != name || strings.HasPrefix(doc.Kind, "Cluster") != (len(parts) == 2) || doc.Metadata.Namespace != "" && doc.Metadata.Namespace != r.Namespace {
			return fmt.Errorf("resource YAML identity does not match its Explorer URI")
		}
	}
	return nil
}

// Cluster rendering is an adapter around the same in-memory validation engines
// used for local projects. The server performs no API requests or disk writes.
func (s *Server) runClusterValidation(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("missing cluster validation snapshot")
	}
	raw, err := json.Marshal(args[0])
	if err != nil {
		return nil, err
	}
	var request clusterValidationRequest
	if err = json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if request.ProjectName != "" {
		if request.ContextID == "" || request.Namespace == "" || request.Documents == nil || !s.clusterValidationContextCurrent(request) {
			return nil, fmt.Errorf("cluster validation snapshot is stale or missing a context/namespace; refresh resources and retry")
		}

		var sources []renderingSource
		for _, input := range request.Documents {
			docs, e := parser.ParseYAML([]byte(input.Text))
			if e != nil {
				sources = append(sources, renderingSource{URI: input.URI, Text: input.Text, Err: e})
				continue
			}
			for _, doc := range docs {
				if !strings.HasPrefix(doc.Kind, "Cluster") && doc.Metadata.Namespace == "" {
					setSnapshotNamespace(doc, request.Namespace)
				}
				sources = append(sources, renderingSource{URI: input.URI, Text: input.Text, Doc: doc})
			}
		}
		chosen, e := selectRenderingProject(sources, &renderingProjectScope{ProjectName: request.ProjectName, Namespace: request.Namespace, ActiveURI: request.ActiveURI})
		if e != nil {
			s.invalidateClusterValidation("")
			return nil, e
		}
		keep := map[string]bool{}
		for _, source := range chosen {
			keep[source.URI] = true
		}
		filtered := request.Documents[:0]
		for _, input := range request.Documents {
			if keep[input.URI] {
				filtered = append(filtered, input)
			}
		}
		request.Documents = filtered
	}

	if err = s.validateClusterSnapshot(request); err != nil {
		return nil, err
	}
	s.postRenderMu.Lock()
	if s.postRenderRunning {
		s.postRenderMu.Unlock()
		return postRenderResult{Message: "OpenChoreo validation is already running."}, nil
	}
	s.postRenderRunning = true
	s.postRenderMu.Unlock()
	defer func() { s.postRenderMu.Lock(); s.postRenderRunning = false; s.postRenderMu.Unlock() }()
	s.clusterValidation.mu.Lock()
	epoch := s.clusterValidation.epoch
	s.clusterValidation.inputs = map[string]string{}
	for _, d := range request.Documents {
		s.clusterValidation.inputs[d.URI] = d.Text
	}
	s.clusterValidation.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), postRenderTimeout)
	defer cancel()
	var sources []post.SourceDocument
	var resources []pre.Resource
	var findings []post.Finding
	components := 0
	for _, d := range request.Documents {
		docs, parseErr := parser.ParseYAML([]byte(d.Text))
		if parseErr != nil {
			findings = append(findings, post.Finding{Path: d.URI, Severity: valueError, Code: "yaml-parse-error", Message: parseErr.Error(), Line: 1, Column: 1})
			continue
		}
		doc := docs[0]
		if !strings.HasPrefix(doc.Kind, "Cluster") && doc.Metadata.Namespace == "" {
			setSnapshotNamespace(doc, request.Namespace)
		}
		doc.ValidationContext = targetschema.WithResolver(doc.ValidationContext, targetschema.Default())
		for _, diag := range validateDocs(docs) {
			severity := valueError
			if diag.Severity != nil {
				switch *diag.Severity {
				case protocol.DiagnosticSeverityWarning:
					severity = "warning"
				case protocol.DiagnosticSeverityInformation:
					severity = "info"
				case protocol.DiagnosticSeverityHint:
					severity = "hint"
				}
			}
			code := ""
			if diag.Code != nil {
				code = fmt.Sprint(diag.Code.Value)
			}
			findings = append(findings, post.Finding{Path: d.URI, Severity: severity, Code: code, Message: diag.Message, Line: int(diag.Range.Start.Line) + 1, Column: int(diag.Range.Start.Character) + 1})
		}
		resources = append(resources, pre.Resource{Path: d.URI, Document: doc})
		sources = append(sources, post.SourceDocument{Path: d.URI, Document: doc})
		if doc.Kind == "Component" {
			components++
		}
	}
	for _, f := range pre.ValidateDocuments(ctx, resources) {
		findings = append(findings, post.Finding{Path: f.Path, Severity: string(f.Severity), Code: f.Code, Message: f.Message, Line: f.Line, Column: f.Column})
	}
	findings = uniqueManualFindings(append(findings, post.ValidateWorkspace(ctx, sources)...))
	if ctx.Err() != nil {
		return nil, fmt.Errorf("cluster validation timed out; no results were published")
	}
	if !s.clusterValidationContextCurrent(request) {
		return postRenderResult{Message: "Cluster context changed during validation; run it again."}, nil
	}
	// An editor can change between initial validation and registering the input
	// set. Recheck versions as well as the invalidation epoch before publishing.
	for _, input := range request.Documents {
		if open := s.GetDocument(input.URI); open != nil && !sameResourceContent(input.URI, input.Text, open.Text) {
			return postRenderResult{Message: "Explorer documents changed during validation; run it again."}, nil
		}
	}
	s.clusterValidation.mu.Lock()
	if s.clusterValidation.epoch != epoch {
		s.clusterValidation.mu.Unlock()
		return postRenderResult{Message: "Resources changed during validation; run it again."}, nil
	}
	old := s.clusterValidation.byURI
	byURI := map[string][]protocol.Diagnostic{}
	result := postRenderResult{Files: len(request.Documents), Root: "openchoreo://" + request.ContextID + "/namespaces/" + request.Namespace}
	for _, f := range findings {
		text := ""
		for _, input := range request.Documents {
			if input.URI == f.Path {
				text = input.Text
				break
			}
		}
		diag := postRenderDiagnosticForText(f, text)
		if open := s.GetDocument(f.Path); open != nil && open.Text != text {
			diag = rebaseValidationDiagnostics(text, open.Text, []protocol.Diagnostic{diag})[0]
		}
		byURI[f.Path] = append(byURI[f.Path], diag)
		if f.Severity == valueError {
			result.Errors++
		} else if f.Severity == "warning" {
			result.Warnings++
		}
		result.Details = append(result.Details, fmt.Sprintf("%s:%d:%d: %s: %s", f.Path, f.Line, f.Column, f.Severity, f.Message))
	}
	s.clusterValidation.byURI = byURI
	s.clusterValidation.mu.Unlock()
	affected := map[string]bool{}
	for uri := range old {
		affected[uri] = true
	}
	for uri := range byURI {
		affected[uri] = true
	}
	for uri := range affected {
		s.republishCurrentDiagnostics(uri)
	}
	if request.ProjectName != "" {
		result.Root += "/projects/" + request.ProjectName
		for _, input := range request.Documents {
			result.ValidatedURIs = append(result.ValidatedURIs, input.URI)
		}
	}
	if components == 0 {
		result.Message = "No renderable components were found in the selected validation scope."
	}
	return result, nil
}

func setSnapshotNamespace(doc *parser.DocumentNode, namespace string) {
	doc.Metadata.Namespace = namespace
	root := doc.Root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "metadata" {
			continue
		}
		meta := root.Content[i+1]
		for j := 0; j+1 < len(meta.Content); j += 2 {
			if meta.Content[j].Value == "namespace" {
				meta.Content[j+1].Value = namespace
				return
			}
		}
		meta.Content = append(meta.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "namespace"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: namespace})
	}
}

// Match the provider's resource-type segments, including its RBAC aliases.
func clusterResourceURIType(kind string) string {
	aliases := map[string]string{"AuthzRole": "namespace-role", "AuthzRoleBinding": "namespace-role-binding", "ClusterAuthzRole": "cluster-role", "ClusterAuthzRoleBinding": "cluster-role-binding"}
	if alias, ok := aliases[kind]; ok {
		return alias
	}
	var result strings.Builder
	for i, r := range kind {
		if i > 0 && unicode.IsUpper(r) {
			result.WriteByte('-')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}
