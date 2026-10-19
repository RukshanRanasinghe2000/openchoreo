// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package fixer

import (
	"regexp"
	"strings"
)

// blockHeaderRe matches a line whose value is a literal (|) or folded (>)
// block scalar header, either as a mapping value or as a sequence item.
// Only the header itself matches, never a scalar that merely contains a pipe.
var blockHeaderRe = regexp.MustCompile(`(?::\s+|^\s*-\s*)[|>][0-9+-]*\s*(#.*)?$`)

// FormatYAML normalizes the whitespace problems that cannot change how the
// document parses: trailing spaces, CRLF line endings, runs of blank lines and
// the final newline. Comments and content lines of block scalars are left
// alone, because a trailing space or a blank line inside a literal block is
// part of the value.
func FormatYAML(data []byte) []byte {
	text := string(data)
	text = strings.ReplaceAll(text, "\r\n", "\n")

	lines := splitLines(text)
	protected := blockScalarLines(lines)
	for i := range lines {
		if protected[i] {
			continue
		}
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	lines = collapseBlankLines(lines, protected)
	lines = trimTrailingBlanks(lines, protected)

	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return data
	}
	// An empty last line is already the final newline, and a last line a block
	// scalar owns must stay byte-for-byte: adding a terminator after it would
	// change the value of a "|+" scalar.
	if lines[len(lines)-1] == "" || protected[len(lines)-1] {
		return []byte(strings.Join(lines, "\n"))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// blockScalarLines marks the content lines of every block scalar, which the
// formatter must not touch. A block scalar continues while the following lines
// are blank or indented deeper than the header, exactly the span the YAML
// parser reads as the scalar's value.
func blockScalarLines(lines []string) []bool {
	marked := make([]bool, len(lines))
	for i, ln := range lines {
		trimmed := strings.TrimRight(ln, " \t")
		if trimmed == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			continue
		}
		if !blockHeaderRe.MatchString(trimmed) {
			continue
		}
		indent := indentWidth(ln)
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimRight(lines[j], " \t")
			if strings.TrimSpace(next) == "" {
				marked[j] = true
				continue
			}
			if indentWidth(lines[j]) <= indent {
				break
			}
			marked[j] = true
		}
	}
	return marked
}

// collapseBlankLines keeps at most one blank line in a run of them, skipping
// blank lines that belong to a block scalar.
func collapseBlankLines(lines []string, protected []bool) []string {
	out := make([]string, 0, len(lines))
	prevBlank := false
	for i, ln := range lines {
		blank := strings.TrimSpace(ln) == ""
		if blank && !protected[i] {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		out = append(out, ln)
	}
	return out
}

// trimTrailingBlanks drops blank lines at the end of the file so the content
// ends with exactly one newline. A blank line that a block scalar owns is kept:
// with "|+" chomping it is part of the value.
func trimTrailingBlanks(lines []string, protected []bool) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" && !protected[end-1] {
		end--
	}
	return lines[:end]
}

// indentWidth returns the number of leading spaces on a line.
func indentWidth(ln string) int {
	return len(ln) - len(strings.TrimLeft(ln, " \t"))
}
