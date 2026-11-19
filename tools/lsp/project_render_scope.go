// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openchoreo/openchoreo/tools/lint/indexer"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
	"gopkg.in/yaml.v3"
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
	eligible := func(doc *parser.DocumentNode) bool {
		if strings.HasPrefix(doc.Kind, "Cluster") {
			return true
		}
		if indexer.NamespaceOrDefault(doc.Metadata.Namespace) != scopeNS {
			return false
		}
		if doc.Kind == "Project" {
			return doc.Metadata.Name == scope.ProjectName
		}
		owner := scalarAt(doc.Spec, "owner", "projectName")
		return owner == "" || owner == scope.ProjectName
	}
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
		if d.Kind == "Project" && d.Metadata.Name == scope.ProjectName || owner == scope.ProjectName || s.URI == scope.ActiveURI && owner == "" {
			add(i)
		}
	}
	if len(queue) == 0 {
		return nil, fmt.Errorf("no resources for OpenChoreo project %q were found in namespace %q", scope.ProjectName, scopeNS)
	}
	resolve := func(kind, name, namespace string) {
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
		var walk func(*yaml.Node, string)
		walk = func(n *yaml.Node, field string) {
			if n == nil {
				return
			}
			if n.Kind == yaml.SequenceNode {
				for _, item := range n.Content {
					walk(item, field)
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
						switch doc.Kind {
						case "Project":
							kind = "ProjectType"
						case "Resource":
							kind = "ResourceType"
						case "Component":
							kind = "ComponentType"
						}
					}
				}
				if v.Kind == yaml.ScalarNode {
					switch key {
					case "environment":
						kind = "Environment"
					case "releaseName":
						kind = "ComponentRelease"
					case "resourceRelease":
						kind = "ResourceRelease"
					case "projectRelease":
						kind = "ProjectRelease"
					}
					resolve(kind, v.Value, namespace)
				}
				walk(v, key)
			}
		}
		walk(doc.Spec, "spec")
	}
	var out []renderingSource
	for i, s := range all {
		if selected[i] {
			out = append(out, s)
		}
	}
	return out, nil
}
