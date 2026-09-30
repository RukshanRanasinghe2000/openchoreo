// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package fixer

import (
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines printed around each change.
const diffContext = 3

// Diff renders a unified-style line diff between the original and the fixed
// content, used to preview the changes -dry-run proposes. It returns "" when
// the two are identical.
func Diff(original, fixed []byte, path string) string {
	a := splitLines(strings.ReplaceAll(string(original), "\r\n", "\n"))
	b := splitLines(strings.ReplaceAll(string(fixed), "\r\n", "\n"))
	hunks := diffHunks(diffOps(a, b))
	if len(hunks) == 0 {
		return ""
	}

	// Trim a leading separator so an absolute path does not read as a//path.
	rel := strings.TrimPrefix(path, "/")

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n+++ b/%s\n", rel, rel)
	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount)
		for _, op := range h.ops {
			sb.WriteString(op.marker)
			sb.WriteString(op.text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// diffOp is one line of a diff: a kept line, a removed line or an added line.
type diffOp struct {
	marker string
	text   string
	oldNo  int
	newNo  int
}

// diffOps compares two line slices with a longest-common-subsequence walk.
func diffOps(a, b []string) []diffOp {
	// lcs[i][j] is the LCS length of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{marker: " ", text: a[i], oldNo: i + 1, newNo: j + 1})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{marker: "-", text: a[i], oldNo: i + 1, newNo: j + 1})
			i++
		default:
			ops = append(ops, diffOp{marker: "+", text: b[j], oldNo: i + 1, newNo: j + 1})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{marker: "-", text: a[i], oldNo: i + 1, newNo: j + 1})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{marker: "+", text: b[j], oldNo: i + 1, newNo: j + 1})
	}
	return ops
}

// diffHunk is a group of diff lines printed as one @@ block.
type diffHunk struct {
	oldStart, oldCount int
	newStart, newCount int
	ops                []diffOp
}

// diffHunks splits the diff into hunks, keeping diffContext unchanged lines
// around every change and dropping hunks that are pure context.
func diffHunks(ops []diffOp) []diffHunk {
	changed := make([]bool, len(ops))
	any := false
	for i, op := range ops {
		if op.marker != " " {
			changed[i] = true
			any = true
		}
	}
	if !any {
		return nil
	}

	keep := make([]bool, len(ops))
	for i := range ops {
		if !changed[i] {
			continue
		}
		lo, hi := i-diffContext, i+diffContext
		if lo < 0 {
			lo = 0
		}
		if hi >= len(ops) {
			hi = len(ops) - 1
		}
		for j := lo; j <= hi; j++ {
			keep[j] = true
		}
	}

	var hunks []diffHunk
	for i := 0; i < len(ops); {
		if !keep[i] {
			i++
			continue
		}
		j := i
		for j < len(ops) && keep[j] {
			j++
		}
		h := diffHunk{ops: ops[i:j]}
		for _, op := range h.ops {
			if op.marker != "+" {
				h.oldCount++
			}
			if op.marker != "-" {
				h.newCount++
			}
		}
		first := h.ops[0]
		h.oldStart = first.oldNo
		h.newStart = first.newNo
		if h.oldCount == 0 {
			h.oldStart = first.oldNo - 1
		}
		if h.newCount == 0 {
			h.newStart = first.newNo - 1
		}
		hunks = append(hunks, h)
		i = j
	}
	return hunks
}
