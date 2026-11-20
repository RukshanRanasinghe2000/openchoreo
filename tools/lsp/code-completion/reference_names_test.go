// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
package codecompletion

import (
	"strings"
	"testing"
	"unicode/utf16"

	lsputil "github.com/openchoreo/openchoreo/tools/lsp/lsp-util"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

type referenceTestIndex map[string][]string

func (r referenceTestIndex) NamesByKind() map[string][]string { return r }

const referenceHeader = "apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata: {name: service}\nspec:\n  allowedTraits:\n"

func referenceCompletions(t *testing.T, source string, idx ResourceIndexer) (string, []protocol.CompletionItem) {
	t.Helper()
	offset := strings.Index(source, "¦")
	if offset < 0 {
		t.Fatal("missing cursor")
	}
	before := source[:offset]
	line := strings.Count(before, "\n") + 1
	column := lsputil.UTF16Length(before[strings.LastIndex(before, "\n")+1:]) + 1
	text := strings.Replace(source, "¦", "", 1)
	result, err := HandleWithIndex(text, line, column, idx, "file:///workspace/type.yaml")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(protocol.CompletionList)
	if !ok {
		t.Fatalf("unexpected completion result: %T", result)
	}
	var items []protocol.CompletionItem
	for _, item := range list.Items {
		if data, ok := item.Data.(map[string]any); ok && data["openchoreoResourceReference"] == true {
			items = append(items, item)
		}
	}
	return text, items
}
func applyReferenceItem(t *testing.T, text string, item protocol.CompletionItem) string {
	t.Helper()
	edit, ok := item.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("missing reference text edit: %+v", item)
	}
	lines := strings.Split(text, "\n")
	encoded := utf16.Encode([]rune(lines[edit.Range.Start.Line]))
	line := append(append(append([]uint16(nil), encoded[:edit.Range.Start.Character]...), utf16.Encode([]rune(edit.NewText))...), encoded[edit.Range.End.Character:]...)
	lines[edit.Range.Start.Line] = string(utf16.Decode(line))
	return strings.Join(lines, "\n")
}
func TestReferenceMappingAndKinds(t *testing.T) {
	idx := referenceTestIndex{"ClusterTrait": {"health-probes", "health-probes", "logging"}, "Trait": {"namespaced-trait"}, "ComponentType": {"unrelated"}}
	for _, body := range []string{
		"    - kind: ClusterTrait\n      name:¦\n",
		"    - kind: ClusterTrait\n      name: ¦\n",
		"    - name: ¦\n      kind: ClusterTrait\n",
		"    - kind: Trait\n      name: namespaced-trait\n    - kind: ClusterTrait\n      name: hea¦\n",
		"    - kind: 'ClusterTrait' # comment\n      name: ¦\n",
	} {
		_, items := referenceCompletions(t, referenceHeader+body, idx)
		if len(items) != 2 || celLabels(items)["health-probes"].Label == "" || celLabels(items)["logging"].Label == "" {
			t.Fatalf("mapping/kind/dedup failed:\n%s\n%+v", body, items)
		}
		for _, item := range items {
			if item.FilterText == nil || *item.FilterText != item.Label || item.Kind == nil || *item.Kind != protocol.CompletionItemKindReference {
				t.Fatalf("normal filtering metadata missing: %+v", item)
			}
		}
	}
}
func TestReferenceExcludesMissingKindsAndOrdinaryFields(t *testing.T) {
	idx := referenceTestIndex{"ClusterTrait": {"health-probes"}, "ComponentType": {"unrelated"}}
	for _, source := range []string{
		referenceHeader + "    - name: ¦\n",
		referenceHeader + "    - kind: Trait\n      name: ¦\n",
		referenceHeader + "    - kind: UnknownKind\n      name: ¦\n",
		referenceHeader + "    - kind: ComponentType\n      name: ¦\n",
		referenceHeader + "    - kind: ClusterTrait\n      name: health-probes\n    - name: ¦\n",
		referenceHeader + "    - name: ¦\n    - kind: ClusterTrait\n      name: health-probes\n",
		referenceHeader + "    - kind: ClusterTrait\n      # name: ¦\n",
		referenceHeader + "    - kind: ClusterTrait\n      name: health-probes # ¦\n",
		"apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nmetadata:\n  name: ¦\nspec: {}\n",
		"apiVersion: openchoreo.dev/v1alpha1\nkind: ComponentType\nspec:\n  parameters:\n    kind: ClusterTrait\n    name: ¦\n",
		strings.Replace(referenceHeader, "openchoreo.dev/v1alpha1", "other.dev/v1", 1) + "    - kind: ClusterTrait\n      name: ¦\n",
	} {
		_, items := referenceCompletions(t, source, idx)
		if len(items) != 0 {
			t.Fatalf("unrelated candidates:\n%s\n%+v", source, items)
		}
	}
	for _, idx := range []ResourceIndexer{nil, referenceTestIndex{}} {
		_, items := referenceCompletions(t, referenceHeader+"    - kind: ClusterTrait\n      name: ¦\n", idx)
		if len(items) != 0 {
			t.Fatal(items)
		}
	}
}
func TestReferenceValueFormatting(t *testing.T) {
	idx := referenceTestIndex{"ClusterTrait": {"health-probes"}}
	for _, tt := range []struct{ value, want string }{
		{"name:¦", "name: health-probes"},
		{"name: ¦", "name: health-probes"},
		{"name:hea¦", "name: health-probes"},
		{"name: hea¦lth # keep 😀", "name: health-probes # keep 😀"},
		{"name: 'hea¦lth' # keep", "name: 'health-probes' # keep"},
		{"name: \"😀hea¦lth\" # keep", "name: \"health-probes\" # keep"},
		{"name: 'hea¦", "name: 'health-probes'"},
		{"name:\"hea¦\"", "name: \"health-probes\""},
		{"name: ¦# keep", "name: health-probes # keep"},
		{"name:¦   # keep", "name: health-probes # keep"},
	} {
		source := referenceHeader + "    - kind: ClusterTrait\n      " + tt.value + "\n"
		text, items := referenceCompletions(t, source, idx)
		if len(items) != 1 {
			t.Fatalf("%s: %+v", tt.value, items)
		}
		got := applyReferenceItem(t, text, items[0])
		want := referenceHeader + "    - kind: ClusterTrait\n      " + tt.want + "\n"
		if got != want {
			t.Errorf("edit for %s:\ngot: %s\nwant: %s", tt.value, got, want)
		}
		var parsed any
		if err := yaml.Unmarshal([]byte(got), &parsed); err != nil {
			t.Errorf("invalid resulting YAML: %v", err)
		}
	}
}
func TestReferenceMultiDocumentAndIncompleteTail(t *testing.T) {
	idx := referenceTestIndex{"ClusterTrait": {"health-probes"}, "Trait": {"other"}}
	for _, source := range []string{
		"apiVersion: v1\nkind: ConfigMap\n---\n" + referenceHeader + "    - kind: ClusterTrait\n      name: ¦\n",
		"broken: [\n---\n" + referenceHeader + "    - name: ¦\n      kind: ClusterTrait\n---\nnext: [\n",
		referenceHeader + "    - kind: ClusterTrait\n      name: ¦\n  unfinished: [\n",
	} {
		_, items := referenceCompletions(t, source, idx)
		if len(items) != 1 || items[0].Label != "health-probes" {
			t.Fatalf("active document/incomplete input: %s\n%+v", source, items)
		}
	}
}
