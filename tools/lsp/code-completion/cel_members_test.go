// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package codecompletion

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf16"

	protocol "github.com/tliron/glsp/protocol_3_16"

	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
)

const celCompletionHeader = `apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
metadata: {name: service}
spec:
  workloadType: deployment
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        database:
          type: object
          properties:
            host: {type: string, description: Database hostname}
            port: {type: integer}
  resources:
    - id: app
      template:
`

func celCompletion(t *testing.T, source string) (string, []protocol.CompletionItem, bool) {
	t.Helper()
	cursor := strings.Index(source, "¦")
	if cursor < 0 {
		t.Fatal("missing cursor")
	}
	before := source[:cursor]
	line := strings.Count(before, "\n") + 1
	column := lsputil.UTF16Length(before[strings.LastIndex(before, "\n")+1:]) + 1
	text := strings.Replace(source, "¦", "", 1)
	items, handled := celMemberItems(text, line, column)
	return text, items, handled
}
func celLabels(items []protocol.CompletionItem) map[string]protocol.CompletionItem {
	out := map[string]protocol.CompletionItem{}
	for _, i := range items {
		out[i.Label] = i
	}
	return out
}
func TestCELMemberCompletionIncompleteAndQuoted(t *testing.T) {
	for _, value := range []string{`${metadata.¦}`, `${metadata.¦`, `'${metadata.¦}'`, `"${metadata.¦}"`, `'${metadata.¦`, `"${metadata.¦`, `'${metadata.name} ${metadata.¦}'`} {
		t.Run(value, func(t *testing.T) {
			_, items, handled := celCompletion(t, celCompletionHeader+"        value: "+value+"\n")
			labels := celLabels(items)
			if !handled || len(labels) == 0 || labels["componentName"].Label == "" || labels["projectName"].Label == "" {
				t.Fatalf("missing members: handled=%v items=%+v", handled, items)
			}
			if labels["componentName"].Detail == nil || *labels["componentName"].Detail != "string" || labels["componentName"].Documentation == nil {
				t.Fatalf("missing type/description: %+v", labels["componentName"])
			}
		})
	}
}
func TestCELMemberCompletionPrefixesAndEdits(t *testing.T) {
	for _, value := range []string{`'${metadata.comp¦}'`, `'${metadata.comp¦onentName}'`, `"😀 ${metadata.comp¦}"`} {
		text, items, handled := celCompletion(t, celCompletionHeader+"        value: "+value+"\n")
		if !handled || len(items) == 0 {
			t.Fatal(items)
		}
		for _, item := range items {
			if !strings.HasPrefix(item.Label, "comp") {
				t.Fatalf("prefix was not filtered: %s", item.Label)
			}
		}
		item := celLabels(items)["componentName"]
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		if !ok {
			t.Fatalf("missing text edit: %+v", item)
		}
		lines := strings.Split(text, "\n")
		encoded := utf16.Encode([]rune(lines[edit.Range.Start.Line]))
		updated := string(utf16.Decode(append(append(append([]uint16(nil), encoded[:edit.Range.Start.Character]...), utf16.Encode([]rune(edit.NewText))...), encoded[edit.Range.End.Character:]...)))
		if !strings.Contains(updated, "${metadata.componentName}") {
			t.Fatalf("surrounding expression changed: %s", updated)
		}
	}
}
func TestCELNestedSchemaAndContextMembers(t *testing.T) {
	_, items, handled := celCompletion(t, celCompletionHeader+"        value: '${parameters.database.¦}'\n")
	labels := celLabels(items)
	if !handled || len(items) != 2 || labels["host"].Documentation == nil || labels["port"].Label == "" {
		t.Fatalf("nested schema missing: %+v", items)
	}
	workflow := "apiVersion: openchoreo.dev/v1alpha1\nkind: Workflow\nmetadata: {name: build}\nspec:\n  resources:\n    - id: job\n      template:\n        value: '${metadata.¦}'\n"
	_, items, handled = celCompletion(t, workflow)
	labels = celLabels(items)
	if !handled || labels["namespaceName"].Label == "" || labels["componentName"].Label != "" {
		t.Fatalf("workflow context leaked component fields: %+v", items)
	}
	for _, kind := range []string{"Trait", "ClusterTrait", "ClusterComponentType"} {
		source := strings.Replace(celCompletionHeader, "kind: ComponentType", "kind: "+kind, 1) + "        value: '${metadata.¦}'\n"
		if strings.Contains(kind, "Trait") {
			source = strings.Replace(source, "  resources:\n    - id: app\n      template:", "  creates:\n    - template:", 1)
		}
		_, items, handled = celCompletion(t, source)
		if !handled || celLabels(items)["componentName"].Label == "" {
			t.Fatalf("%s did not use context: %+v", kind, items)
		}
	}
}
func TestCELMemberCompletionBlockAndMultiDocument(t *testing.T) {
	source := celCompletionHeader + "        value: |\n          prefix\n          ${metadata.¦}\n"
	_, items, handled := celCompletion(t, source)
	if !handled || celLabels(items)["componentName"].Label == "" {
		t.Fatalf("block scalar missing: %+v", items)
	}
	_, items, handled = celCompletion(t, "apiVersion: v1\nkind: ConfigMap\n---\n"+source)
	if !handled || celLabels(items)["componentName"].Label == "" {
		t.Fatal("wrong YAML document selected")
	}
}
func TestCELMemberCompletionExcludesStringsCommentsAndUnknownKeys(t *testing.T) {
	for _, value := range []string{`'ordinary metadata.¦'`, `'${"metadata.¦"}'`, `'${"""metadata.¦"""}'`, `'${true // metadata.¦}'`, `'${metadata.name} metadata.¦'`} {
		_, _, handled := celCompletion(t, celCompletionHeader+"        value: "+value+"\n")
		if handled {
			t.Fatalf("completed outside CEL code: %s", value)
		}
	}
	for _, path := range []string{"unknown.", "metadata.unknown.", "metadata.labels."} {
		_, items, handled := celCompletion(t, celCompletionHeader+"        value: '${"+path+"¦}'\n")
		if !handled || len(items) != 0 {
			t.Fatalf("invented members for %s: %+v", path, items)
		}
	}
	_, items, _ := celCompletion(t, celCompletionHeader+"        value: \"${\\\"text\\\" + metadata.¦}\"\n")
	if celLabels(items)["componentName"].Label == "" {
		t.Fatal("YAML escaping confused CEL string boundary")
	}
}
func TestCELMemberCompletionDispatcherAndNoMutation(t *testing.T) {
	source := celCompletionHeader + "        value: '${metadata.}'\n"
	lines := strings.Split(source, "\n")
	line := len(lines) - 1
	column := strings.Index(lines[line-1], ".}") + 2
	before := source
	result, err := Handle(source, line, column)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(protocol.CompletionList)
	if !ok || !list.IsIncomplete || celLabels(list.Items)["componentName"].Label == "" {
		t.Fatalf("dispatcher missed CEL: %+v", result)
	}
	if source != before {
		t.Fatal("editor text mutated")
	}
}

