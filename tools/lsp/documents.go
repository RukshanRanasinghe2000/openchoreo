// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"strings"
	"unicode/utf8"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

type Document struct {
	URI     string
	Text    string
	Version int
}

func (s *Server) OpenDocument(uri, text string, version int) {
	s.docMu.Lock()
	defer s.docMu.Unlock()

	s.documents[uri] = &Document{
		URI:     uri,
		Text:    text,
		Version: version,
	}
}

func (s *Server) UpdateDocument(uri, text string, version int) error {
	s.docMu.Lock()
	defer s.docMu.Unlock()

	doc, ok := s.documents[uri]
	if !ok {
		return fmt.Errorf("document not open: %s", uri)
	}

	s.documents[uri] = &Document{
		URI:     doc.URI,
		Text:    text,
		Version: version,
	}
	return nil
}

// GetDocument returns a copy of an open document, so a caller (including the
// file watcher goroutine) can read it with no risk of racing a concurrent
// didChange/didClose even after the lock is released.
func (s *Server) GetDocument(uri string) *Document {
	s.docMu.RLock()
	defer s.docMu.RUnlock()

	doc := s.documents[uri]
	if doc == nil {
		return nil
	}
	return &Document{URI: doc.URI, Text: doc.Text, Version: doc.Version}
}

// snapshotOpenDocuments returns value copies of every open document for
// background goroutines that need to revalidate all of them.
func (s *Server) snapshotOpenDocuments() []Document {
	s.docMu.RLock()
	defer s.docMu.RUnlock()

	out := make([]Document, 0, len(s.documents))
	for _, d := range s.documents {
		out = append(out, Document{URI: d.URI, Text: d.Text, Version: d.Version})
	}
	return out
}

func (s *Server) CloseDocument(uri string) {
	s.docMu.Lock()
	defer s.docMu.Unlock()

	delete(s.documents, uri)
}

// applyChangesToDocument applies the content changes from a didChange
// notification to the open document. The updated document is stored under the
// write lock; callers receive a copy they own.
func (s *Server) applyChangesToDocument(uri string, params *protocol.DidChangeTextDocumentParams) (*Document, error) {
	s.docMu.RLock()
	current, ok := s.documents[uri]
	s.docMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("document not open: %s", uri)
	}

	updated := &Document{URI: current.URI, Text: current.Text, Version: current.Version}

	// Changes in a single notification must be applied sequentially: c1 moves
	// the document from state S to S', c2 from S' to S''.
	for _, change := range params.ContentChanges {
		switch change := change.(type) {
		case protocol.TextDocumentContentChangeEventWhole:
			updated.Text = change.Text
		case protocol.TextDocumentContentChangeEvent:
			if change.Range == nil {
				// Missing range means the text is the full content.
				updated.Text = change.Text
				continue
			}
			start, end, err := rangeToByteOffsets(updated.Text, change.Range)
			if err != nil {
				return nil, err
			}
			if start > end {
				return nil, fmt.Errorf(
					"invalid change range: start offset %d > end offset %d", start, end)
			}
			updated.Text = updated.Text[:start] + change.Text + updated.Text[end:]
		default:
			return nil, fmt.Errorf("unknown content change type %T", change)
		}
	}

	updated.Version = int(params.TextDocument.Version)

	s.docMu.Lock()
	s.documents[uri] = updated
	s.docMu.Unlock()

	return updated, nil
}

// rangeToByteOffsets converts an LSP range into [start, end) byte offsets into
// text. Positions are validated so malformed ranges surface as errors instead
// of silently corrupting the document.
func rangeToByteOffsets(text string, rng *protocol.Range) (int, int, error) {
	lineStarts := LineStartOffsets(text)

	start, err := PositionToByteOffset(text, lineStarts, rng.Start)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range start: %w", err)
	}
	end, err := PositionToByteOffset(text, lineStarts, rng.End)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range end: %w", err)
	}
	return start, end, nil
}

// LineStartOffsets returns the byte offset of the start of every line,
// counting "\n" as the line terminator (line 0 always starts at byte 0).
func LineStartOffsets(text string) []int {
	offsets := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// PositionToByteOffset converts an LSP position (zero-based line plus UTF-16
// code-unit character offset) to a byte offset inside text.
//
// Line endings are handled best-effort: lines are located by "\n" and a
// trailing "\r" on CRLF lines is not counted toward the character offset.
// If the character value exceeds the line length it clamps to the end of the
// line content, per the LSP spec.
func PositionToByteOffset(text string, lineStarts []int, pos protocol.Position) (int, error) {
	line := int(pos.Line)
	if line >= len(lineStarts) {
		return 0, fmt.Errorf("line %d out of range: document has %d lines",
			line, len(lineStarts))
	}

	lineStart := lineStarts[line]
	lineEnd := len(text)
	if line+1 < len(lineStarts) {
		lineEnd = lineStarts[line+1]
	}

	lineContent := text[lineStart:lineEnd]
	if before, ok := strings.CutSuffix(lineContent, "\n"); ok {
		lineContent = before
	}
	lineContent = strings.TrimSuffix(lineContent, "\r")

	charOffset := utf16ToByteOffset(lineContent, pos.Character)
	if charOffset > len(lineContent) {
		charOffset = len(lineContent)
	}
	return lineStart + charOffset, nil
}

// utf16ToByteOffset counts UTF-16 code units (not runes) into line and returns
// the matching byte offset. A rune above U+FFFF (a surrogate pair) consumes
// two code units.
func utf16ToByteOffset(line string, character uint32) int {
	byteOffset := 0
	remaining := line
	count := int(character)

	for i := 0; i < count; i++ {
		if len(remaining) == 0 {
			break
		}
		r, size := utf8.DecodeRuneInString(remaining)
		if r >= 0x10000 {
			// Surrogate pair: consume a second code unit.
			i++
		}
		remaining = remaining[size:]
		byteOffset += size
	}
	return byteOffset
}
