// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/openchoreo/openchoreo/internal/pathutil"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	prerendervalidator "github.com/openchoreo/openchoreo/tools/lint/pre-render-validator"
	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
)

const preRenderPreRenderSchemaValidationSource = "openchoreo-schema"

// runPreRenderSchemaValidationFiles validates Component/trait parameters against schemas
// for the given files and publishes diagnostics.
func (s *Server) runPreRenderSchemaValidationFiles(files []string) {
	s.preRenderRunMu.Lock()
	defer s.preRenderRunMu.Unlock()
	revision := s.indexer.Version()
	overlays := make(map[string]string)
	for _, doc := range s.snapshotOpenDocuments() {
		overlays[pathutil.PathKey(fileURIToPath(doc.URI))] = doc.Text
	}
	indexed := map[string][]*parser.DocumentNode{}
	if len(files) > 0 {
		for _, entry := range s.indexer.Snapshot(pathToDocumentURI(files[0])) {
			p := fileURIToPath(entry.URI)
			indexed[pathutil.PathKey(p)] = append(indexed[pathutil.PathKey(p)], entry.Document)
		}
	}
	var resources []prerendervalidator.Resource
	seen := make(map[string]string)
	for _, path := range files {
		path = pathutil.Absolute(path)
		if _, ok := seen[pathutil.PathKey(path)]; ok {
			continue
		}
		seen[pathutil.PathKey(path)] = path
		if _, open := overlays[pathutil.PathKey(path)]; !open && len(indexed[pathutil.PathKey(path)]) > 0 {
			for _, doc := range indexed[pathutil.PathKey(path)] {
				resources = append(resources, prerendervalidator.Resource{Path: path, Document: doc})
			}
			continue
		}
		text, open := overlays[pathutil.PathKey(path)]
		if !open {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			text = string(data)
		}
		docs, err := parser.ParseYAML([]byte(text))
		if err != nil {
			continue
		}
		for _, doc := range docs {
			resources = append(resources, prerendervalidator.Resource{Path: path, Document: doc})
		}
	}
	if len(files) > 0 {
		seenDocs := map[string]bool{}
		for _, r := range resources {
			seenDocs[fmt.Sprintf("%s:%d", pathutil.PathKey(r.Path), r.Document.DocumentIndex)] = true
		}
		for _, entry := range s.indexer.Snapshot(pathToDocumentURI(files[0])) {
			p := fileURIToPath(entry.URI)
			if _, open := overlays[pathutil.PathKey(p)]; open {
				continue
			}
			key := fmt.Sprintf("%s:%d", pathutil.PathKey(p), entry.Document.DocumentIndex)
			if !seenDocs[key] {
				resources = append(resources, prerendervalidator.Resource{Path: p, Document: entry.Document})
			}
		}
	}
	// Validate unopened files through the same cached static entrypoint used by editors.
	static := make(map[string][]protocol.Diagnostic)
	rawSources := map[string][]byte{}
	if len(files) > 0 {
		rawSources = s.indexer.SchemaSources(pathToDocumentURI(files[0]))
	}
	for _, path := range seen {
		uri := pathToDocumentURI(path)
		s.rememberFileURI(uri)
		text, open := overlays[pathutil.PathKey(path)]
		if !open {
			data, ok := rawSources[path]
			var err error
			if !ok {
				data, err = os.ReadFile(path)
			}
			if err != nil {
				s.deleteValidationEntry(uri)
				static[pathutil.DocumentKey(uri)] = nil
				continue
			}
			text = string(data)
		}
		static[pathutil.DocumentKey(uri)] = s.diagnosticsForCached(uri, text)
	}
	findings := prerendervalidator.ValidateDocuments(context.Background(), resources)
	if s.indexer.Version() != revision {
		return
	}
	s.publishPreRenderFindings(files, static, findings)
}

