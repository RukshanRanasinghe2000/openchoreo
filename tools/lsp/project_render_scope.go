// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/openchoreo/openchoreo/tools/lint/indexer"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
)

type renderingProjectScope struct {
	ProjectName string `json:"projectName"`
	Namespace   string `json:"namespace"`
	ActiveURI   string `json:"activeUri,omitempty"`
}
type renderingSource struct {
	URI  string
	Text string
	Doc  *parser.DocumentNode
	Err  error
}

// Project membership is defined by OpenChoreo ownership, never by the number
// of projects in a VS Code folder or namespace. Shared definitions enter the
// run only through references made by the selected project.
func selectRenderingProject(all []renderingSource, scope *renderingProjectScope) ([]renderingSource, error) {
	if scope == nil {
		return all, nil
	} // Existing protocol callers remain compatible.
	if strings.TrimSpace(scope.ProjectName) == "" {
		return nil, fmt.Errorf("select an OpenChoreo project before running validation")
	}
	scopeNS := indexer.NamespaceOrDefault(scope.Namespace)
	kinds := map[string]bool{}
	for _, s := range all {
		if s.Doc != nil {
			kinds[s.Doc.Kind] = true
		}
	}
	var knownKinds []string
	for kind := range kinds {
		knownKinds = append(knownKinds, kind)
	}
	sort.Slice(knownKinds, func(i, j int) bool {
		if len(knownKinds[i]) != len(knownKinds[j]) {
			return len(knownKinds[i]) > len(knownKinds[j])
		}
		return knownKinds[i] < knownKinds[j]
	})
	eligible := func(doc *parser.DocumentNode) bool { return renderingEligible(doc, scope, scopeNS) }
	selected := map[int]bool{}
	var queue []int
	add := func(i int) {
		if !selected[i] {
			selected[i] = true
			queue = append(queue, i)
		}
	}
	for i, s := range all {
		if s.Doc == nil {
			if s.URI == scope.ActiveURI {
				add(i)
			}
			continue
		}
		d := s.Doc
		if !eligible(d) {
			continue
		}
		owner := scalarAt(d.Spec, "owner", "projectName")
		if d.Kind == kindProject && d.Metadata.Name == scope.ProjectName || owner == scope.ProjectName || s.URI == scope.ActiveURI && owner == "" {
			add(i)
		}
	}
	if len(queue) == 0 {
		return nil, fmt.Errorf("no resources for OpenChoreo project %q were found in namespace %q", scope.ProjectName, scopeNS)
	}
	resolve := func(kind, name, namespace string) {
		resolveRenderingReference(all, eligible, add, kind, name, namespace)
	}
	for next := 0; next < len(queue); next++ {
		source := all[queue[next]]
		if source.Doc == nil {
			continue
		}
		doc := source.Doc
		namespace := indexer.NamespaceOrDefault(doc.Metadata.Namespace)
		if strings.HasPrefix(doc.Kind, "Cluster") {
			namespace = scopeNS
		}
		walkRenderingReferences(doc.Spec, "spec", doc.Kind, knownKinds, namespace, resolve)
	}
	var out []renderingSource
	for i, s := range all {
		if selected[i] {
			out = append(out, s)
		}
	}
	return out, nil
}

func renderingEligible(doc *parser.DocumentNode, scope *renderingProjectScope, scopeNS string) bool {
	if strings.HasPrefix(doc.Kind, "Cluster") {
		return true
	}
	if indexer.NamespaceOrDefault(doc.Metadata.Namespace) != scopeNS {
		return false
	}
	if doc.Kind == kindProject {
		return doc.Metadata.Name == scope.ProjectName
	}
	owner := scalarAt(doc.Spec, "owner", "projectName")
	return owner == "" || owner == scope.ProjectName
}

func walkRenderingReferences(n *yaml.Node, field, docKind string, knownKinds []string, namespace string, resolve func(string, string, string)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.SequenceNode {
		for _, item := range n.Content {
			walkRenderingReferences(item, field, docKind, knownKinds, namespace, resolve)
		}
		return
	}
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, v := n.Content[i].Value, n.Content[i+1]
		switch key {
		case "template", "parameters", "environmentConfigs", "openAPIV3Schema":
			continue
		}
		kind := ""
		if key == "name" && v.Kind == yaml.ScalarNode {
			if k := yamlMappingValue(n, "kind"); k != nil {
				kind = k.Value
			}
			if kind == "" {
				kind = lsputil.FindKindByFieldName(field, knownKinds)
			}
			if kind == "" && field == "type" {
				switch docKind {
				case kindProject:
					kind = "ProjectType"
				case "Resource":
					kind = "ResourceType"
				case kindComponent:
					kind = "ComponentType"
				}
			}
		}
		if v.Kind == yaml.ScalarNode {
			switch key {
			case "environment":
				kind = "Environment"
			case "releaseName":
				kind = kindComponentRelease
			case "resourceRelease":
				kind = "ResourceRelease"
			case "projectRelease":
				kind = "ProjectRelease"
			}
			resolve(kind, v.Value, namespace)
		}
		walkRenderingReferences(v, key, docKind, knownKinds, namespace, resolve)
	}
}

func resolveRenderingReference(all []renderingSource, eligible func(*parser.DocumentNode) bool, add func(int), kind, name, namespace string) {
	if kind == "" || name == "" || strings.Contains(name, "${") {
		return
	}
	normalized := indexer.ReferenceName(kind, name)
	for i, s := range all {
		d := s.Doc
		if d == nil || !eligible(d) || d.Kind != kind || d.Metadata.Name != normalized {
			continue
		}
		if !strings.HasPrefix(kind, "Cluster") && indexer.NamespaceOrDefault(d.Metadata.Namespace) != namespace {
			continue
		}
		if normalized != name && scalarAt(d.Spec, "workloadType") != strings.Split(name, "/")[0] {
			continue
		}
		add(i)
	}
}
