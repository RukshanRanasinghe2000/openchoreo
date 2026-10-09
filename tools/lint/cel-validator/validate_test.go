// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"slices"
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

// parseDoc parses a single-document YAML body into a DocumentNode.
func parseDoc(t *testing.T, body string) *parser.DocumentNode {
	t.Helper()
	docs, err := parser.ParseYAML([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	return docs[0]
}

// validateParses extracts schemas and runs ValidateDocument.
func validateParses(t *testing.T, body string) []Finding {
	t.Helper()
	doc := parseDoc(t, body)
	pair, err := ExtractSchemas(doc.Spec)
	if err != nil {
		t.Fatalf("ExtractSchemas: %v", err)
	}
	findings, err := ValidateDocument(doc, pair)
	if err != nil {
		t.Fatalf("ValidateDocument: %v", err)
	}
	return findings
}

func codes(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func hasCode(fs []Finding, code string) bool {
	return slices.Contains(codes(fs), code)
}

const cleanComponent = `
apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
spec:
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        config:
          type: array
          items:
            type: object
            properties:
              name:
                type: string
          x-kubernetes-preserve-unknown-fields: true
        weight:
          type: integer
  environmentConfigs:
    openAPIV3Schema:
      type: object
      properties:
        size:
          type: string
  resources:
    - id: in-memory-config
      forEach: ${configurations.toConfigFileList()}
      var: entry
      includeWhen: ${environmentConfigs.size != ""}
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${entry.name}
          namespace: ${metadata.namespace}
        data:
          weight: ${parameters.weight}
`

func TestValidateDocumentComponentClean(t *testing.T) {
	findings := validateParses(t, cleanComponent)
	if len(findings) != 0 {
		t.Fatalf("clean component should produce no findings, got %+v", findings)
	}
}

func TestValidateDocumentComponentUnknownField(t *testing.T) {
	body := strings.Replace(cleanComponent, "{entry.name}", "{entry.nmae}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("entry.nmae should be a finding")
	}
	if !slices.Contains([]string{CodeCELTypeError, CodeCELUnknownName}, findings[0].Code) {
		t.Fatalf("unexpected code %s: %s", findings[0].Code, findings[0].Message)
	}
}

const cleanTrait = `
apiVersion: openchoreo.dev/v1alpha1
kind: Trait
spec:
  patches:
    - forEach: ${configurations.toConfigFileList()}
      var: entry
      target:
        version: v1
        kind: Deployment
        where: ${resource.kind == "Deployment"}
      operations:
        - op: add
          path: /metadata/annotations/key
          value:
            value: ${entry.name}
  removes:
    - target:
        version: v1
        kind: ConfigMap
        where: ${metadata.namespace == "default"}
`

func TestValidateDocumentTraitClean(t *testing.T) {
	findings := validateParses(t, cleanTrait)
	if len(findings) != 0 {
		t.Fatalf("clean trait should produce no findings, got %+v", findings)
	}
}

func TestValidateDocumentTraitUnknownName(t *testing.T) {
	body := strings.Replace(cleanTrait, "${resource.kind == \"Deployment\"}", "${resourceFoo.kind == \"Deployment\"}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("reference to unknown name resourceFoo should be a finding")
	}
	if findings[0].Code != CodeCELUnknownName {
		t.Fatalf("expected %s, got %s: %s", CodeCELUnknownName, findings[0].Code, findings[0].Message)
	}
}

func TestValidateDocumentParseError(t *testing.T) {
	body := strings.Replace(cleanComponent, "${entry.name}", "${([]}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("unparseable expression should be a finding")
	}
	if !hasCode(findings, CodeCELParseError) {
		t.Fatalf("expected a %s finding, got %+v", CodeCELParseError, findings)
	}
}

func TestValidateDocumentBooleanFieldType(t *testing.T) {
	// includeWhen must be boolean; metadata.namespace is a string.
	body := strings.Replace(cleanComponent, "${environmentConfigs.size != \"\"}", "${metadata.namespace}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("non-boolean includeWhen should be a finding")
	}
	if findings[0].Code != CodeCELTypeError {
		t.Fatalf("expected %s, got %s: %s", CodeCELTypeError, findings[0].Code, findings[0].Message)
	}
}

func TestValidateDocumentForEachNonIterable(t *testing.T) {
	// forEach must return a list/map; metadata.namespace is a string.
	body := strings.Replace(cleanComponent, "forEach: ${configurations.toConfigFileList()}", "forEach: ${metadata.namespace}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("non-iterable forEach should be a finding")
	}
	if !slices.Contains(codes(findings), CodeCELTypeError) {
		t.Fatalf("expected a %s finding, got %+v", CodeCELTypeError, findings)
	}
}

const cleanResource = `
apiVersion: openchoreo.dev/v1alpha1
kind: ResourceType
spec:
  environmentConfigs:
    openAPIV3Schema:
      type: object
      properties:
        memory:
          type: string
  resources:
    - id: deployment
      readyWhen: ${applied.deployment.status.readyReplicas > 1}
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: ${metadata.name}
  outputs:
    - name: host
      value: ${metadata.namespace}.svc.cluster.local
`

func TestValidateDocumentResourceClean(t *testing.T) {
	findings := validateParses(t, cleanResource)
	if len(findings) != 0 {
		t.Fatalf("clean resource should produce no findings, got %+v", findings)
	}
}

func TestValidateDocumentResourceUnknownAppliedID(t *testing.T) {
	body := strings.Replace(cleanResource, "applied.deployment.status.readyReplicas", "applied.deploymnt.status.readyReplicas", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("applied.<id> typo should be a finding")
	}
	if !slices.Contains(codes(findings), CodeCELUnknownName) {
		t.Fatalf("expected a %s finding, got %+v", CodeCELUnknownName, findings)
	}
}

func TestValidateDocumentResourceTemplateRejectsApplied(t *testing.T) {
	// applied.* is not in scope inside the template body.
	body := strings.Replace(cleanResource,
		"name: ${metadata.name}",
		"name: ${applied.deployment.metadata.name}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("applied.* in a template body should be rejected")
	}
}

func TestValidateDocumentNotCELKind(t *testing.T) {
	body := `
apiVersion: openchoreo.dev/v1alpha1
kind: Component
spec:
  components:
    - name: foo
`
	doc := parseDoc(t, body)
	pair, err := ExtractSchemas(doc.Spec)
	if err != nil {
		t.Fatalf("ExtractSchemas: %v", err)
	}
	findings, err := ValidateDocument(doc, pair)
	if err != nil {
		t.Fatalf("ValidateDocument: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("non-CEL kind should produce no findings, got %+v", findings)
	}
}

const cleanWorkflow = `
apiVersion: openchoreo.dev/v1alpha1
kind: Workflow
spec:
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        componentName:
          type: string
        repository:
          type: object
          properties:
            url:
              type: string
  runTemplate:
    apiVersion: argoproj.io/v1alpha1
    kind: Workflow
    metadata:
      name: ${metadata.workflowRunName}
      namespace: ${metadata.namespace}
    spec:
      arguments:
        parameters:
          - name: component-name
            value: ${parameters.componentName}
          - name: repo-url
            value: ${parameters.repository.url}
          - name: store
            value: ${workflowplane.secretStore}
          - name: ref
            value: ${externalRefs['git-secret'].spec.type}
  resources:
    - id: source-git-secret
      includeWhen: ${parameters.componentName != ""}
      template:
        apiVersion: v1
        kind: Secret
        metadata:
          name: ${metadata.namespaceName}-secret
          labels:
            run: ${metadata.labels['run']}
  externalRefs:
    - id: git-secret
      apiVersion: v1alpha1
      kind: SecretReference
      name: ${parameters.componentName}-ref
`

func TestValidateDocumentWorkflowClean(t *testing.T) {
	findings := validateParses(t, cleanWorkflow)
	if len(findings) != 0 {
		t.Fatalf("clean workflow should produce no findings, got %+v", findings)
	}
}

func TestValidateDocumentWorkflowClusterWorks(t *testing.T) {
	body := strings.Replace(cleanWorkflow, "kind: Workflow", "kind: ClusterWorkflow", 1)
	findings := validateParses(t, body)
	if len(findings) != 0 {
		t.Fatalf("clean ClusterWorkflow should produce no findings, got %+v", findings)
	}
}

func TestValidateDocumentWorkflowUnknownMetadataField(t *testing.T) {
	body := strings.Replace(cleanWorkflow, "${metadata.workflowRunName}", "${metadata.noSuchField}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("metadata.<field> typo should be a finding")
	}
	if !slices.Contains(codes(findings), CodeCELTypeError) {
		t.Fatalf("expected a %s finding, got %+v", CodeCELTypeError, findings)
	}
}

func TestValidateDocumentWorkflowUnknownParameter(t *testing.T) {
	body := strings.Replace(cleanWorkflow, "${parameters.componentName}", "${parameters.definitelyTypo}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("parameters.<field> typo should be a finding")
	}
	if !slices.Contains(codes(findings), CodeCELTypeError) {
		t.Fatalf("expected a %s finding, got %+v", CodeCELTypeError, findings)
	}
}

func TestValidateDocumentWorkflowIncludeWhenNonBoolean(t *testing.T) {
	body := strings.Replace(cleanWorkflow, `${parameters.componentName != ""}`, "${parameters.componentName}", 1)
	findings := validateParses(t, body)
	if !slices.Contains(codes(findings), CodeCELTypeError) {
		t.Fatalf("expected a %s finding for a non-boolean includeWhen, got %+v", CodeCELTypeError, findings)
	}
}

func TestValidateDocumentWorkflowRunTemplateBaseEnvOnly(t *testing.T) {
	// Component receiver macros are not in scope for workflow templates.
	body := strings.Replace(cleanWorkflow, "${workflowplane.secretStore}", "${configurations.toConfigFileList()}", 1)
	findings := validateParses(t, body)
	if len(findings) == 0 {
		t.Fatal("component receiver macros must not resolve inside workflow templates")
	}
}

func TestValidateDocumentStrayClosingBrace(t *testing.T) {
	body := strings.Replace(cleanWorkflow, "${metadata.namespace}", "${metadata.namespace}}", 1)
	findings := validateParses(t, body)
	if !slices.Contains(codes(findings), CodeCELParseError) {
		t.Fatalf("expected a %s finding for a stray closing brace, got %+v", CodeCELParseError, findings)
	}
	for _, f := range findings {
		if f.Code == CodeCELParseError && !strings.Contains(f.Message, "stray '}'") {
			t.Fatalf("unexpected parse error %q", f.Message)
		}
	}
}

func TestValidateDocumentCELSyntaxErrors(t *testing.T) {
	// Every malformed marker shape must surface as a parse error rather than
	// being silently ignored by the balanced-marker scanner or CEL parser.
	cases := map[string]string{
		"unclosed marker":        "${metadata.namespace",
		"unbalanced braces":      "${merge({a:1}",
		"trailing unclosed":      "${metadata.namespace}${",
		"empty expression":       "${}",
		"double separator":       "${metadata..namespace}",
		"trailing separator":     "${metadata.namespace.}",
		"bad grammar":            "${fn(}",
		"missing paren":          "${merge({a:1} with no close",
		"stray brace after expr": "${metadata.namespace}}",
	}
	for name, marker := range cases {
		t.Run(name, func(t *testing.T) {
			body := strings.Replace(cleanWorkflow, "${metadata.namespace}", marker, 1)
			findings := validateParses(t, body)
			if !slices.Contains(codes(findings), CodeCELParseError) {
				t.Fatalf("expected a %s finding for %q, got %+v", CodeCELParseError, marker, findings)
			}
		})
	}
}
