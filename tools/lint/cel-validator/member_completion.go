// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"context"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"gopkg.in/yaml.v3"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

type memberInspection struct{ marker, receiver string }

// InspectExpressionMembers uses the same environment walk as validation and
// hover. marker identifies a scalar in a request-local document copy, allowing
// incomplete expressions and repeated receiver paths without evaluating CEL.
func InspectExpressionMembers(doc *parser.DocumentNode, pair SchemaPair, marker, receiver string) ([]ExpressionChild, error) {
	if doc == nil || marker == "" || receiver == "" {
		return nil, nil
	}
	copyDoc := *doc
	base := doc.ValidationContext
	if base == nil {
		base = context.Background()
	}
	query := &inspection{schemas: pair, members: &memberInspection{marker: marker, receiver: receiver}}
	copyDoc.ValidationContext = context.WithValue(base, inspectionKey{}, query)
	_, err := ValidateDocument(&copyDoc, pair)
	if query.result == nil {
		return nil, err
	}
	return append(query.result.Children, query.result.Functions...), err
}

func (c *checker) inspectMembers(node *yaml.Node, env *cel.Env) {
	q := c.inspection
	if q == nil || q.members == nil || q.result != nil || node == nil || !strings.Contains(node.Value, q.members.marker) {
		return
	}
	path := strings.Split(q.members.receiver, ".")
	typ, _ := pathType(env, path)
	// Unknown/dynamic receivers cannot establish membership. Non-object types
	// may still expose functions, but never acquire guessed fields or map keys.
	if typ == nil || typ.Kind() == types.DynKind {
		return
	}
	info := &ExpressionInfo{Name: q.members.receiver, Type: typ.String(), ChildrenParent: q.members.receiver}
	info.Functions = receiverFunctions(env, typ, q.members.receiver)
	for _, name := range structFieldNames(env, typ) {
		field, ok := env.CELTypeProvider().FindStructFieldType(typ.TypeName(), name)
		if !ok {
			continue
		}
		child := append(append([]string(nil), path...), name)
		info.Children = append(info.Children, ExpressionChild{Name: strings.Join(child, "."), Type: field.Type.String(), Description: c.symbolDescription(child, typ.TypeName())})
	}
	q.result = info
}
