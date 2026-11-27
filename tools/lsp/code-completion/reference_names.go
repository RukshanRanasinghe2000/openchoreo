// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package codecompletion

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"

	"github.com/openchoreo/openchoreo/tools/lint/indexer"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/schema"
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/template"
	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
)

var scalarValueLine = regexp.MustCompile(`^([ \t]*(?:-[ \t]+)?(?:[A-Za-z_][A-Za-z0-9_]*|'name'|"name")[ \t]*:)([ \t]*)(.*)$`)

type referenceValueSlot struct {
	head, field        string
	start, end         int // byte offsets of scalar content, excluding its quotes/comment
	quote              byte
	closed, needsSpace bool
}

// scalarSlot never treats a cursor in a comment, key or trailing field as a
// value. Its offsets refer to the original text, not the repaired parser copy.
func scalarSlot(line string, column int) (referenceValueSlot, bool) {
	m := scalarValueLine.FindStringSubmatchIndex(line)
	if m == nil {
		return referenceValueSlot{}, false
	}
	cursor, ok := completionByteOffset(line, column-1)
	if !ok {
		return referenceValueSlot{}, false
	}
	head := line[:m[3]]
	key := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(head), "-")), ":"))
	key = strings.Trim(key, `'"`)
	slot := referenceValueSlot{head: head, field: key, start: m[6], end: len(strings.TrimRight(line, "\r")), needsSpace: m[4] == m[5]}
	if slot.start < len(line) && (line[slot.start] == '\'' || line[slot.start] == '"') {
		slot.quote = line[slot.start]
		slot.start++
		for i := slot.start; i < slot.end; i++ {
			if slot.quote == '"' && line[i] == '\\' {
				i++
				continue
			}
			if line[i] != slot.quote {
				continue
			}
			if slot.quote == '\'' && i+1 < slot.end && line[i+1] == '\'' {
				i++
				continue
			}
			slot.end = i
			slot.closed = true
			tail := strings.TrimSpace(line[i+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return referenceValueSlot{}, false
			}
			break
		}
	} else {
		for i := slot.start; i < slot.end; i++ {
			if line[i] == '#' && (i == slot.start || line[i-1] == ' ' || line[i-1] == '\t') {
				slot.end = i
				break
			}
		}
		slot.end = len(strings.TrimRight(line[:slot.end], " \t\r"))
		if slot.end < slot.start {
			slot.end = slot.start
		}
	}
	if slot.quote == 0 && slot.end == slot.start && cursor >= len(slot.head) && cursor <= slot.start {
		slot.start = cursor
		slot.needsSpace = cursor == len(slot.head)
	}
	if cursor < slot.start || cursor > slot.end || strings.Contains(line[slot.start:slot.end], "${") {
		return referenceValueSlot{}, false
	}
	// Flow collections and block scalars require a different scalar edit shape.
	if slot.quote == 0 && strings.ContainsAny(line[slot.start:slot.end], "{}[]|>") {
		return referenceValueSlot{}, false
	}
	return slot, true
}
func completionByteOffset(text string, units int) (int, bool) {
	if units < 0 {
		return 0, false
	}
	for offset, r := range text {
		if units == 0 {
			return offset, true
		}
		width := 1
		if r > 0xffff {
			width = 2
		}
		units -= width
		if units < 0 {
			return 0, false
		}
	}
	if units == 0 {
		return len(text), true
	}
	return 0, false
}

func referenceValueEdit(ctx completionContext, value string) (*protocol.TextEdit, bool) {
	lines := strings.Split(ctx.text, "\n")
	if ctx.line < 1 || ctx.line > len(lines) || strings.ContainsAny(value, "\r\n") {
		return nil, false
	}
	line := lines[ctx.line-1]
	slot, ok := scalarSlot(line, ctx.col)
	if !ok {
		return nil, false
	}
	replacement := value
	switch slot.quote {
	case '\'':
		replacement = strings.ReplaceAll(value, "'", "''")
	case '"':
		encoded := strconv.Quote(value)
		replacement = encoded[1 : len(encoded)-1]
	default:
		encoded, err := yaml.Marshal(value)
		if err != nil {
			return nil, false
		}
		replacement = strings.TrimSuffix(string(encoded), "\n")
	}
	if slot.quote == 0 && slot.end < len(line) && line[slot.end] == '#' {
		replacement += " "
	}
	if slot.quote != 0 && !slot.closed {
		replacement += string(slot.quote)
	}
	start := slot.start
	if slot.needsSpace {
		replacement = " " + replacement
		if slot.quote != 0 {
			start--
			replacement = " " + string(slot.quote) + replacement[1:]
		}
	}
	return &protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: lsputil.PositionIndex(ctx.line - 1), Character: lsputil.PositionIndex(lsputil.UTF16Length(line[:start]))}, End: protocol.Position{Line: lsputil.PositionIndex(ctx.line - 1), Character: lsputil.PositionIndex(lsputil.UTF16Length(line[:slot.end]))}}, NewText: replacement}, true
}

