// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package fixer

import (
	"fmt"
	"sort"
	"strings"
)

// Edit replaces the token of value token at the 1-indexed position (line, col)
// with newText. Every fix the linter applies is a single-token replacement, so
// an edit never spans lines and can be applied without re-parsing the document.
type Edit struct {
	Line    int
	Col     int
	Token   string
	NewText string
}

// fix pairs a text edit with the diagnostic code and human message reported
// for it.
type fix struct {
	edit    Edit
	code    string
	message string
	from    string
	to      string
}

// splitLines splits source into lines, keeping the empty element produced by a
// trailing newline so joining the result back is lossless.
func splitLines(source string) []string {
	return strings.Split(source, "\n")
}

// TokenSpan locates the token value inside line, which starts at the 1-indexed
// column col. It reports ok=false when the text at that position is not the
// token itself, which happens for quoted keys and any position the parser
// reported differently from the raw file. Such edits are skipped rather than
// guessed at, so a fix can never corrupt an unrelated part of the file.
func TokenSpan(line string, col int, value string) (start, end int, ok bool) {
	start = col - 1
	if start < 0 || start > len(line) {
		return 0, 0, false
	}
	end = start + len(value)
	if end > len(line) || line[start:end] != value {
		return 0, 0, false
	}
	return start, end, true
}

// applyEdits rewrites lines with the given fixes. Fixes are applied from the
// bottom of the file upwards so that each replacement leaves the positions of
// the fixes above it untouched. Fixes whose token no longer matches the text at
// their recorded position are skipped, and a fix is never applied twice to the
// same position.
func applyEdits(lines []string, fixes []fix) []fix {
	var applied []fix
	sorted := make([]fix, len(fixes))
	copy(sorted, fixes)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].edit.Line != sorted[j].edit.Line {
			return sorted[i].edit.Line > sorted[j].edit.Line
		}
		return sorted[i].edit.Col > sorted[j].edit.Col
	})

	touched := make(map[[2]int]bool, len(sorted))
	for _, f := range sorted {
		pos := [2]int{f.edit.Line, f.edit.Col}
		if touched[pos] {
			continue
		}
		li := f.edit.Line - 1
		if li < 0 || li >= len(lines) {
			continue
		}
		start, end, ok := TokenSpan(lines[li], f.edit.Col, f.edit.Token)
		if !ok {
			continue
		}
		lines[li] = lines[li][:start] + f.edit.NewText + lines[li][end:]
		touched[pos] = true
		applied = append(applied, f)
	}
	return applied
}

// fixMessage renders the "what changed" text reported for a fix.
func fixMessage(label, from, to string) string {
	if from == "" {
		return label
	}
	return fmt.Sprintf("%s %q -> %q", label, from, to)
}
