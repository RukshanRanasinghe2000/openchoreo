// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"

	"github.com/openchoreo/openchoreo/tools/lint/indexer"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	codedefinition "github.com/openchoreo/openchoreo/tools/lsp/code-definition"
)

const resourceLocationsMethod = "openchoreo/updateResourceLocations"

type clusterLocation struct {
	Kind          string          `json:"kind"`
	Name          string          `json:"name"`
	Namespace     string          `json:"namespace,omitempty"`
	ComponentName string          `json:"componentName,omitempty"`
	WorkloadType  string          `json:"workloadType,omitempty"`
	URI           string          `json:"uri"`
	Range         *protocol.Range `json:"range,omitempty"`
}
type locationSnapshot struct {
	ContextID     *string           `json:"contextId"`
	Namespace     *string           `json:"namespace"`
	Generation    *uint64           `json:"generation"`
	CompleteKinds []string          `json:"completeKinds,omitempty"`
	Resources     []clusterLocation `json:"resources"`
}
type clusterLocations struct {
	mu       sync.RWMutex
	snapshot locationSnapshot
	received bool
}

func (c *clusterLocations) update(data []byte) error {
	var next locationSnapshot
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	if next.Generation == nil || next.Resources == nil {
		return fmt.Errorf("locations require generation and resources")
	}
	if next.ContextID == nil {
		if len(next.Resources) > 0 {
			return fmt.Errorf("locations require context")
		}
	} else if *next.ContextID == "" {
		return fmt.Errorf("empty context")
	}
	for _, kind := range next.CompleteKinds {
		if kind == "" || next.ContextID == nil {
			return fmt.Errorf("invalid complete resource kind")
		}
	}
	if err := validateClusterLocations(next); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.Generation != nil && *next.Generation < *c.snapshot.Generation {
		return fmt.Errorf("stale resource locations")
	}
	c.snapshot = next
	c.received = true
	return nil
}

type definitionIndex struct {
	server    *Server
	source    string
	namespace string
	component string
	documents []indexer.SnapshotDocument
	snapshot  locationSnapshot
	ranges    map[string]protocol.Range
}