func (s *Server) publishPreRenderFindings(files []string, static map[string][]protocol.Diagnostic, findings []prerendervalidator.Finding) {
	s.preRenderSchemaMu.Lock()
	if s.workspaceStaticByURI == nil {
		s.workspaceStaticByURI = make(map[string][]protocol.Diagnostic)
	}
	for uri, diags := range static {
		s.workspaceStaticByURI[pathutil.DocumentKey(uri)] = diags
	}
	s.preRenderSchemaMu.Unlock()
	if len(findings) == 0 {
		s.clearSchemaFindings(files)
		return
	}

	byURI := make(map[string][]protocol.Diagnostic)
	for _, finding := range findings {
		uri := pathToDocumentURI(finding.Path)
		s.rememberFileURI(uri)
		byURI[pathutil.DocumentKey(uri)] = append(byURI[pathutil.DocumentKey(uri)], schemaFindingToDiagnostic(finding))
	}

	s.preRenderSchemaMu.Lock()
	affected := make(map[string]struct{})
	if s.preRenderSchemaByURI == nil {
		s.preRenderSchemaByURI = make(map[string][]protocol.Diagnostic)
	}
	if s.preRenderSchemaFiles == nil {
		s.preRenderSchemaFiles = make(map[string]struct{})
	}
	for _, f := range files {
		uri := pathToDocumentURI(f)
		s.rememberFileURI(uri)
		if _, hadSchema := s.preRenderSchemaByURI[pathutil.DocumentKey(uri)]; hadSchema || s.GetDocument(uri) == nil {
			affected[pathutil.DocumentKey(uri)] = struct{}{}
		}
		delete(s.preRenderSchemaByURI, pathutil.DocumentKey(uri))
		s.preRenderSchemaFiles[pathutil.PathKey(f)] = struct{}{}
	}
	for uri, diags := range byURI {
		s.preRenderSchemaByURI[pathutil.DocumentKey(uri)] = diags
		affected[pathutil.DocumentKey(uri)] = struct{}{}
	}
	s.preRenderSchemaMu.Unlock()

	open := make(map[string]Document)
	for _, doc := range s.snapshotOpenDocuments() {
		open[pathutil.DocumentKey(doc.URI)] = doc
	}
	for uri := range affected {
		if doc, ok := open[pathutil.DocumentKey(uri)]; ok {
			structural := s.diagnosticsForCached(uri, doc.Text)
			s.publishDiagnosticsAsync(uri, doc.Version, structural)
		} else {
			s.publishDiagnosticsAsync(uri, 0, nil)
		}
	}
}

func (s *Server) clearSchemaFindings(files []string) {
	if len(files) == 0 {
		return
	}
	s.preRenderSchemaMu.Lock()
	affected := make(map[string]struct{})
	for _, f := range files {
		uri := pathToDocumentURI(f)
		s.rememberFileURI(uri)
		if _, hadSchema := s.preRenderSchemaByURI[pathutil.DocumentKey(uri)]; hadSchema || s.GetDocument(uri) == nil {
			affected[pathutil.DocumentKey(uri)] = struct{}{}
		}
		delete(s.preRenderSchemaByURI, pathutil.DocumentKey(uri))
		delete(s.preRenderSchemaFiles, pathutil.PathKey(f))
	}
	s.preRenderSchemaMu.Unlock()

	open := make(map[string]Document)
	for _, doc := range s.snapshotOpenDocuments() {
		open[pathutil.DocumentKey(doc.URI)] = doc
	}
	for uri := range affected {
		if doc, ok := open[pathutil.DocumentKey(uri)]; ok {
			structural := s.diagnosticsForCached(uri, doc.Text)
			s.publishDiagnosticsAsync(uri, doc.Version, structural)
		} else {
			s.publishDiagnosticsAsync(uri, 0, nil)
		}
	}
}

