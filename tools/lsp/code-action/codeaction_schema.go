// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package codeaction

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/schema"
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/template"
	"github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// schemaFixes walks the parsed document against its JSON schema and produces
// quick fixes for missing required fields, invalid enum values, and misspelt
// field names at every level of the YAML, giving schema-driven fixes for "each
// and every field" rather than only the handful of top-level diagnostics.
func schemaFixes(uri, text string, root *schema.FieldSchema, clientDiags []protocol.Diagnostic, requestRange protocol.Range) []protocol.CodeAction {
	if root == nil {
		return nil
	}
	// The root `kind` field is a free-form string in every schema file; declare
	// every known kind name as its enum so a misspelt kind value is corrected
	// by the same schema-driven enum fixer used for every other field.
	if kind, ok := root.Properties["kind"]; ok && len(kind.Enum) == 0 {
		kind.Enum = template.AllKinds()
	}
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil {
		return nil
	}

	var actions []protocol.CodeAction
	lines := splitLines(text)
	for _, doc := range docs {
		if doc.Root == nil || doc.Root.Kind != yaml.MappingNode {
			continue
		}
		walkMap(uri, text, lines, root, doc.Root, &actions, clientDiags, requestRange)
	}
	return actions
}

// walkMap inspects a mapping node against a schema object and emits fixes for
// its children, recursing into nested objects/arrays.
func walkMap(uri, text string, lines []string, sc *schema.FieldSchema, m *yaml.Node, actions *[]protocol.CodeAction, clientDiags []protocol.Diagnostic, requestRange protocol.Range) {
	if sc == nil || m == nil || m.Kind != yaml.MappingNode {
		return
	}

	// index existing keys by name -> (keyNode, valueNode)
	existing := map[string]*yaml.Node{}
	var childIndices []int
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		if k.Kind == yaml.ScalarNode {
			existing[k.Value] = v
			childIndices = append(childIndices, i)
		}
	}

	// Ensure schema order is deterministic.
	names := make([]string, 0, len(sc.Properties))
	for n := range sc.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	sort.SliceStable(names, func(a, b int) bool {
		ra := sc.Properties[names[a]].Required
		rb := sc.Properties[names[b]].Required
		if ra != rb {
			return ra
		}
		return false
	})

	mappingIndent := indentOfMap(m)

	// 1) Missing required fields.
	for _, name := range names {
		child := sc.Properties[name]
		if !child.Required {
			continue
		}
		if _, ok := existing[name]; ok {
			continue
		}
		edit := insertSiblingEdit(lines, m, mappingIndent, name, child)
		if edit == nil {
			continue
		}
		diag := fakeDiag(clientDiags, edit.Range, "missing-field", fmt.Sprintf("%s is required", name))
		title := fmt.Sprintf("Add %s", name)
		*actions = append(*actions, *quickFix(title, uri, diag, []protocol.TextEdit{*edit}))
	}

	// 2+3) Unknown/misspelt keys, enum fixes, and recursion.
	for _, i := range childIndices {
		keyNode := m.Content[i]
		valNode := m.Content[i+1]
		fieldName := keyNode.Value

		childSchema, known := sc.Properties[fieldName]

		if !known && !sc.Preserve {
			// Misspelt / unrecognized field: offer the closest schema field.
			matches := closestMatches(fieldName, names, 1)
			if len(matches) > 0 && matches[0] != fieldName {
				edit := replaceTokenAt(lines, keyNode, matches[0])
				if edit != nil {
					diag := fakeDiag(clientDiags, edit.Range, "unknown-field", fmt.Sprintf("unknown field %q", fieldName))
					title := fmt.Sprintf("Fix field: change %q to %q", fieldName, matches[0])
					*actions = append(*actions, *quickFix(title, uri, diag, []protocol.TextEdit{*edit}))
				}
				continue
			}
		}

		// Enum violation for scalar leaves.
		if childSchema != nil && valNode.Kind == yaml.ScalarNode && len(childSchema.Enum) > 0 {
			if fix := enumFix(uri, lines, text, keyNode, valNode, childSchema, requestRange, clientDiags); fix != nil {
				*actions = append(*actions, *fix)
			}
		}

		// Recurse into nested structures.
		switch valNode.Kind {
		case yaml.MappingNode:
			if childSchema != nil {
				walkMap(uri, text, lines, childSchema, valNode, actions, clientDiags, requestRange)
			}
		case yaml.SequenceNode:
			if childSchema != nil && childSchema.Items != nil {
				for _, item := range valNode.Content {
					if item.Kind == yaml.MappingNode {
						walkMap(uri, text, lines, childSchema.Items, item, actions, clientDiags, requestRange)
					}
				}
			}
		}
	}
}

