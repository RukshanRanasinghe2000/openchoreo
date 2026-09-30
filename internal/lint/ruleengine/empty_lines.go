// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"

	"github.com/openchoreo/openchoreo/internal/lint/parser"
)

type EmptyLinesRules struct{}

func (r *EmptyLinesRules) Name() string { return "empty-lines" }

func (r *EmptyLinesRules) Evaluate(doc *parser.DocumentNode) Diagnostics {
	if len(doc.Source) == 0 {
		return nil
	}

	lines := strings.Split(string(doc.Source), "\n")
	var diags Diagnostics

	for i := 1; i < len(lines); i++ {
		if isBlank(lines[i]) && isBlank(lines[i-1]) {
			diags = append(diags, Diagnostic{
				Range: parser.Range{
					Start: parser.Position{Line: i + 1, Column: 1},
					End:   parser.Position{Line: i + 1, Column: 1},
				},
				Severity: SeverityWarning,
				Code:     "consecutive-blank-lines",
				Message:  "consecutive blank lines",
			})
		}
	}

	return diags
}

func isBlank(line string) bool {
	return strings.TrimSpace(line) == ""
}