func schemaFindingToDiagnostic(f prerendervalidator.Finding) protocol.Diagnostic {
	line := f.Line
	column := f.Column
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	start := protocol.Position{
		Line:      lsputil.PositionIndex(line - 1),
		Character: lsputil.PositionIndex(column - 1),
	}
	end := protocol.Position{
		Line:      start.Line,
		Character: min(start.Character+1, math.MaxInt32),
	}
	severity := protocol.DiagnosticSeverityError
	switch f.Severity {
	case prerendervalidator.SeverityWarning:
		severity = protocol.DiagnosticSeverityWarning
	case prerendervalidator.SeverityInfo:
		severity = protocol.DiagnosticSeverityInformation
	case prerendervalidator.SeverityHint:
		severity = protocol.DiagnosticSeverityHint
	}
	return protocol.Diagnostic{
		Range:    protocol.Range{Start: start, End: end},
		Severity: &severity,
		Code:     &protocol.IntegerOrString{Value: f.Code},
		Source:   stringPtr(preRenderPreRenderSchemaValidationSource),
		Message:  f.Message,
	}
}

func (s *Server) triggerPreRenderSchemaValidationForPath(path string) {
	dir := filepath.Dir(path)
	if len(s.workspaceRootsSnapshot()) > 0 {
		contained := false
		for _, root := range s.workspaceRootsSnapshot() {
			if projectContains(root, path) {
				contained = true
				break
			}
		}
		if !contained {
			if s.isSchemaDependency(path) {
				for _, root := range s.workspaceRootsSnapshot() {
					s.triggerPreRenderSchemaValidationForPath(filepath.Join(root, ".workspace-diagnostics.yaml"))
				}
			}
			return
		}
	}
	for _, root := range s.workspaceRootsSnapshot() {
		if projectContains(root, path) {
			dir = root
			break
		}
	}

	// Use the containing workspace so sibling platform definitions are available.
	files, err := parser.CollectYAMLFiles(dir, true)
	if err != nil {
		return
	}
	if _, err := os.Stat(path); err == nil {
		// ensure path included
		found := false
		for _, f := range files {
			if pathutil.SamePath(f, path) {
				found = true
				break
			}
		}
		if !found {
			files = append(files, path)
		}
	}
	for _, doc := range s.snapshotOpenDocuments() {
		p := fileURIToPath(doc.URI)
		if pathutil.Within(dir, p) {
			files = append(files, p)
		}
	}
	// Include the changed path even after deletion so cached findings clear.
	files = append(files, path)
	s.preRenderSchemaMu.Lock()
	for prior := range s.preRenderSchemaFiles {
		if pathutil.Within(dir, prior) {
			files = append(files, prior)
		}
	}
	s.preRenderSchemaMu.Unlock()
	clean := files[:0]
	for _, file := range files {
		if filepath.Base(file) != ".workspace-diagnostics.yaml" {
			clean = append(clean, file)
		}
	}
	s.runPreRenderSchemaValidationFiles(clean)
}

func (s *Server) mergePreRenderSchemaDiagnostics(uri string, diags []protocol.Diagnostic) []protocol.Diagnostic {
	s.preRenderSchemaMu.Lock()
	cached := append([]protocol.Diagnostic(nil), s.preRenderSchemaByURI[pathutil.DocumentKey(uri)]...)
	s.preRenderSchemaMu.Unlock()
	if len(cached) == 0 {
		return diags
	}
	merged := make([]protocol.Diagnostic, 0, len(diags)+len(cached))
	merged = append(merged, diags...)
	for _, finding := range cached {
		duplicate := false
		if finding.Code != nil && finding.Code.Value == "pre-render-validation-failed" {
			for _, prior := range diags {
				if prior.Source != nil && *prior.Source == "openchoreo-post-render" && strings.Contains(prior.Message, finding.Message) {
					duplicate = true
					break
				}
			}
		}
		if !duplicate {
			merged = append(merged, finding)
		}
	}
	return merged
}
