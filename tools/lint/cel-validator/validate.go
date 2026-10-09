// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	celast "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"gopkg.in/yaml.v3"
	apiservercel "k8s.io/apiserver/pkg/cel"

	"github.com/openchoreo/openchoreo/internal/template"
	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

// Diagnostic codes reported for CEL expressions. Each is unique across the
// rule engine (see tools/lint/ruleengine for the full catalog).
const (
	// CodeCELParseError reports a ${...} expression that does not parse.
	CodeCELParseError = "cel-parse-error"
	// CodeCELTypeError reports a ${...} expression that is ill-typed, e.g. a
	// select on a field the context does not declare, or a value that forces
	// something that is not boolean/iterable.
	CodeCELTypeError = "cel-type-error"
	// CodeCELUnknownName reports a reference to a name not in scope.
	CodeCELUnknownName = "cel-unknown-name"
	// CodeCELUnknownFunction reports a call with no matching overload.
	CodeCELUnknownFunction = "cel-unknown-function"
)

// Finding is one CEL error resolved back to its location in the document.
type Finding struct {
	Range   parser.Range
	Code    string
	Message string
}

// ValidateDocument type-checks every ${...} expression in a CEL-bearing
// document against the schema-derived environment for its kind, mirroring the
// webhook validation in internal/validation. Kinds that carry no template CEL
// (Project, Component, ComponentRelease, ...) yield no findings.
//
// The reported position is the exact location of the first parser/checker
// issue within the expression; for structural issues (e.g. a boolean field
// that does not return a boolean) it is the start of the enclosing field.
func ValidateDocument(doc *parser.DocumentNode, pair SchemaPair) ([]Finding, error) {
	switch doc.Kind {
	case KindComponentType, KindClusterComponentType:
		return validateComponent(doc, pair)
	case KindTrait, KindClusterTrait:
		return validateTrait(doc, pair)
	case KindResourceType, KindClusterResourceType:
		return validateResourceType(doc, pair)
	case KindWorkflow, KindClusterWorkflow:
		return validateWorkflow(doc, pair)
	default:
		return nil, nil
	}
}

// checker collects findings as the walk descends. The CEL env changes per path
// (forEach loop variables, applied scope, resource binding) and is threaded
// explicitly through the walk so no call site has to restore it.
type checker struct {
	findings []Finding

	// appliedIDs, when non-nil, enables the applied.<id> declaration check
	// (ResourceType/ClusterResourceType only).
	appliedIDs map[string]bool
}

// checkCompile parses and type-checks expr against env, emitting one finding
// on the first issue. It returns the checked AST, or nil when anything failed.
func (c *checker) checkCompile(node *yaml.Node, env *cel.Env, expr, full string) *cel.Ast {
	ast, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		c.addIssues(node, CodeCELParseError, issues, full, expr)
		return nil
	}

	checked, issues := env.Check(ast)
	if issues != nil && issues.Err() != nil {
		c.addIssues(node, classifyCELIssues(issues), issues, full, expr)
		return nil
	}

	if c.appliedIDs != nil {
		c.checkAppliedReferences(checked, node)
	}

	return checked
}

// checkAny validates a general expression (any result type is accepted).
func (c *checker) checkAny(node *yaml.Node, env *cel.Env, expr, full string) {
	c.checkCompile(node, env, expr, full)
}

// checkBoolean validates an expression whose runtime contract is a boolean.
func (c *checker) checkBoolean(node *yaml.Node, env *cel.Env, expr, full string) {
	checked := c.checkCompile(node, env, expr, full)
	if checked == nil {
		return
	}
	outputType := checked.OutputType()
	if !outputType.IsExactType(cel.BoolType) && outputType != cel.DynType {
		c.reportAt(node, nodeStart(node), CodeCELTypeError,
			fmt.Sprintf("expression must return boolean, got %s", outputType))
	}
}

