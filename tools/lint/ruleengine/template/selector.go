// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine"
)

// RulesForKind returns the validation rules for a given Kubernetes kind name
// in the active template version. If the kind has no specific rules, it
// returns nil.
func RulesForKind(kind string) []ruleengine.Rule {
	_ = load()
	ks, ok := kindIndex[kind]
	if !ok {
		return nil
	}
	return []ruleengine.Rule{&SchemaRule{
		name:   ks.ruleName,
		prefix: ks.prefix,
		schema: schemasByVersion[currentVersion()][kind],
	}}
}

// AllKinds returns the names of every kind that has registered rules. It is
// used by tests and tooling to enumerate the full kind surface.
func AllKinds() []string {
	_ = load()
	out := make([]string, len(allKindNames))
	copy(out, allKindNames)
	return out
}