func TestCELMembersRespectForEachAndValidationScopes(t *testing.T) {
	source := `apiVersion: openchoreo.dev/v1alpha1
kind: ComponentType
metadata: {name: service}
spec:
  workloadType: deployment
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        workers:
          type: array
          items:
            type: object
            properties:
              image: {type: string}
  resources:
    - id: worker
      forEach: '${parameters.workers}'
      var: worker
      template:
        value: '${worker.¦}'
`
	_, items, handled := celCompletion(t, source)
	if !handled || celLabels(items)["image"].Label == "" || celLabels(items)["componentName"].Label != "" {
		t.Fatalf("forEach scope not respected: %+v", items)
	}
	source = celCompletionHeader + "        value: hello\n  postRenderValidations:\n    - target: {group: apps, version: v1, kind: Deployment}\n      rule: '${resource.metadata.¦}'\n"
	_, items, handled = celCompletion(t, source)
	if !handled || celLabels(items)["name"].Label == "" {
		t.Fatalf("validation resource scope not respected: %+v", items)
	}
	// Unfinished boolean expressions also use their own environment.
	source = celCompletionHeader + "        value: hello\n  preRenderValidations:\n    - rule: '${parameters.database.¦'\n"
	_, items, handled = celCompletion(t, source)
	if !handled || celLabels(items)["host"].Label == "" {
		t.Fatalf("unfinished validation expression not completed: %+v", items)
	}
}