// checkIterable validates an expression whose runtime contract is a list or
// map (forEach sources).
func (c *checker) checkIterable(node *yaml.Node, env *cel.Env, expr, full string) {
	checked := c.checkCompile(node, env, expr, full)
	if checked == nil {
		return
	}
	outputType := checked.OutputType()
	kind := outputType.Kind()
	if kind != types.ListKind && kind != types.MapKind && outputType != cel.DynType {
		c.reportAt(node, nodeStart(node), CodeCELTypeError,
			fmt.Sprintf("forEach expression must return list or map, got %s", outputType))
	}
}

// checkString locates every ${...} expression in a scalar value and validates
// each one. A '}' that immediately abuts the end of an expression marker is
// reported as a stray brace: it is almost always a typo, and without this check
// the render engine would interpolate the marker and silently leak the extra
// '}' into the output as literal text. A `${` that never reaches a balanced
// close (an unclosed marker, or braces that never balance inside the
// expression) is reported too — FindCELExpressions only surfaces balanced
// markers, so an unclosed one would otherwise be silently ignored.
func (c *checker) checkString(node *yaml.Node, env *cel.Env) {
	matches, err := template.FindCELExpressions(node.Value)
	if err != nil {
		c.reportAt(node, nodeStart(node), CodeCELParseError,
			fmt.Sprintf("failed to parse CEL expressions: %v", err))
		return
	}
	offset := 0
	for _, m := range matches {
		idx := strings.Index(node.Value[offset:], m.FullExpr)
		matchStart := offset
		if idx >= 0 {
			matchStart = offset + idx
		}
		if end := matchStart + len(m.FullExpr); end < len(node.Value) && node.Value[end] == '}' {
			c.reportAt(node, positionOf(node, node.Value, common.NewLocation(1, matchStart)),
				CodeCELParseError,
				fmt.Sprintf("stray '}' after CEL expression (did you add an extra brace?)"))
		}
		if idx >= 0 {
			offset = matchStart + len(m.FullExpr)
		}
		c.checkAny(node, env, m.InnerExpr, m.FullExpr)
	}
	if rest := strings.Index(node.Value[offset:], "${"); rest >= 0 {
		c.reportAt(node, positionOf(node, node.Value, common.NewLocation(1, offset+rest)),
			CodeCELParseError,
			"unclosed CEL expression marker (missing closing '}')")
	}
}

// walk recursively validates every scalar in a template body, including
// dynamic (${...}) mapping keys.
func (c *checker) walk(node *yaml.Node, env *cel.Env) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.ScalarNode:
		c.checkString(node, env)
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if strings.Contains(key.Value, "${") {
				c.checkString(key, env)
			}
			c.walk(node.Content[i+1], env)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			c.walk(item, env)
		}
	}
}

// templateBool validates a field whose whole value must be a single
// ${...} boolean expression (includeWhen, readyWhen, rule, when).
func (c *checker) templateBool(node *yaml.Node, env *cel.Env, what string) {
	if node == nil {
		return
	}
	inner, ok := extractTemplate(node.Value)
	if !ok {
		c.reportAt(node, nodeStart(node), CodeCELParseError,
			fmt.Sprintf("%s must be a template expression wrapped in ${...}", what))
		return
	}
	c.checkBoolean(node, env, inner, node.Value)
}

// includeWhen validates an includeWhen field (optional) against the given env.
func (c *checker) includeWhen(node *yaml.Node, env *cel.Env) {
	c.templateBool(node, env, fieldIncludeWhen)
}

// withResource returns an env extended with the `resource` variable. Resource
// bindings are structurally dynamic; callers expect only that the name exists.
func withResource(env *cel.Env) *cel.Env {
	if env == nil {
		return env
	}
	ext, err := env.Extend(cel.Variable("resource", cel.DynType))
	if err != nil {
		return env
	}
	return ext
}

