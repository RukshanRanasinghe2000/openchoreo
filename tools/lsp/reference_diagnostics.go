// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/openchoreo/openchoreo/tools/lint/indexer"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

const missingReferenceCode = "unresolved-resource-reference"

type resourceReference struct {
	kind, name     string
	rng            protocol.Range
	candidates     []string
	missing        bool
	overrideLegacy bool
}

// referenceChecks shares navigation's scoped resolver without inserting remote
// resources into the filesystem index. Missing remote targets are only errors
// after a successful, complete list of the corresponding kind.
func (s *Server) referenceChecks(uri, text string) []resourceReference {
	u, err := url.Parse(uri)
	if err != nil || (u.Scheme != "file" && u.Scheme != "openchoreo") || !parser.HasOpenChoreoAPIVersion([]byte(text)) {
		return nil
	}
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil {
		return nil
	}
	s.locations.mu.RLock()
	snapshot := s.locations.snapshot
	locationsStarted := s.locations.received
	s.locations.mu.RUnlock()
	s.cluster.mu.RLock()
	clusterStarted := s.cluster.names != nil || s.cluster.schemas != nil
	s.cluster.mu.RUnlock()
	if u.Scheme == "openchoreo" && (snapshot.ContextID == nil || *snapshot.ContextID != u.Host) {
		return nil
	}
	var local []indexer.SnapshotDocument
	for _, d := range s.indexer.DefinitionSnapshot(uri) {
		if strings.HasPrefix(d.URI, "file:") {
			local = append(local, d)
		}
	}
	var out []resourceReference
	for _, doc := range docs {
		if !parser.IsOpenChoreoAPIVersion(doc.APIVersion) {
			continue
		}
		namespace := indexer.NamespaceOrDefault(doc.Metadata.Namespace)
		if u.Scheme == "openchoreo" {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 4 && parts[0] == "namespaces" {
				if doc.Metadata.Namespace != "" && doc.Metadata.Namespace != parts[1] {
					continue
				}
				namespace = parts[1]
			}
		}
		idx := &definitionIndex{server: s, source: uri, namespace: namespace, component: scalarAt(doc.Spec, "owner", "componentName"), documents: local, snapshot: snapshot, ranges: map[string]protocol.Range{}}
		if idx.component == "" {
			idx.component = scalarAt(doc.Spec, "componentName")
		}
		kinds := idx.FindAllKinds()
		sort.Slice(kinds, func(i, j int) bool {
			if len(kinds[i]) != len(kinds[j]) {
				return len(kinds[i]) > len(kinds[j])
			}
			return kinds[i] < kinds[j]
		})
		var walk func(*yaml.Node, string)
		walk = func(n *yaml.Node, field string) {
			if n == nil {
				return
			}
			if n.Kind == yaml.SequenceNode {
				for _, child := range n.Content {
					walk(child, field)
				}
				return
			}
			if n.Kind != yaml.MappingNode {
				return
			}
			for i := 0; i+1 < len(n.Content); i += 2 {
				key, v := n.Content[i].Value, n.Content[i+1]
				switch key {
				case "template", "parameters", "environmentConfigs", "metadata":
					continue
				}
				if (key == "name" || key == "releaseName" || key == "resourceRelease" || key == "projectRelease") && v.Kind == yaml.ScalarNode && v.Tag == "!!str" && v.Value != "" && !strings.Contains(v.Value, "${") && !strings.ContainsAny(v.Value, "\n\r") && v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
					kind := map[string]string{"releaseName": "ComponentRelease", "resourceRelease": "ResourceRelease", "projectRelease": "ProjectRelease"}[key]
					if sibling := lsputil.FindSiblingKey(n, "kind"); sibling != nil {
						kind = sibling.Value
					}
					if kind == "" {
						kind = lsputil.FindKindByFieldName(field, kinds)
					}
					known := false
					for _, k := range kinds {
						if kind == k {
							known = true
							break
						}
					}
					if known {
						rng, valid := referenceValueRange(text, v)
						if !valid {
							continue
						}
						r := resourceReference{kind: kind, name: v.Value, rng: rng}
						ready := !clusterStarted && !locationsStarted && snapshot.ContextID == nil && u.Scheme == "file" && s.indexer.HasWorkspace(uri)
						if ready && s.initialIndexStarted {
							select {
							case <-s.indexDone:
							default:
								ready = false
							}
						}
						for _, k := range snapshot.CompleteKinds {
							if k == kind && snapshot.ContextID != nil && (strings.HasPrefix(kind, "Cluster") || snapshot.Namespace != nil && *snapshot.Namespace == namespace) {
								ready = true
							}
						}
						r.overrideLegacy = ready || clusterStarted || locationsStarted
						r.missing = ready && len(idx.FindScopedDefinition(kind, namespace, v.Value, uri)) == 0
						seen := map[string]bool{}
						add := func(name, workload string) {
							if strings.Contains(kind, "ComponentType") && workload != "" {
								name = workload + "/" + name
							}
							if !seen[name] {
								seen[name] = true
								r.candidates = append(r.candidates, name)
							}
						}
						for _, d := range local {
							if d.Document.Kind != kind || (!strings.HasPrefix(kind, "Cluster") && indexer.NamespaceOrDefault(d.Document.Metadata.Namespace) != namespace) {
								continue
							}
							if kind == "ComponentRelease" && idx.component != "" && scalarAt(d.Document.Spec, "owner", "componentName") != idx.component {
								continue
							}
							add(d.Document.Metadata.Name, scalarAt(d.Document.Spec, "workloadType"))
						}
						if snapshot.ContextID != nil {
							for _, target := range snapshot.Resources {
								if target.Kind != kind || target.Namespace != "" && target.Namespace != namespace || kind == "ComponentRelease" && idx.component != "" && target.ComponentName != idx.component {
									continue
								}
								add(target.Name, target.WorkloadType)
							}
						}
						out = append(out, r)
					}
				}
				walk(v, key)
			}
		}
		walk(doc.Spec, "spec")
	}
	return out
}

