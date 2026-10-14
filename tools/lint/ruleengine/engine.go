// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import "github.com/openchoreo/openchoreo/tools/lint/parser"

// Rule defines a single validation rule that can evaluate a DocumentNode.
type Rule interface {
	Name() string
	Evaluate(doc *parser.DocumentNode) Diagnostics
}

// Engine runs all registered rules against a DocumentNode.
type Engine struct {
	rules     []Rule
	kindRules map[string][]Rule
}

// NewEngine creates an Engine with all default rules registered.
func NewEngine() *Engine {
	e := &Engine{kindRules: make(map[string][]Rule)}
	e.rules = append(e.rules, &NamingRules{})
	e.rules = append(e.rules, &RequiredFieldsRules{})
	e.rules = append(e.rules, &EnumRules{})
	e.rules = append(e.rules, &ReferenceRules{})
	e.rules = append(e.rules, &MetadataRules{})
	e.rules = append(e.rules, &EmptyLinesRules{})
	return e
}

// AddRule registers an additional rule with the engine.
func (e *Engine) AddRule(r Rule) {
	e.rules = append(e.rules, r)
}

// AddKindRules registers kind-specific rules for the given kind.
// These rules run after the common rules when doc.Kind matches.
func (e *Engine) AddKindRules(kind string, rules []Rule) {
	e.kindRules[kind] = append(e.kindRules[kind], rules...)
}

// Evaluate runs all rules against a single DocumentNode and returns
// the combined diagnostics. Common rules run first, then kind-specific
// rules are looked up via doc.Kind.
func (e *Engine) Evaluate(doc *parser.DocumentNode) Diagnostics {
	var all Diagnostics

	// Run common rules.
	for _, rule := range e.rules {
		all.Append(rule.Evaluate(doc))
	}

	// Run kind-specific rules.
	if kindRules, ok := e.kindRules[doc.Kind]; ok {
		for _, rule := range kindRules {
			all.Append(rule.Evaluate(doc))
		}
	}

	return all
}

// EvaluateAll runs all rules against multiple DocumentNodes.
func (e *Engine) EvaluateAll(docs []*parser.DocumentNode) Diagnostics {
	var all Diagnostics
	for _, doc := range docs {
		all.Append(e.Evaluate(doc))
	}
	return all
}
