// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/openchoreo/openchoreo/tools/lint/parser"
)

func projectSource(t *testing.T, uri, text string) renderingSource {
	t.Helper()
	docs, err := parser.ParseYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return renderingSource{URI: uri, Text: text, Doc: docs[0]}
}
func TestRenderingProjectMembershipAndDependencies(t *testing.T) {
	entries := []renderingSource{
		projectSource(t, "app.yaml", clusterValidationComponent),
		projectSource(t, "web.yaml", clusterValidationType),
		projectSource(t, "workload.yaml", clusterValidationWorkload),
		projectSource(t, "other.yaml", strings.ReplaceAll(strings.Replace(clusterValidationComponent, "name: app,", "name: other-app,", 1), "projectName: demo", "projectName: other")),
		projectSource(t, "unused.yaml", strings.Replace(clusterValidationType, "name: web,", "name: unused,", 1)),
		projectSource(t, "wrong-ns.yaml", strings.ReplaceAll(clusterValidationWorkload, "namespace: default", "namespace: other")),
	}
	selected, err := selectRenderingProject(entries, &renderingProjectScope{ProjectName: "demo", Namespace: "default", ActiveURI: "app.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 3 {
		t.Fatalf("selected unrelated sources: %+v", selected)
	}
	for _, source := range selected {
		if source.URI == "other.yaml" || source.URI == "unused.yaml" || source.URI == "wrong-ns.yaml" {
			t.Fatal(source.URI)
		}
	}
	if _, err := selectRenderingProject(entries, &renderingProjectScope{}); err == nil {
		t.Fatal("unknown project fell back to full namespace")
	}
}
func TestRenderingProjectFollowsSharedTraitAndClusterDefinitions(t *testing.T) {
	component := strings.Replace(clusterValidationComponent, "kind: ComponentType, name: deployment/web", "kind: ClusterComponentType, name: deployment/web", 1) + "  traits: [{kind: Trait, name: logging}]\n"
	all := []renderingSource{
		projectSource(t, "app", component),
		projectSource(t, "type", strings.Replace(clusterValidationType, "kind: ComponentType", "kind: ClusterComponentType", 1)),
		projectSource(t, "trait", "apiVersion: openchoreo.dev/v1alpha1\nkind: Trait\nmetadata: {name: logging, namespace: default}\nspec: {}\n"),
		projectSource(t, "unrelated", "apiVersion: openchoreo.dev/v1alpha1\nkind: Trait\nmetadata: {name: unused, namespace: default}\nspec: {}\n"),
	}
	selected, err := selectRenderingProject(all, &renderingProjectScope{ProjectName: "demo", Namespace: "default", ActiveURI: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 3 {
		t.Fatalf("lost shared dependencies or included unused ones: %+v", selected)
	}
}
func TestClusterRenderingOnlyCurrentProject(t *testing.T) {
	s, r := clusterValidationFixture(t)
	r.ProjectName = "demo"
	r.ActiveURI = r.Documents[1].URI
	other := strings.ReplaceAll(strings.Replace(clusterValidationComponent, "name: app,", "name: other-app,", 1), "projectName: demo", "projectName: other")
	r.Documents = append(r.Documents, clusterValidationDocument{URI: "openchoreo://ctx/namespaces/default/component/other-app.yaml", Text: other}, clusterValidationDocument{URI: "openchoreo://ctx/namespaces/default/component-type/unused.yaml", Text: strings.Replace(clusterValidationType, "name: web,", "name: unused,", 1)})
	result := runCluster(t, s, r)
	if result.Files != 3 || strings.Contains(strings.Join(result.Details, "\n"), "other-app") || !strings.Contains(result.Root, "projects/demo") {
		t.Fatalf("project boundary violated: %+v", result)
	}
	if _, present := s.clusterValidation.inputs[r.Documents[3].URI]; present {
		t.Fatal("unrelated project was a validation input")
	}
	// Invalid unsaved identity in another project must not block this project.
	r.Documents[3].Text = strings.Replace(other, "name: other-app,", "name: unrelated-edit,", 1)
	result = runCluster(t, s, r)
	if result.Files != 3 {
		t.Fatal("an unrelated editor affected the selected project")
	}
}
func TestLocalRenderingOnlyCurrentProject(t *testing.T) {
	s, r := clusterValidationFixture(t)
	root := t.TempDir()
	s.workspaceRoots = []string{root}
	// Local resources can omit the default namespace while shared types declare it.
	r.Documents[1].Text = strings.Replace(clusterValidationComponent, ", namespace: default", "", 1)
	files := []string{"type.yaml", "component.yaml", "workload.yaml"}
	for i, input := range r.Documents {
		if err := os.WriteFile(filepath.Join(root, files[i]), []byte(input.Text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	other := strings.ReplaceAll(strings.Replace(clusterValidationComponent, "name: app,", "name: other-app,", 1), "projectName: demo", "projectName: other")
	if err := os.WriteFile(filepath.Join(root, "other.yaml"), []byte(other), 0600); err != nil {
		t.Fatal(err)
	}
	uri := pathToDocumentURI(filepath.Join(root, "component.yaml"))
	result, err := s.ExecuteCommand(nil, &protocol.ExecuteCommandParams{Command: postRenderCommand, Arguments: []any{uri, []Document{}, renderingProjectScope{ProjectName: "demo"}}})
	if err != nil {
		t.Fatal(err)
	}
	summary := result.(postRenderResult)
	if summary.Files != 3 || strings.Contains(strings.Join(summary.Details, "\n"), "other-app") || !strings.Contains(summary.Root, "project demo") {
		t.Fatalf("validated unrelated project: %+v", summary)
	}
}