func referenceValueRange(text string, n *yaml.Node) (protocol.Range, bool) {
	lines := strings.Split(text, "\n")
	line := max(n.Line-1, 0)
	col := max(n.Column-1, 0)
	// yaml.v3 columns count Unicode code points, LSP columns count UTF-16 units.
	offset := 0
	if line < len(lines) {
		runes := []rune(lines[line])
		col = min(col, len(runes))
		offset = len(utf16.Encode(runes[:col]))
	}
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
		offset++
	}
	width := len(utf16.Encode([]rune(n.Value)))
	// Preserve escaped spellings inside quoted values by covering the original token.
	if line < len(lines) && n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
		runes := []rune(lines[line])
		start := col + 1
		quote := runes[col]
		closed := false
		for j := start; j < len(runes); j++ {
			if quote == '"' && runes[j] == '\\' {
				j++
				continue
			}
			if runes[j] == quote {
				if quote == '\'' && j+1 < len(runes) && runes[j+1] == quote {
					j++
					continue
				}
				width = len(utf16.Encode(runes[start:j]))
				closed = true
				break
			}
		}
		if !closed {
			return protocol.Range{}, false
		}
	}
	return protocol.Range{Start: protocol.Position{Line: uint32(line), Character: uint32(offset)}, End: protocol.Position{Line: uint32(line), Character: uint32(offset + width)}}, true
}

// Legacy ranges may include an opening quote; match only this scalar, not
// unrelated names sharing a line in a flow mapping.
func replacesLegacyReference(d protocol.Diagnostic, r resourceReference) bool {
	return r.overrideLegacy && d.Range.Start.Line == r.rng.Start.Line && d.Range.Start.Character <= r.rng.Start.Character && r.rng.Start.Character-d.Range.Start.Character <= 1
}