// where validates an (optional) patch-target where filter. Unlike other CEL
// fields, `resource` is in scope and the value may legally appear without
// ${...} wrapping (the webhook accepts both).
func (c *checker) where(node *yaml.Node, env *cel.Env) {
	if node == nil {
		return
	}
	expr := node.Value
	if inner, ok := extractTemplate(node.Value); ok {
		expr = inner
	}
	c.checkBoolean(node, withResource(env), expr, node.Value)
}

// forEachEnv validates a forEach field and, when it can, returns an env that
// binds the loop variable. A nil return means there is no forEach to handle (or
// it could not be extended); the caller keeps the base env.
func (c *checker) forEachEnv(env *cel.Env, provider *apiservercel.DeclTypeProvider, forEach, varNode *yaml.Node) *cel.Env {
	if forEach == nil {
		return nil
	}
	inner, ok := extractTemplate(forEach.Value)
	if !ok {
		c.reportAt(forEach, nodeStart(forEach), CodeCELParseError,
			"forEach must be a template expression wrapped in ${...}")
		return nil
	}
	c.checkIterable(forEach, env, inner, forEach.Value)

	info, err := analyzeForEachExpression(inner, scalarString(varNode), env)
	if err != nil && !strings.Contains(err.Error(), "type check") {
		c.reportAt(forEach, nodeStart(forEach), CodeCELTypeError,
			fmt.Sprintf("failed to analyze forEach: %v", err))
		return nil
	}
	if info == nil {
		return nil
	}

	ext, err := extendEnvWithForEach(env, info, provider)
	if err != nil {
		c.reportAt(forEach, nodeStart(forEach), CodeCELTypeError,
			fmt.Sprintf("failed to extend environment with forEach: %v", err))
		return nil
	}
	return ext
}

// resourceEntry validates one resource template holder (component resources and
// trait creates): includeWhen with the base env before forEach binds the loop
// variable, then the template body under the (possibly extended) env.
func (c *checker) resourceEntry(env *cel.Env, provider *apiservercel.DeclTypeProvider, entry *yaml.Node) {
	c.includeWhen(mappingValue(entry, fieldIncludeWhen), env)
	bodyEnv := env
	if ext := c.forEachEnv(env, provider, mappingValue(entry, fieldForEach), mappingValue(entry, fieldVar)); ext != nil {
		bodyEnv = ext
	}
	c.walk(mappingValue(entry, fieldTemplate), bodyEnv)
}

// validationRules validates spec.validations / spec.preRenderValidations rule
// fields (boolean, base env).
func (c *checker) validationRules(seq *yaml.Node, env *cel.Env) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, entry := range seq.Content {
		c.templateBool(mappingValue(entry, fieldRule), env, fieldRule)
	}
}

// postRenderValidations validates the same fields the webhook applies to a
// post-render validation: when (base env, before forEach), forEach/var,
// target.where and rule (boolean, with `resource` and the loop variable).
func (c *checker) postRenderValidations(seq *yaml.Node, env *cel.Env, provider *apiservercel.DeclTypeProvider) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, entry := range seq.Content {
		c.templateBool(mappingValue(entry, fieldWhen), env, fieldWhen)
		bodyEnv := env
		if ext := c.forEachEnv(env, provider, mappingValue(entry, fieldForEach), mappingValue(entry, fieldVar)); ext != nil {
			bodyEnv = ext
		}
		if target := mappingValue(entry, fieldTarget); target != nil {
			c.where(mappingValue(target, fieldWhere), bodyEnv)
		}
		c.templateBool(mappingValue(entry, fieldRule), bodyEnv, fieldRule)
	}
}

// patch validates one trait patch: forEach/var, target.where, and (for add /
// replace) the operation values, all under the loop-variable-extended env.
func (c *checker) patch(env *cel.Env, provider *apiservercel.DeclTypeProvider, entry *yaml.Node) {
	bodyEnv := env
	if ext := c.forEachEnv(env, provider, mappingValue(entry, fieldForEach), mappingValue(entry, fieldVar)); ext != nil {
		bodyEnv = ext
	}
	if target := mappingValue(entry, fieldTarget); target != nil {
		c.where(mappingValue(target, fieldWhere), bodyEnv)
	}
	c.operations(mappingValue(entry, fieldOperations), bodyEnv)
}

