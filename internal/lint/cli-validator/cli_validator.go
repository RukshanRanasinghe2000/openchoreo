// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package clivalidator

import (
	"github.com/openchoreo/openchoreo/internal/lint/parser"
	"github.com/openchoreo/openchoreo/internal/lint/ruleengine"
	"github.com/openchoreo/openchoreo/internal/lint/ruleengine/template"
)

// DocumentResult holds the validation outcome for a single YAML document.
type DocumentResult struct {
	Kind        string
	Diagnostics ruleengine.Diagnostics
}

// ValidateYAML parses the given YAML text and runs the common rules plus the
// kind-specific rules for each document's kind. Text that does not declare an
// OpenChoreo apiVersion is ignored entirely (no results, no documents).
func ValidateYAML(data []byte) ([]DocumentResult, error) {
	if !parser.HasOpenChoreoAPIVersion(data) {
		return nil, nil
	}

	docs, err := parser.ParseYAML(data)
	if err != nil {
		return nil, err
	}

	results := make([]DocumentResult, 0, len(docs))
	for _, doc := range docs {
		e := ruleengine.NewEngine()
		if kindRules := template.RulesForKind(doc.Kind); len(kindRules) > 0 {
			e.AddKindRules(doc.Kind, kindRules)
		} else if inferred := template.InferKind(doc); inferred != "" {
			// Unrecognized kind: still validate deeply against the closest
			// matching schema so all issues surface, not just unknown-kind.
			for _, r := range template.RulesForKind(inferred) {
				e.AddRule(r)
			}
		}

		results = append(results, DocumentResult{
			Kind:        doc.Kind,
			Diagnostics: e.Evaluate(doc),
		})
	}

	return results, nil
}
