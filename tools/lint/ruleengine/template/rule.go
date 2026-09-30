// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"github.com/openchoreo/openchoreo/tools/lint/parser"
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine"
	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/schema"
)

// SchemaRule validates a document against an embedded JSON schema. It is the
// shared implementation behind every kind returned by RulesForKind.
type SchemaRule struct {
	name   string
	prefix string
	schema *schema.FieldSchema
}

func (r *SchemaRule) Name() string { return r.name }

func (r *SchemaRule) Evaluate(doc *parser.DocumentNode) ruleengine.Diagnostics {
	return schema.Validate(doc.Root, r.schema, r.prefix)
}