// operations validates add/replace operation values (walked as template
// bodies); remove operations carry no value to validate.
func (c *checker) operations(seq *yaml.Node, env *cel.Env) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, op := range seq.Content {
		switch scalarString(mappingValue(op, fieldOp)) {
		case patchOpAdd, patchOpReplace:
			c.walk(mappingValue(op, fieldValue), env)
		}
	}
}

// validateComponent validates a (Cluster)ComponentType document.
func validateComponent(doc *parser.DocumentNode, pair SchemaPair) ([]Finding, error) {
	env, provider, err := NewComponentEnv(SchemaOptions{
		ParametersSchema:         pair.Parameters,
		EnvironmentConfigsSchema: pair.EnvironmentConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("build CEL env: %w", err)
	}

	c := &checker{}
	spec := specMapping(doc)
	if spec == nil {
		return c.findings, nil
	}

	c.resourceEntries(mappingValue(spec, fieldResources), env, provider)
	c.validationRules(mappingValue(spec, fieldValidations), env)
	c.validationRules(mappingValue(spec, fieldPreRenderValidations), env)
	c.postRenderValidations(mappingValue(spec, fieldPostRenderValidations), env, provider)
	return c.findings, nil
}

// validateTrait validates a (Cluster)Trait document.
func validateTrait(doc *parser.DocumentNode, pair SchemaPair) ([]Finding, error) {
	env, provider, err := NewTraitEnv(SchemaOptions{
		ParametersSchema:         pair.Parameters,
		EnvironmentConfigsSchema: pair.EnvironmentConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("build CEL env: %w", err)
	}

	c := &checker{}
	spec := specMapping(doc)
	if spec == nil {
		return c.findings, nil
	}

	c.resourceEntries(mappingValue(spec, fieldCreates), env, provider)
	c.patchEntries(mappingValue(spec, fieldPatches), env, provider)
	c.patchEntries(mappingValue(spec, fieldRemoves), env, provider)
	c.postRenderValidations(mappingValue(spec, fieldPostRenderValidations), env, provider)
	return c.findings, nil
}

// resourceEntries validates a sequence of template holders (component
// resources / trait creates).
func (c *checker) resourceEntries(seq *yaml.Node, env *cel.Env, provider *apiservercel.DeclTypeProvider) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, entry := range seq.Content {
		c.resourceEntry(env, provider, entry)
	}
}

// patchEntries validates a sequence of trait patches/removes.
func (c *checker) patchEntries(seq *yaml.Node, env *cel.Env, provider *apiservercel.DeclTypeProvider) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, entry := range seq.Content {
		c.patch(env, provider, entry)
	}
}

