// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package codecompletion

import (
	"regexp"
	"strings"
	"unicode/utf8"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"

	celvalidator "github.com/openchoreo/openchoreo/tools/lint/cel-validator"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
)

var celMemberPrefix = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Za-z_][A-Za-z0-9_]*)?$`)

// CEL completion precedes YAML completion, but only inside an expression value.
// All repair and marker insertion happens on a copy, never the editor buffer.
func celMemberItems(text string, line, column int) ([]protocol.CompletionItem, bool) {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) || column < 1 {
		return nil, false
	}
	current := lines[line-1]
	byteCursor, units := 0, 0
	for byteCursor < len(current) && units < column-1 {
		r, size := utf8.DecodeRuneInString(current[byteCursor:])
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > column-1 {
			return nil, false
		}
		units += width
		byteCursor += size
	}
	if units != column-1 {
		return nil, false
	}
	match := celMemberPrefix.FindStringSubmatchIndex(current[:byteCursor])
	if match == nil {
		return nil, false
	}
	receiver := current[match[2]:match[3]]
	prefix := ""
	start := byteCursor
	if match[4] >= 0 {
		prefix = current[match[4]:match[5]]
		start = match[4]
	}
	// An identifier following another identifier or dot is not a new receiver.
	if match[0] > 0 {
		b := current[match[0]-1]
		if celIdentifierByte(b) || b == '.' {
			return nil, false
		}
	}
	end := byteCursor
	for end < len(current) && celIdentifierByte(current[end]) {
		end++
	}
	marker := "__occ_member_completion__"
	for strings.Contains(text, marker) {
		marker += "x"
	}
	patched := append([]string(nil), lines...)
	patched[line-1] = current[:start] + marker + current[end:]
	docs, target := celCompletionDocument(patched, line, marker)
	if target == nil || !insideCELMember(target.Value, marker) {
		return nil, false
	}
	doc := docs[0]
	if !parser.IsOpenChoreoAPIVersion(doc.APIVersion) {
		return nil, false
	}
	schemas, err := celvalidator.ExtractSchemas(doc.Spec)
	if err != nil {
		return []protocol.CompletionItem{}, true
	}
	members, err := celvalidator.InspectExpressionMembers(doc, schemas, marker, receiver)
	if err != nil {
		return []protocol.CompletionItem{}, true
	}
	items := make([]protocol.CompletionItem, 0, len(members))
	for _, member := range members {
		name := strings.TrimPrefix(member.Name, receiver+".")
		if name == member.Name || !strings.HasPrefix(name, prefix) {
			continue
		}
		kind := protocol.CompletionItemKindField
		edit := protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: lsputil.PositionIndex(line - 1), Character: lsputil.PositionIndex(lsputil.UTF16Length(current[:start]))}, End: protocol.Position{Line: lsputil.PositionIndex(line - 1), Character: lsputil.PositionIndex(lsputil.UTF16Length(current[:end]))}}, NewText: name}
		detail := member.Type
		item := protocol.CompletionItem{Label: name, Kind: &kind, Detail: &detail, TextEdit: &edit, Data: map[string]any{"openchoreoCelMember": true}}
		if paren := strings.IndexByte(name, '('); paren >= 0 {
			kind = protocol.CompletionItemKindMethod
			functionName := name[:paren]
			// Preserve arguments/parentheses already present after the identifier.
			if strings.HasPrefix(strings.TrimLeft(current[end:], " \t"), "(") {
				edit.NewText = functionName
			} else if name[paren:] != "()" {
				// Signatures describe argument types, never literal argument values.
				// Put the caret inside the call for the user to supply arguments.
				edit.NewText = functionName + "($0)"
				format := protocol.InsertTextFormatSnippet
				item.InsertTextFormat = &format
			}
			item.FilterText = &functionName
		}
		if member.Description != "" {
			item.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: member.Description}
		}
		items = append(items, item)
	}
	return items, true
}
func celIdentifierByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
func scalarWithMarker(root *yaml.Node, marker string) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.ScalarNode && strings.Contains(root.Value, marker) {
		return root
	}
	for _, child := range root.Content {
		if node := scalarWithMarker(child, marker); node != nil {
			return node
		}
	}
	return nil
}

// Scan decoded YAML rather than raw quoted source, so YAML escapes and CEL
// strings/comments remain distinct. Only the state at the unique marker matters.
func insideCELMember(value, marker string) bool {
	stop := strings.Index(value, marker)
	if stop < 0 {
		return false
	}
	depth := 0
	quote := byte(0)
	triple := false
	raw := false
	comment := false
	for i := 0; i < stop; i++ {
		c := value[i]
		if depth == 0 {
			if c == '$' && i+1 < stop && value[i+1] == '{' {
				depth = 1
				i++
			}
			continue
		}
		if comment {
			if c == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			i, quote, triple = consumeCELQuoted(value, i, stop, quote, triple, raw)
			continue
		}
		if c == '/' && i+1 < stop && value[i+1] == '/' {
			comment = true
			i++
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			raw = i > 0 && (value[i-1] == 'r' || value[i-1] == 'R') && (i < 2 || !celIdentifierByte(value[i-2]))
			triple = i+2 < stop && value[i+1] == c && value[i+2] == c
			if triple {
				i += 2
			}
			continue
		}
		if c == '{' {
			depth++
		}
		if c == '}' {
			depth--
		}
	}
	return depth > 0 && quote == 0 && !comment
}

func celCompletionDocument(patched []string, line int, marker string) ([]*parser.DocumentNode, *yaml.Node) {
	var docs []*parser.DocumentNode
	var target *yaml.Node
	// First preserve the entire document. If YAML is unfinished, try closing the
	// current scalar and finally ignore incomplete text below the cursor line.
	for _, truncate := range []bool{false, true} {
		for _, quote := range []string{"", "'", "\""} {
			candidate := append([]string(nil), patched...)
			candidate[line-1] += quote
			if truncate {
				candidate = candidate[:line]
			}
			parsed, err := parser.ParseYAML([]byte(strings.Join(candidate, "\n")))
			if err != nil {
				continue
			}
			for _, doc := range parsed {
				if n := scalarWithMarker(doc.Root, marker); n != nil {
					docs = []*parser.DocumentNode{doc}
					target = n
					break
				}
			}
			if target != nil {
				break
			}
		}
		if target != nil {
			break
		}
	}
	return docs, target
}

func consumeCELQuoted(value string, i, stop int, quote byte, triple, raw bool) (int, byte, bool) {
	if value[i] == '\\' && !raw {
		return i + 1, quote, triple
	}
	if value[i] != quote {
		return i, quote, triple
	}
	if !triple {
		return i, 0, triple
	}
	if i+2 < stop && value[i+1] == quote && value[i+2] == quote {
		return i + 2, 0, false
	}
	return i, quote, triple
}