// referenceNameItems resolves a name/kind pair in the same actual YAML mapping.
// Repair only the scalar under the cursor; never mutate the user's document or
// infer a kind from another sequence item or a preserved arbitrary object.
func referenceNameItems(text string, line, column int, idx ResourceIndexer, uri string) ([]protocol.CompletionItem, bool) {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return nil, false
	}
	slot, ok := scalarSlot(lines[line-1], column)
	if !ok || slot.field != "name" {
		return nil, false
	}
	candidates, marker := referenceCompletionCandidates(text, lines, line, slot.head)
	for _, candidate := range candidates {
		docs, err := parser.ParseYAML([]byte(candidate))
		if err != nil {
			continue
		}
		for _, doc := range docs {
			parent, path := referenceMarkerMapping(doc.Root, marker, nil)
			if parent == nil {
				continue
			}
			if doc.APIVersion != "" && !parser.IsOpenChoreoAPIVersion(doc.APIVersion) {
				return nil, false
			}
			root := template.SchemaFor(doc.Kind)
			object := schema.FindSchema(root, strings.Join(path, "."))
			if object != nil && object.Type == valueArray {
				object = object.Items
			}
			if object == nil || object.Properties["name"] == nil || object.Properties[valueKind] == nil {
				return nil, false
			}
			kind := dynamicScalar(dynamicNode(parent, valueKind))
			if kind == "" || template.SchemaFor(kind) == nil {
				return []protocol.CompletionItem{}, true
			}
			kindSchema := object.Properties[valueKind]
			if len(kindSchema.Enum) > 0 {
				allowed := false
				for _, k := range kindSchema.Enum {
					if k == kind {
						allowed = true
						break
					}
				}
				if !allowed {
					return []protocol.CompletionItem{}, true
				}
			}
			namespace := doc.Metadata.Namespace
			if namespace == "" {
				if u, e := url.Parse(uri); e == nil && u.Scheme == "openchoreo" {
					parts := strings.Split(strings.Trim(u.Path, "/"), "/")
					if len(parts) >= 2 && parts[0] == "namespaces" {
						namespace = parts[1]
					}
				}
			}
			names := scopedReferenceNames(idx, kind, indexer.NamespaceOrDefault(namespace), doc.APIVersion, uri)
			ctx := completionContext{text: text, line: line, col: column, field: "name", mode: modeValue}
			return buildNameItems(names, ctx), true
		}
	}
	return nil, false
}
func referenceMarkerMapping(n *yaml.Node, marker string, path []string) (*yaml.Node, []string) {
	if n == nil {
		return nil, nil
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.Value == "name" && value.Kind == yaml.ScalarNode && value.Value == marker {
				return n, path
			}
			next := append(append([]string(nil), path...), key.Value)
			if parent, p := referenceMarkerMapping(value, marker, next); parent != nil {
				return parent, p
			}
		}
	} else if n.Kind == yaml.DocumentNode || n.Kind == yaml.SequenceNode {
		for _, child := range n.Content {
			if parent, p := referenceMarkerMapping(child, marker, path); parent != nil {
				return parent, p
			}
		}
	}
	return nil, nil
}
func scopedReferenceNames(idx ResourceIndexer, kind, namespace, api, uri string) []string {
	if idx == nil {
		return nil
	}
	if scoped, ok := idx.(interface {
		ReferenceNames(string, string, string, string) []string
	}); ok {
		return scoped.ReferenceNames(kind, namespace, api, uri)
	}
	var docs []indexer.SnapshotDocument
	if workspace, ok := idx.(interface {
		DefinitionSnapshot(string) []indexer.SnapshotDocument
	}); ok {
		docs = workspace.DefinitionSnapshot(uri)
	} else if workspace, ok := idx.(interface {
		Snapshot(string) []indexer.SnapshotDocument
	}); ok {
		docs = workspace.Snapshot(uri)
	} else {
		return idx.NamesByKind()[kind]
	}
	var names []string
	for _, entry := range docs {
		d := entry.Document
		if d != nil && d.Kind == kind && (api == "" || d.APIVersion == api) && (strings.HasPrefix(kind, "Cluster") || indexer.NamespaceOrDefault(d.Metadata.Namespace) == namespace) {
			names = append(names, d.Metadata.Name)
		}
	}
	sort.Strings(names)
	return names
}

func referenceCompletionCandidates(text string, lines []string, line int, head string) ([]string, string) {
	marker := "__occ_reference_completion__"
	for strings.Contains(text, marker) {
		marker += "x"
	}
	patched := append([]string(nil), lines...)
	patched[line-1] = head + " " + marker
	// Isolate the active YAML document so an unrelated unfinished document does
	// not suppress suggestions. Keep following lines to find reversed kind/name.
	first, last := line-1, line
	for first > 0 && strings.TrimSpace(lines[first-1]) != yamlDocumentStart && strings.TrimSpace(lines[first-1]) != "..." {
		first--
	}
	for last < len(lines) && strings.TrimSpace(lines[last]) != yamlDocumentStart && strings.TrimSpace(lines[last]) != "..." {
		last++
	}
	// If a following field is unfinished, keep the complete current mapping
	// (including a following kind) but omit the later sibling/sequence item.
	boundary := last
	indent := len(lines[line-1]) - len(strings.TrimLeft(lines[line-1], " "))
	if strings.HasPrefix(strings.TrimSpace(lines[line-1]), "- ") {
		indent += 2
	}
	for i := line; i < last; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if level <= indent && !strings.HasPrefix(trimmed, "kind:") {
			boundary = i
			break
		}
	}
	candidates := []string{strings.Join(patched, "\n"), strings.Join(patched[first:last], "\n"), strings.Join(patched[first:boundary], "\n"), strings.Join(patched[first:line], "\n")}
	return candidates, marker
}
