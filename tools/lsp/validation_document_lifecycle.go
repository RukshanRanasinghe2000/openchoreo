// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"net/url"
	"reflect"
	"strings"

	"github.com/openchoreo/openchoreo/internal/pathutil"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	post "github.com/openchoreo/openchoreo/tools/lint/post-render-validator"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// Virtual documents are resource identities, never filesystem paths.
func localDocumentPath(uri string) string { return pathutil.NativePath(uri) }

func normalizedResource(uri, text string) (any, bool) {
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil || len(docs) != 1 {
		return nil, false
	}
	var resource map[string]any
	if err = docs[0].Root.Decode(&resource); err != nil || resource == nil {
		return nil, false
	}
	if u, e := url.Parse(uri); e == nil && u.Scheme == "openchoreo" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == "namespaces" {
			if meta, ok := resource["metadata"].(map[string]any); ok && (meta["namespace"] == nil || meta["namespace"] == "") {
				meta["namespace"] = parts[1]
			}
		}
	}
	return resource, true
}
func sameResourceContent(uri, a, b string) bool {
	if a == b {
		return true
	}
	left, ok := normalizedResource(uri, a)
	if !ok {
		return false
	}
	right, ok := normalizedResource(uri, b)
	return ok && reflect.DeepEqual(left, right)
}

// Opening a provider document may only reformat the snapshot used by Play.
// Retain its findings and move their ranges onto the newly loaded YAML nodes.
func (s *Server) reconcileClusterValidation(uri, text string) {
	s.clusterValidation.mu.Lock()
	previous, known := s.clusterValidation.inputs[uri]
	epoch := s.clusterValidation.epoch
	diags := append([]protocol.Diagnostic(nil), s.clusterValidation.byURI[uri]...)
	s.clusterValidation.mu.Unlock()
	if !known {
		return
	}
	if !sameResourceContent(uri, previous, text) {
		s.clusterValidation.mu.Lock()
		if s.clusterValidation.epoch != epoch || s.clusterValidation.inputs[uri] != previous {
			s.clusterValidation.mu.Unlock()
			return
		}
		s.clusterValidation.epoch++
		affected := keysOfDiagnosticMap(s.clusterValidation.byURI)
		s.clusterValidation.byURI = nil
		s.clusterValidation.inputs = nil
		s.clusterValidation.mu.Unlock()
		s.republishStructural(affected, "")
		return
	}
	if previous == text {
		return
	}
	diags = rebaseValidationDiagnostics(previous, text, diags)
	s.clusterValidation.mu.Lock()
	defer s.clusterValidation.mu.Unlock()
	if s.clusterValidation.epoch != epoch || s.clusterValidation.inputs[uri] != previous {
		return
	}
	s.clusterValidation.inputs[uri] = text
	if len(diags) > 0 {
		s.clusterValidation.byURI[uri] = diags
	}
}

func yamlMappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// Pair nodes by YAML key or sequence index, rather than line or map order.
func rebaseValidationDiagnostics(oldText, newText string, diags []protocol.Diagnostic) []protocol.Diagnostic {
	oldDocs, e := parser.ParseYAML([]byte(oldText))
	if e != nil || len(oldDocs) != 1 {
		return diags
	}
	newDocs, e := parser.ParseYAML([]byte(newText))
	if e != nil || len(newDocs) != 1 {
		return diags
	}
	for i := range diags {
		start := diags[i].Range.Start
		found := false
		var pair func(*yaml.Node, *yaml.Node)
		pair = func(old, new *yaml.Node) {
			if found || old == nil || new == nil || old.Kind != new.Kind {
				return
			}
			if old.Kind == yaml.ScalarNode {
				previous, valid := referenceValueRange(oldText, old)
				if valid && previous.Start.Line == start.Line && start.Character >= previous.Start.Character && start.Character <= previous.End.Character {
					if next, valid := referenceValueRange(newText, new); valid {
						diags[i].Range = next
						found = true
					}
				}
				return
			}
			if old.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(old.Content); j += 2 {
					for k := 0; k+1 < len(new.Content); k += 2 {
						if old.Content[j].Value == new.Content[k].Value {
							pair(old.Content[j], new.Content[k])
							pair(old.Content[j+1], new.Content[k+1])
							break
						}
					}
				}
				return
			}
			for j := 0; j < len(old.Content) && j < len(new.Content); j++ {
				pair(old.Content[j], new.Content[j])
			}
		}
		pair(oldDocs[0].Root, newDocs[0].Root)
	}
	return diags
}

func postRenderDiagnosticForText(f post.Finding, text string) protocol.Diagnostic {
	diag := postRenderDiagnostic(f)
	if !strings.Contains(f.Message, "no Workload for Component") {
		return diag
	}
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil {
		return diag
	}
	for _, doc := range docs {
		if doc.Kind != "Component" {
			continue
		}
		name := yamlMappingValue(yamlMappingValue(doc.Root, "metadata"), "name")
		if name == nil || name.Line != f.Line || name.Column != f.Column {
			continue
		}
		if rng, valid := referenceValueRange(text, name); valid {
			diag.Range = rng
		}
	}
	return diag
}