func referenceDiagnostic(r resourceReference) protocol.Diagnostic {
	severity := protocol.DiagnosticSeverityError
	return protocol.Diagnostic{Range: r.rng, Severity: &severity, Code: &protocol.IntegerOrString{Value: missingReferenceCode}, Source: stringPtr("openchoreo"), Message: fmt.Sprintf("%s '%s' was not found.", r.kind, r.name)}
}

func (s *Server) diagnosticsForCached(uri, text string) []protocol.Diagnostic {
	base := s.staticDiagnosticsForCached(uri, text)
	refs := s.referenceChecks(uri, text)
	out := make([]protocol.Diagnostic, 0, len(base)+len(refs))
	for _, d := range base {
		legacy := false
		if d.Code != nil && d.Code.Value == "unknown-resource-file" {
			for _, r := range refs {
				if replacesLegacyReference(d, r) {
					legacy = true
					break
				}
			}
		}
		if !legacy {
			out = append(out, d)
		}
	}
	for _, r := range refs {
		if r.missing {
			out = append(out, referenceDiagnostic(r))
		}
	}
	return out
}

type referenceMatch struct {
	name   string
	score  float64
	prefix bool
}

func referenceMatches(current string, candidates []string) []referenceMatch {
	var matches []referenceMatch
	a := []rune(strings.ToLower(current))
	prefix, _, hasPrefix := strings.Cut(strings.ToLower(current), "/")
	for _, name := range candidates {
		b := []rune(strings.ToLower(name))
		row := make([]int, len(b)+1)
		for j := range row {
			row[j] = j
		}
		for i, x := range a {
			previous := row[0]
			row[0] = i + 1
			for j, y := range b {
				old := row[j+1]
				cost := 0
				if x != y {
					cost = 1
				}
				row[j+1] = min(row[j+1]+1, row[j]+1, previous+cost)
				previous = old
			}
		}
		score := 1 - float64(row[len(b)])/float64(max(len(a), len(b), 1))
		if score >= 0.6 {
			p, _, _ := strings.Cut(strings.ToLower(name), "/")
			matches = append(matches, referenceMatch{name, score, hasPrefix && p == prefix})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].prefix != matches[j].prefix {
			return matches[i].prefix
		}
		return matches[i].name < matches[j].name
	})
	return matches[:min(3, len(matches))]
}

func (s *Server) referenceActions(uri, text string, version int, _ protocol.Range) []protocol.CodeAction {
	var actions []protocol.CodeAction
	for _, r := range s.referenceChecks(uri, text) {
		if !r.missing {
			continue
		}
		matches := referenceMatches(r.name, r.candidates)
		for i, m := range matches {
			kind := protocol.CodeActionKindQuickFix
			preferred := i == 0 && (len(matches) == 1 || m.score > matches[1].score)
			v := protocol.Integer(version)
			replacement := m.name
			// Names are resource identifiers; keep existing quotes and escape their content.
			line := strings.Split(text, "\n")[r.rng.Start.Line]
			before := utf16.Decode(utf16.Encode([]rune(line))[:r.rng.Start.Character])
			if len(before) > 0 {
				switch before[len(before)-1] {
				case '\'':
					replacement = strings.ReplaceAll(replacement, "'", "''")
				case '"':
					replacement = strings.ReplaceAll(strings.ReplaceAll(replacement, "\\", "\\\\"), "\"", "\\\"")
				}
			}
			actions = append(actions, protocol.CodeAction{Title: "Change to " + m.name, Kind: &kind, IsPreferred: &preferred, Diagnostics: []protocol.Diagnostic{referenceDiagnostic(r)}, Edit: &protocol.WorkspaceEdit{DocumentChanges: []any{protocol.TextDocumentEdit{TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri}, Version: &v}, Edits: []any{protocol.TextEdit{Range: r.rng, NewText: replacement}}}}}})
		}
	}
	return actions
}
