// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import "github.com/openchoreo/openchoreo/internal/lint/parser"

// Severity represents the severity level of a diagnostic.
type Severity int

const (
	SeverityError   Severity = 1
	SeverityWarning Severity = 2
	SeverityInfo    Severity = 3
	SeverityHint    Severity = 4
)

// Diagnostic represents a single validation issue found by the rule engine.
type Diagnostic struct {
	Range    parser.Range
	Severity Severity
	Code     string
	Message  string
}

// Diagnostics is a convenience type for a slice of Diagnostic.
type Diagnostics []Diagnostic

// Errors returns only diagnostics with SeverityError.
func (d Diagnostics) Errors() Diagnostics {
	var out Diagnostics
	for _, diag := range d {
		if diag.Severity == SeverityError {
			out = append(out, diag)
		}
	}
	return out
}

// Warnings returns only diagnostics with SeverityWarning.
func (d Diagnostics) Warnings() Diagnostics {
	var out Diagnostics
	for _, diag := range d {
		if diag.Severity == SeverityWarning {
			out = append(out, diag)
		}
	}
	return out
}

// HasErrors returns true if any diagnostic is an error.
func (d Diagnostics) HasErrors() bool {
	return len(d.Errors()) > 0
}

// Append adds another diagnostics slice to this one.
func (d *Diagnostics) Append(other Diagnostics) {
	*d = append(*d, other...)
}