// indentOfMap returns the number of leading spaces on a mapping's first line.
func indentOfMap(m *yaml.Node) string {
	if m == nil || len(m.Content) == 0 {
		return ""
	}
	return strings.Repeat(" ", m.Column-1)
}

// insertSiblingEdit builds a TextEdit that inserts `key:` (with its fully
// expanded structure) as the next sibling line after a mapping.
func insertSiblingEdit(lines []string, m *yaml.Node, indent, name string, child *schema.FieldSchema) *protocol.TextEdit {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	// The mapping's last child determines the insertion line (end of its value).
	last := m.Content[len(m.Content)-1]
	anchor := last
	if len(m.Content) >= 2 {
		anchor = m.Content[len(m.Content)-1] // value of last key
	}
	bottomLine := bottomLine(anchor)
	if bottomLine >= len(lines) {
		bottomLine = len(lines) - 1
	}
	if bottomLine < 0 {
		return nil
	}
	body := "\n" + indent + name + ":" + lsputil.ExpandObjectBody(name, child, indent+"  ")
	edit := &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: protocol.UInteger(bottomLine), Character: protocol.UInteger(utf16Len(lines[bottomLine]))},
			End:   protocol.Position{Line: protocol.UInteger(bottomLine), Character: protocol.UInteger(utf16Len(lines[bottomLine]))},
		},
		NewText: body,
	}
	return edit
}

// bottomLine returns the last source line of a yaml node (0-indexed).
func bottomLine(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	switch n.Kind {
	case yaml.ScalarNode, yaml.AliasNode:
		return n.Line - 1
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return bottomLine(n.Content[0])
		}
		return n.Line - 1
	default:
		last := n
		if len(n.Content) > 0 {
			last = n.Content[len(n.Content)-1]
		}
		return bottomLine(last)
	}
}

// utf16Len returns the UTF-16 length of a string (LSP character counts in
// UTF-16 code units). For ASCII YAML this equals byte length.
func utf16Len(s string) int {
	return len([]rune(s))
}

// replaceTokenAt replaces the exact text span of a yaml scalar node with the
// given replacement.
func replaceTokenAt(lines []string, n *yaml.Node, replacement string) *protocol.TextEdit {
	if n == nil || n.Line < 1 {
		return nil
	}
	li := n.Line - 1
	if li >= len(lines) {
		return nil
	}
	start := n.Column - 1
	token := n.Value
	end := start + utf16Len(token)
	if end > utf16Len(lines[li]) {
		end = utf16Len(lines[li])
	}
	return &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: protocol.UInteger(li), Character: protocol.UInteger(start)},
			End:   protocol.Position{Line: protocol.UInteger(li), Character: protocol.UInteger(end)},
		},
		NewText: replacement,
	}
}

// enumFix offers to replace a scalar value with the closest enum option.
func enumFix(uri string, lines []string, text string, keyNode, valNode *yaml.Node, child *schema.FieldSchema, requestRange protocol.Range, clientDiags []protocol.Diagnostic) *protocol.CodeAction {
	current := valNode.Value
	if current == "" {
		return nil
	}
	// If the value already matches an allowed enum member, case-insensitively,
	// there is nothing to fix. closestMatches skips equal members, so this
	// cannot be inferred from its result.
	for _, e := range child.Enum {
		if strings.EqualFold(e, current) {
			return nil
		}
	}
	matches := closestMatches(current, child.Enum, 1)
	if len(matches) == 0 || matches[0] == current {
		return nil
	}
	edit := replaceTokenAt(lines, valNode, matches[0])
	if edit == nil {
		return nil
	}
	diag := fakeDiag(clientDiags, edit.Range, "invalid-enum", fmt.Sprintf("%s must be one of: %s", keyNode.Value, strings.Join(child.Enum, ", ")))
	title := fmt.Sprintf("Change %s to %q", keyNode.Value, matches[0])
	return quickFix(title, uri, diag, []protocol.TextEdit{*edit})
}

// fakeDiag builds a diagnostic for a code action, reusing a client diagnostic
// at the same line when available so the fix is anchored to a reported issue.
func fakeDiag(client []protocol.Diagnostic, r protocol.Range, code, message string) protocol.Diagnostic {
	d := protocol.Diagnostic{Range: r, Message: message}
	sev := protocol.DiagnosticSeverity(1)
	d.Severity = &sev
	d.Code = &protocol.IntegerOrString{Value: code}
	d.Source = lsputil.StringPtr("openchoreo")
	for _, c := range client {
		if c.Range.Start.Line == r.Start.Line {
			if c.Severity != nil {
				sev = *c.Severity
			}
			break
		}
	}
	d.Severity = &sev
	return d
}
