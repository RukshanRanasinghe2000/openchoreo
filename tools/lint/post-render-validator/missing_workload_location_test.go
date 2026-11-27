// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package postrendervalidator

import (
	"context"
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

func TestMissingWorkloadPointsToComponentName(t *testing.T) {
	text := `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: backend
  namespace: default
spec:
  owner: {projectName: test-1}
  componentType: {kind: ComponentType, name: deployment/service}
---
apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
metadata: {name: service, namespace: default}
spec: {workloadType: deployment, resources: []}
`
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	sources := []SourceDocument{{Path: "component.yaml", Document: docs[0]}, {Path: "type.yaml", Document: docs[1]}}
	findings := ValidateWorkspace(context.Background(), sources)
	for _, f := range findings {
		if strings.Contains(f.Message, "no Workload for Component test-1/backend") {
			if f.Path != "component.yaml" || f.Line != 4 || f.Column != 9 || f.FixLine != 4 {
				t.Fatalf("incorrect location: %+v", f)
			}
			return
		}
	}
	t.Fatalf("missing expected finding: %+v", findings)
}
