// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package fixer_test

import (
	"strings"
	"testing"

	"github.com/openchoreo/openchoreo/internal/lint/fixer"
)

// TestFormat_StripsTrailingWhitespace verifies spaces and tabs at the end of a
// line are removed, including on comment and empty lines.
func TestFormat_StripsTrailingWhitespace(t *testing.T) {
	src := "apiVersion: openchoreo.dev/v1alpha1   \nkind: Project\t\n# a comment  \n"
	want := "apiVersion: openchoreo.dev/v1alpha1\nkind: Project\n# a comment\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormat_EnsuresSingleFinalNewline verifies the file ends with exactly one
// newline and no trailing blank lines.
func TestFormat_EnsuresSingleFinalNewline(t *testing.T) {
	cases := map[string]string{
		"a: 1":               "a: 1\n",
		"a: 1\n":             "a: 1\n",
		"a: 1\n\n\n":         "a: 1\n",
		"a: 1\n\n\n  \n":     "a: 1\n",
		"a: 1\r\n":           "a: 1\n",
		"a: 1\nb: 2":         "a: 1\nb: 2\n",
		"a: 1\n\nb: 2\n\n\n": "a: 1\n\nb: 2\n",
	}
	for src, want := range cases {
		if got := string(fixer.FormatYAML([]byte(src))); got != want {
			t.Errorf("format(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestFormat_CollapsesBlankLineRuns verifies runs of blank lines shrink to one
// blank line and single blank lines are kept.
func TestFormat_CollapsesBlankLineRuns(t *testing.T) {
	src := "a: 1\n\n\n\nb: 2\n\nc: 3\n"
	want := "a: 1\n\nb: 2\n\nc: 3\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormat_LeavesBlockScalarContent verifies trailing spaces and blank lines
// inside a literal block scalar are preserved, because they are part of the
// value.
func TestFormat_LeavesBlockScalarContent(t *testing.T) {
	src := "script: |\n  echo one   \n  echo two\n\n\nafter: 1\n"
	want := "script: |\n  echo one   \n  echo two\n\n\nafter: 1\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormat_LeavesFoldedScalarContent verifies the same for a folded block.
func TestFormat_LeavesFoldedScalarContent(t *testing.T) {
	src := "text: >-\n  line one   \n  line two\n\nnext: 1   \n"
	want := "text: >-\n  line one   \n  line two\n\nnext: 1\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormat_LeavesSequenceBlockScalar verifies a block scalar as a sequence
// item is protected too. The header's own trailing space is removed, since it
// is not part of the value.
func TestFormat_LeavesSequenceBlockScalar(t *testing.T) {
	src := "steps:\n  - | \n    keep me   \n  - run   \n"
	want := "steps:\n  - |\n    keep me   \n  - run\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormat_KeepsBlockScalarTailWithKeepChomping verifies the blank lines at the
// end of a "|+" block scalar are content, not formatting, and stay put.
func TestFormat_KeepsBlockScalarTailWithKeepChomping(t *testing.T) {
	src := "apiVersion: openchoreo.dev/v1alpha1\nkind: Component\nmetadata:\n  name: tail\n  annotations:\n    openchoreo.dev/description: |+\n      alpha\n\n      beta\n\n\n"
	got := string(fixer.FormatYAML([]byte(src)))
	if got != src {
		t.Errorf("content of a |+ block scalar changed:\ngot:  %q\nwant: %q", got, src)
	}
}

// TestFormat_LeavesBlockScalarTail verifies the blank lines a block scalar owns
// at the end of the file stay put, whether the block keeps or strips them.
func TestFormat_LeavesBlockScalarTail(t *testing.T) {
	cases := map[string]string{
		"kind: Component\ndescription: |\n  alpha\n\n\n":         "kind: Component\ndescription: |\n  alpha\n\n\n",
		"kind: Component\ndescription: |+\n  alpha\n\n\n":        "kind: Component\ndescription: |+\n  alpha\n\n\n",
		"kind: Component\ndescription: |\n  alpha   \n":          "kind: Component\ndescription: |\n  alpha   \n",
		"kind: Component\nname: tail\ndescription: |\n  alpha\n": "kind: Component\nname: tail\ndescription: |\n  alpha\n",
	}
	for src, want := range cases {
		if got := string(fixer.FormatYAML([]byte(src))); got != want {
			t.Errorf("format(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestFormat_LeavesIndentationAlone verifies the formatter does not re-indent,
// which is the change that could alter how a document parses.
func TestFormat_LeavesIndentationAlone(t *testing.T) {
	src := "a:\n    b:\n        c: 1\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != src {
		t.Errorf("indentation changed: got %q, want %q", got, src)
	}
}

// TestFormat_LeavesPipeInScalarAlone verifies a pipe inside a normal scalar is
// not mistaken for a block header.
func TestFormat_LeavesPipeInScalarAlone(t *testing.T) {
	src := "message: hello | world   \nnext: 1\n"
	want := "message: hello | world\nnext: 1\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormat_EmptyContent verifies an empty or whitespace-only file is left as
// it is instead of gaining a newline.
func TestFormat_EmptyContent(t *testing.T) {
	for _, src := range []string{"", "\n", "  \n\n"} {
		if got := string(fixer.FormatYAML([]byte(src))); got != src {
			t.Errorf("format(%q) = %q, want unchanged", src, got)
		}
	}
}

// TestFormat_PreservesCRLFConversion verifies CRLF input is normalized to LF.
func TestFormat_PreservesCRLFConversion(t *testing.T) {
	src := "a: 1\r\nb: 2\r\n"
	want := "a: 1\nb: 2\n"
	if got := string(fixer.FormatYAML([]byte(src))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFix_FormattingIsReported verifies the formatting pass runs as part of a
// fix and is reported once.
func TestFix_FormattingIsReported(t *testing.T) {
	src := "apiVersion: openchoreo.dev/v1alpha1   \nkind: Project\nmetadata:\n  name: doclet   \n"
	res := fixSource(t, src, fixer.Options{Format: true})
	if got := string(res.Data); got != "apiVersion: openchoreo.dev/v1alpha1\nkind: Project\nmetadata:\n  name: doclet\n" {
		t.Errorf("got %q", got)
	}
	var formatEntries int
	for _, a := range res.Applied {
		if a.Code == "format" {
			formatEntries++
		}
	}
	if formatEntries != 1 {
		t.Errorf("format entries = %d, want 1 (%v)", formatEntries, appliedMessages(res))
	}
}

// TestFix_FormattingDisabled verifies fixer.Options without Format leaves
// whitespace alone.
func TestFix_FormattingDisabled(t *testing.T) {
	src := "apiVersion: openchoreo.dev/v1alpha1   \nkind: Project\n"
	res := fixSource(t, src, fixer.Options{Format: false})
	if string(res.Data) != src {
		t.Errorf("whitespace changed with Format disabled: %q", res.Data)
	}
}

// TestFix_TypoAndFormattingTogether verifies both repair kinds apply in one run
// and the file still parses afterwards.
func TestFix_TypoAndFormattingTogether(t *testing.T) {
	src := "apiVersion: openchoreo.dev/v1alpha1   \r\nkind: Projectf\r\nmetadata:\r\n  name: doclet\r\nspec:\r\n  deploymentPipelinveRef:   \r\n    name: standard\r\n"
	res := fixSource(t, src, fixer.Options{Format: true})
	got := string(res.Data)
	want := "apiVersion: openchoreo.dev/v1alpha1\nkind: Project\nmetadata:\n  name: doclet\nspec:\n  deploymentPipelineRef:\n    name: standard\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(res.Applied) != 3 {
		t.Errorf("applied = %v, want 3 entries (2 typos + formatting)", appliedMessages(res))
	}
}

// TestDiff_RendersChange verifies the dry-run preview shows the changed lines.
func TestDiff_RendersChange(t *testing.T) {
	before := []byte("kind: Projectf\nmetadata:\n  name: doclet\n")
	after := []byte("kind: Project\nmetadata:\n  name: doclet\n")
	got := fixer.Diff(before, after, "project.yaml")
	for _, want := range []string{"--- a/project.yaml", "+++ b/project.yaml", "@@", "-kind: Projectf", "+kind: Project", " metadata:"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
}

// TestDiff_IdenticalIsEmpty verifies no diff is produced for equal content.
func TestDiff_IdenticalIsEmpty(t *testing.T) {
	same := []byte("kind: Project\n")
	if got := fixer.Diff(same, same, "project.yaml"); got != "" {
		t.Errorf("expected no diff, got:\n%s", got)
	}
}

// TestTokenSpan_RejectsMismatch verifies a position that does not hold the token
// is refused instead of edited blindly.
func TestTokenSpan_RejectsMismatch(t *testing.T) {
	if _, _, ok := fixer.TokenSpan("  \"parametrs\": []", 3, "parametrs"); ok {
		t.Error("expected a quoted key position to be refused")
	}
	if _, _, ok := fixer.TokenSpan("  parametrs: []", 3, "parametrs"); !ok {
		t.Error("expected the plain key position to be accepted")
	}
	if _, _, ok := fixer.TokenSpan("short", 3, "parametrs"); ok {
		t.Error("expected an out-of-range position to be refused")
	}
}