func (d *definitionIndex) HasWorkspace(string) bool { return true }
func (d *definitionIndex) FindAllKinds() []string {
	seen := map[string]bool{}
	for _, doc := range d.documents {
		seen[doc.Document.Kind] = true
	}
	for _, r := range d.snapshot.Resources {
		seen[r.Kind] = true
	}
	// Infer standard reference kinds even when their target is missing locally.
	for _, k := range []string{"ComponentType", "ClusterComponentType", "Trait", "ClusterTrait", "Workflow", "ClusterWorkflow", kindProject, kindComponent, "ResourceType", "ClusterResourceType", "ProjectType", "ClusterProjectType", kindComponentRelease, "ResourceRelease", "ProjectRelease"} {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func (d *definitionIndex) FindDefinitionByKind(kind string) []indexer.Location {
	return d.FindScopedDefinition(kind, d.namespace, "", d.source)
}
func (d *definitionIndex) FindDefinition(kind, name string) []indexer.Location {
	return d.FindScopedDefinition(kind, d.namespace, name, d.source)
}
func (d *definitionIndex) FindByName(name string) []indexer.Location {
	// Without a known kind, do not guess among cluster resources.
	var out []indexer.Location
	for _, doc := range d.documents {
		if doc.Document.Metadata.Name == name {
			out = append(out, d.localLocation(doc))
		}
	}
	return out
}
func (d *definitionIndex) localLocation(doc indexer.SnapshotDocument) indexer.Location {
	line, col := 1, 1
	if doc.Document.Metadata.Node != nil {
		line, col = doc.Document.Metadata.Node.Line, doc.Document.Metadata.Node.Column
	}
	return indexer.Location{URI: doc.URI, Line: line, Column: col}
}
func scalarAt(n *yaml.Node, path ...string) string {
	for _, key := range path {
		var child *yaml.Node
		if n != nil {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == key {
					child = n.Content[i+1]
					break
				}
			}
		}
		n = child
	}
	if n != nil {
		return n.Value
	}
	return ""
}
func (d *definitionIndex) FindScopedDefinition(kind, namespace, name, _ string) []indexer.Location {
	if namespace == "" {
		namespace = d.namespace
	}
	namespace = indexer.NamespaceOrDefault(namespace)
	d.server.logger.Debug("definition reference", "kind", kind, "name", name, "namespace", namespace, "component", d.component)
	normalized := indexer.ReferenceName(kind, name)
	local := d.localDefinitions(kind, namespace, name, normalized)
	if len(local) > 0 {
		return local
	}
	return d.remoteDefinitions(kind, namespace, name, normalized)
}

func (s *Server) definitionLocations(_ *glsp.Context, p *protocol.DefinitionParams) (any, error) {
	uri := p.TextDocument.URI
	u, err := url.Parse(uri)
	if err != nil || (u.Scheme != uriSchemeFile && u.Scheme != uriSchemeOpenChoreo) {
		return nil, nil
	}
	doc := s.GetDocument(uri)
	if doc == nil {
		return nil, nil
	}
	docs, err := parser.ParseYAML([]byte(doc.Text))
	if err != nil {
		return nil, nil
	}
	source := parser.DocumentForPosition(docs, int(p.Position.Line)+1, int(p.Position.Character)+1)
	if source == nil {
		return nil, nil
	}
	namespace := indexer.NamespaceOrDefault(source.Metadata.Namespace)
	if u.Scheme == uriSchemeOpenChoreo {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == uriNamespaces {
			if source.Metadata.Namespace != "" && source.Metadata.Namespace != parts[1] {
				return nil, nil
			}
			namespace = parts[1]
		}
	}
	s.locations.mu.RLock()
	snapshot := s.locations.snapshot
	s.locations.mu.RUnlock()
	if u.Scheme == uriSchemeOpenChoreo && (snapshot.ContextID == nil || u.Host != *snapshot.ContextID) {
		return nil, nil
	}
	candidates := s.indexer.DefinitionSnapshot(uri)
	local := make([]indexer.SnapshotDocument, 0, len(candidates))
	for _, entry := range candidates {
		if target, e := url.Parse(entry.URI); e == nil && target.Scheme == uriSchemeFile {
			local = append(local, entry)
		}
	}
	idx := &definitionIndex{ranges: map[string]protocol.Range{}, server: s, source: uri, namespace: namespace, component: scalarAt(source.Spec, "owner", "componentName"), documents: local, snapshot: snapshot}
	if idx.component == "" {
		idx.component = scalarAt(source.Spec, "componentName")
	}
	s.logger.Debug("definition request", "uri", uri, "scheme", u.Scheme, "position", p.Position, "workspaceRoots", s.workspaceRootsSnapshot())
	// Namespace may come from a virtual URI when omitted from its YAML.
	text := doc.Text
	if source.Metadata.Namespace == "" && namespace != "default" {
		// Scoped adapter supplies the source namespace independently of YAML parsing.
		idx.namespace = namespace
	}
	result := codedefinition.Handle(text, int(p.Position.Line)+1, int(p.Position.Character)+1, uri, idx)
	for i := range result {
		if rng, ok := idx.ranges[result[i].URI]; ok {
			result[i].Range = rng
		}
	}
	s.logger.Debug("definition result", "uri", uri, "locations", result)
	return result, nil
}

func validateClusterLocations(next locationSnapshot) error {
	for _, r := range next.Resources {
		u, err := url.Parse(r.URI)
		if err != nil || u.Scheme != uriSchemeOpenChoreo || u.Host == "" || next.ContextID == nil || u.Host != *next.ContextID || u.User != nil || u.Fragment != "" || r.Kind == "" || r.Name == "" {
			return fmt.Errorf("invalid resource location identity")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if r.Namespace != "" {
			if len(parts) != 4 || parts[0] != uriNamespaces || parts[1] != r.Namespace {
				return fmt.Errorf("location namespace mismatch")
			}
		} else if len(parts) != 2 {
			return fmt.Errorf("invalid cluster location path")
		}
		if !strings.HasSuffix(u.Path, "/"+r.Name+".yaml") {
			return fmt.Errorf("location name mismatch")
		}
		if r.Range != nil {
			a, b := r.Range.Start, r.Range.End
			if a.Line > 2147483647 || b.Line > 2147483647 || a.Character > 2147483647 || b.Character > 2147483647 || b.Line < a.Line || (b.Line == a.Line && b.Character < a.Character) {
				return fmt.Errorf("invalid location range")
			}
		}
	}
	return nil
}

func (d *definitionIndex) localDefinitions(kind, namespace, name, normalized string) []indexer.Location {
	var local []indexer.Location
	for _, entry := range d.documents {
		doc := entry.Document
		if doc.Kind != kind || (name != "" && doc.Metadata.Name != normalized) || (!strings.HasPrefix(kind, "Cluster") && indexer.NamespaceOrDefault(doc.Metadata.Namespace) != namespace) {
			continue
		}
		if normalized != name && scalarAt(doc.Spec, "workloadType") != strings.Split(name, "/")[0] {
			continue
		}
		if d.component != "" && kind == kindComponentRelease && scalarAt(doc.Spec, "owner", "componentName") != d.component {
			continue
		}
		local = append(local, d.localLocation(entry))
	}
	return local
}

func (d *definitionIndex) remoteDefinitions(kind, namespace, name, normalized string) []indexer.Location {
	source, _ := url.Parse(d.source)
	if d.snapshot.ContextID == nil || (source.Scheme == uriSchemeOpenChoreo && source.Host != *d.snapshot.ContextID) {
		return nil
	}
	var remote []indexer.Location
	seen := map[string]bool{}
	for _, r := range d.snapshot.Resources {
		if r.Kind != kind || (name != "" && r.Name != normalized) || (r.Namespace != "" && r.Namespace != namespace) {
			continue
		}
		if normalized != name && r.WorkloadType != strings.Split(name, "/")[0] {
			continue
		}
		if d.component != "" && kind == kindComponentRelease && r.ComponentName != d.component {
			continue
		}
		if seen[r.URI] {
			continue
		}
		seen[r.URI] = true
		start := protocol.Position{}
		if r.Range != nil {
			start = r.Range.Start
		}
		if open := d.server.GetDocument(r.URI); open != nil {
			if docs, err := parser.ParseYAML([]byte(open.Text)); err == nil {
				for _, doc := range docs {
					if doc.Kind == kind && doc.Metadata.Name == r.Name && doc.Metadata.Node != nil {
						start = toLSPPosition(parser.Position{Line: doc.Metadata.Node.Line, Column: doc.Metadata.Node.Column})
					}
				}
			}
		}
		rng := protocol.Range{Start: start, End: start}
		if r.Range != nil {
			rng = *r.Range
		}
		if open := d.server.GetDocument(r.URI); open != nil {
			rng = protocol.Range{Start: start, End: start}
		}
		d.ranges[r.URI] = rng
		remote = append(remote, indexer.Location{URI: r.URI, Line: int(start.Line) + 1, Column: int(start.Character) + 1})
	}
	return remote
}
