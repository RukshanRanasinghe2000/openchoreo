// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"regexp"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

// namePattern validates Kubernetes/OpenChoreo resource names (RFC 1123 labels).
// Lowercase alphanumeric, hyphens, max 63 chars, must start/end with alphanumeric.
var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

// NamingRules validates resource naming conventions.
type NamingRules struct{}

func (r *NamingRules) Name() string { return "naming" }

func (r *NamingRules) Evaluate(doc *parser.DocumentNode) Diagnostics {
	var diags Diagnostics

	if doc.Metadata.Name == "" {
		return diags
	}

	name := doc.Metadata.Name

	nameField := doc.GetField("metadata")
	if nameField == nil {
		return diags
	}

	if len(name) > 63 {
		diags = append(diags, Diagnostic{
			Range:    nodeRange(nameField.ValueNode),
			Severity: SeverityError,
			Code:     "name-too-long",
			Message:  fmt.Sprintf("metadata.name must be 63 characters or fewer, got %d", len(name)),
		})
	}

	if !namePattern.MatchString(name) {
		diags = append(diags, Diagnostic{
			Range:    nodeRange(nameField.ValueNode),
			Severity: SeverityError,
			Code:     "name-invalid",
			Message:  fmt.Sprintf("metadata.name %q must consist of lowercase alphanumeric characters or '-', and must start and end with an alphanumeric character", name),
		})
	}

	return diags
}