// validateResourceType validates a (Cluster)ResourceType document: resource
// templates and includeWhen against the base env, readyWhen and outputs
// against the env-with-applied, and applied.<id> references against the
// declared resources[].id set.
func validateResourceType(doc *parser.DocumentNode, pair SchemaPair) ([]Finding, error) {
	base, err := NewResourceEnv(SchemaOptions{
		ParametersSchema:         pair.Parameters,
		EnvironmentConfigsSchema: pair.EnvironmentConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("build CEL env: %w", err)
	}
	applied, err := WithApplied(base)
	if err != nil {
		return nil, fmt.Errorf("extend CEL env with applied: %w", err)
	}

	c := &checker{}
	spec := specMapping(doc)
	if spec == nil {
		return c.findings, nil
	}

	res := mappingValue(spec, fieldResources)
	ids := map[string]bool{}
	if res != nil && res.Kind == yaml.SequenceNode {
		for _, entry := range res.Content {
			if id := scalarString(mappingValue(entry, fieldID)); id != "" {
				ids[id] = true
			}
		}
	}
	c.appliedIDs = ids

	if res != nil && res.Kind == yaml.SequenceNode {
		for _, entry := range res.Content {
			c.includeWhen(mappingValue(entry, fieldIncludeWhen), base)
			c.walk(mappingValue(entry, fieldTemplate), base)
			c.templateBool(mappingValue(entry, fieldReadyWhen), applied, fieldReadyWhen)
		}
	}

	c.outputs(mappingValue(spec, fieldOutputs), applied)
	return c.findings, nil
}

// validateWorkflow validates a Workflow or ClusterWorkflow document:
// spec.runTemplate and spec.resources[].template walk the base env,
// resources[].includeWhen is a boolean expression, and externalRefs[].name
// supports CEL expressions. Workflow resources have no forEach/var, so the
// base env applies throughout. Mirrors
// internal/pipeline/workflow.(*Pipeline).BuildCELContext's declared surface.
func validateWorkflow(doc *parser.DocumentNode, pair SchemaPair) ([]Finding, error) {
	env, err := NewWorkflowEnv(SchemaOptions{ParametersSchema: pair.Parameters})
	if err != nil {
		return nil, fmt.Errorf("build CEL env: %w", err)
	}

	c := &checker{}
	spec := specMapping(doc)
	if spec == nil {
		return c.findings, nil
	}

	c.walk(mappingValue(spec, fieldRunTemplate), env)
	c.workflowResources(mappingValue(spec, fieldResources), env)
	c.externalRefs(mappingValue(spec, fieldExternalRefs), env)
	return c.findings, nil
}

// workflowResources validates a sequence of WorkflowResource holders.
func (c *checker) workflowResources(seq *yaml.Node, env *cel.Env) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, entry := range seq.Content {
		c.includeWhen(mappingValue(entry, fieldIncludeWhen), env)
		c.walk(mappingValue(entry, fieldTemplate), env)
	}
}

// externalRefs validates the CEL-supporting `name` field of each externalRef
// declaration; id/apiVersion/kind are plain strings.
func (c *checker) externalRefs(seq *yaml.Node, env *cel.Env) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, entry := range seq.Content {
		c.walk(mappingValue(entry, fieldName), env)
	}
}

// outputs validates value / secretKeyRef / configMapKeyRef template strings.
func (c *checker) outputs(seq *yaml.Node, env *cel.Env) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, out := range seq.Content {
		if v := mappingValue(out, fieldValue); v != nil {
			c.walk(v, env)
		}
		for _, ref := range []string{fieldSecretKeyRef, fieldConfigMapKeyRef} {
			refNode := mappingValue(out, ref)
			if refNode == nil {
				continue
			}
			c.walk(mappingValue(refNode, fieldName), env)
			c.walk(mappingValue(refNode, fieldKey), env)
		}
	}
}

// checkAppliedReferences walks a checked AST and reports any applied.<id>
// reference whose <id> is not in c.appliedIDs. Both applied.<ident>
// (SelectKind) and applied["<lit>"] (CallKind with the index operator) are
// recognized. Mirrors internal/validation/resource.resourcetype.go.
func (c *checker) checkAppliedReferences(checked *cel.Ast, node *yaml.Node) {
	expr := checked.NativeRep().Expr()
	if expr == nil {
		return
	}

	var undeclared []string
	seen := make(map[string]bool)

	visitor := celast.NewExprVisitor(func(e celast.Expr) {
		var id string

		switch e.Kind() {
		case celast.SelectKind:
			sel := e.AsSelect()
			operand := sel.Operand()
			if operand.Kind() == celast.IdentKind && operand.AsIdent() == "applied" {
				id = sel.FieldName()
			}
		case celast.CallKind:
			call := e.AsCall()
			if call.FunctionName() != operators.Index {
				return
			}
			args := call.Args()
			if len(args) != 2 {
				return
			}
			target := args[0]
			key := args[1]
			if target.Kind() != celast.IdentKind || target.AsIdent() != "applied" {
				return
			}
			if key.Kind() != celast.LiteralKind {
				// applied[someVar] — cannot validate at compile time; skip.
				return
			}
			lit, ok := key.AsLiteral().Value().(string)
			if !ok {
				return
			}
			id = lit
		default:
			return
		}

		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if !c.appliedIDs[id] {
			undeclared = append(undeclared, id)
		}
	})
	celast.PostOrderVisit(expr, visitor)

	if len(undeclared) == 0 {
		return
	}
	sort.Strings(undeclared)
	c.reportAt(node, nodeStart(node), CodeCELUnknownName,
		fmt.Sprintf("applied.<id> references unknown ids %v; declared ids: %v", undeclared, sortKeys(c.appliedIDs)))
}