func TestCELMemberConcurrentRequests(t *testing.T) {
	sources := []string{
		celCompletionHeader + "        value: '${metadata.¦}'\n",
		"apiVersion: openchoreo.dev/v1alpha1\nkind: Workflow\nmetadata: {name: build}\nspec:\n  resources:\n    - id: task\n      template:\n        value: '${metadata.¦}'\n",
	}
	type request struct {
		text      string
		line, col int
		field     string
	}
	requests := make([]request, 0, len(sources))
	for i, source := range sources {
		before, _, found := strings.Cut(source, "¦")
		if !found {
			t.Fatal("missing completion cursor")
		}
		field := "componentName"
		if i == 1 {
			field = "workflowRunName"
		}
		requests = append(requests, request{strings.Replace(source, "¦", "", 1), strings.Count(before, "\n") + 1, lsputil.UTF16Length(before[strings.LastIndex(before, "\n")+1:]) + 1, field})
	}
	var wg sync.WaitGroup
	failures := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				r := requests[j%len(requests)]
				items, handled := celMemberItems(r.text, r.line, r.col)
				if !handled || celLabels(items)[r.field].Label == "" {
					failures <- r.field
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for field := range failures {
		t.Errorf("concurrent completion lost %s", field)
	}
}

func TestCELConfigurationFieldAndFunctionCompletions(t *testing.T) {
	_, items, handled := celCompletion(t, celCompletionHeader+"        value: '${configurations.¦}'\n")
	labels := celLabels(items)
	if !handled || labels["configs"].Label == "" || labels["secrets"].Label == "" {
		t.Fatalf("configuration fields missing: %+v", items)
	}
	functions := map[string]string{
		"toConfigEnvsByContainer()": "list(EnvsByContainerEntry)",
		"toConfigFileList()":        "list(ConfigFileListEntry)",
		"toContainerEnvFrom()":      "list(EnvFromEntry)",
		"toContainerVolumeMounts()": "list(VolumeMountEntry)",
		"toSecretEnvsByContainer()": "list(EnvsByContainerEntry)",
		"toSecretFileList()":        "list(SecretFileListEntry)",
		"toVolumes()":               "list(VolumeEntry)",
	}
	for name, returnType := range functions {
		item, found := labels[name]
		if !found || item.Kind == nil || *item.Kind != protocol.CompletionItemKindMethod {
			t.Errorf("missing method %s: %+v", name, item)
			continue
		}
		if item.Detail == nil || *item.Detail != returnType {
			t.Errorf("%s return type: %v, want %s", name, item.Detail, returnType)
		}
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		if !ok || edit.NewText != name || item.Documentation == nil {
			t.Errorf("%s insertion/documentation: %+v", name, item)
		}
	}
	_, metadata, _ := celCompletion(t, celCompletionHeader+"        value: '${metadata.¦}'\n")
	for _, item := range metadata {
		if strings.HasPrefix(item.Label, "toConfig") || item.Label == "toVolumes()" {
			t.Errorf("configuration function leaked to metadata: %s", item.Label)
		}
	}
}

func TestCELFunctionPrefixAndExistingCall(t *testing.T) {
	for _, value := range []string{`'${configurations.toVo¦}'`, `'${configurations.toVo¦lumes()}'`, `'${configurations.toVo¦lumes ()}'`} {
		text, items, handled := celCompletion(t, celCompletionHeader+"        value: "+value+"\n")
		if !handled || len(items) != 1 || items[0].Label != "toVolumes()" {
			t.Fatalf("incorrect function prefix filtering: %+v", items)
		}
		edit := items[0].TextEdit.(*protocol.TextEdit)
		lines := strings.Split(text, "\n")
		line := lines[edit.Range.Start.Line]
		updated := line[:edit.Range.Start.Character] + edit.NewText + line[edit.Range.End.Character:]
		if strings.Count(updated, "(") != 1 || strings.Count(updated, ")") != 1 || !strings.Contains(updated, "configurations.toVolumes") {
			t.Fatalf("call corrupted: %s", updated)
		}
	}
}

func TestCELStringFunctionCompletionUsesArgumentCursor(t *testing.T) {
	_, items, handled := celCompletion(t, celCompletionHeader+"        value: '${metadata.componentName.startsW¦}'\n")
	item := celLabels(items)["startsWith(string)"]
	if !handled || item.Kind == nil || *item.Kind != protocol.CompletionItemKindMethod || item.Detail == nil || *item.Detail != "bool" {
		t.Fatalf("missing typed string function: %+v", items)
	}
	edit, ok := item.TextEdit.(*protocol.TextEdit)
	if !ok || edit.NewText != "startsWith($0)" || item.InsertTextFormat == nil || *item.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Fatalf("argument type inserted as a literal instead of an editable call: %+v", item)
	}
}