// addIssues emits a finding for the first issue reported by the CEL parser or
// checker, resolved to its intra-expression location.
func (c *checker) addIssues(node *yaml.Node, code string, issues *cel.Issues, full, expr string) {
	errs := issues.Errors()
	if len(errs) == 0 {
		c.reportAt(node, nodeStart(node), code,
			fmt.Sprintf("invalid CEL expression %q: %s", expr, issues.Err()))
		return
	}
	e := errs[0]
	c.reportAt(node, positionOf(node, full, e.Location), code,
		fmt.Sprintf("invalid CEL expression %q: %s", expr, e.Message))
}

// nodeStart returns the document position of the node's first character.
func nodeStart(node *yaml.Node) parser.Position {
	return parser.Position{Line: node.Line, Column: node.Column}
}

// positionOf maps a CEL error location (1-based line, 0-based column within
// the expression text) back to absolute document coordinates. full is the
// ${...} marker as it appears in node.Value, so the column offset is exact for
// single-line expressions; continuation lines of a block scalar are relative
// to their own line start.
func positionOf(node *yaml.Node, full string, loc common.Location) parser.Position {
	offset := strings.Index(node.Value, full)
	if offset < 0 {
		offset = 0
	}
	if loc.Line() > 1 {
		return parser.Position{Line: node.Line + loc.Line() - 1, Column: loc.Column() + 1}
	}
	return parser.Position{Line: node.Line + loc.Line() - 1, Column: node.Column + offset + loc.Column()}
}

// classifyCELIssues maps the first checker message to a diagnostic code. The
// distinction between unknown names and unknown functions is picked out of the
// message because cel-go exposes no stable error code; everything else falls
// back to a plain type error.
func classifyCELIssues(issues *cel.Issues) string {
	var msgs []string
	for _, e := range issues.Errors() {
		msgs = append(msgs, e.Message)
	}
	joined := strings.Join(msgs, "\n")
	switch {
	case strings.Contains(joined, "undeclared reference"):
		return CodeCELUnknownName
	case strings.Contains(joined, "no matching overload"), strings.Contains(joined, "no such overload"):
		return CodeCELUnknownFunction
	default:
		return CodeCELTypeError
	}
}

func (c *checker) reportAt(node *yaml.Node, pos parser.Position, code, message string) {
	c.findings = append(c.findings, Finding{
		Range:   parser.Range{Start: pos, End: pos},
		Code:    code,
		Message: message,
	})
}

// --- yaml helpers ---------------------------------------------------------

func specMapping(doc *parser.DocumentNode) *yaml.Node {
	if doc == nil || doc.Spec == nil || doc.Spec.Kind != yaml.MappingNode {
		return nil
	}
	return doc.Spec
}

// scalarString returns the value of a scalar node, or "".
func scalarString(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// extractTemplate strips a single ${...} wrapper, matching the webhook's
// extractCELFromTemplate semantics.
func extractTemplate(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "${") || !strings.HasSuffix(trimmed, "}") {
		return "", false
	}
	return strings.TrimSpace(trimmed[2 : len(trimmed)-1]), true
}

func sortKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
