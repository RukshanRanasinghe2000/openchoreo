// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package suppress

import "testing"

func TestSameLineSingleCode(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: INVALID_NAME # occ:ignore name-invalid
`
	s := New([]byte(src))
	if !s.Suppressed(4, "name-invalid") {
		t.Fatal("expected name-invalid suppressed on line 4")
	}
	if s.Suppressed(4, "name-too-long") {
		t.Fatal("did not expect name-too-long suppressed on line 4")
	}
}

func TestSameLineNoCodesAll(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
  name: BAD # occ:ignore
`
	s := New([]byte(src))
	if !s.Suppressed(2, "name-invalid") {
		t.Fatal("expected every code suppressed on line 2")
	}
	if !s.Suppressed(2, "anything-else") {
		t.Fatal("expected arbitrary code suppressed on line 2")
	}
}

func TestAllKeyword(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
  name: BAD # occ:ignore all
`
	s := New([]byte(src))
	if !s.Suppressed(2, "name-invalid") {
		t.Fatal("expected code suppressed when marker uses 'all'")
	}
}

func TestCommaAndSpaceSeparated(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
  name: BAD # occ:ignore name-invalid, name-too-long
  other: x # occ:ignore missing-metadata-name missing-kind; odd
`
	s := New([]byte(src))
	for _, code := range []string{"name-invalid", "name-too-long"} {
		if !s.Suppressed(2, code) {
			t.Fatalf("expected %s suppressed on line 2", code)
		}
	}
	if s.Suppressed(2, "name-other") {
		t.Fatal("did not expect unrelated code suppressed on line 2")
	}
	for _, code := range []string{"missing-metadata-name", "missing-kind", "odd"} {
		if !s.Suppressed(3, code) {
			t.Fatalf("expected %s suppressed on line 3", code)
		}
	}
}

func TestNonMatchingCodeNotSuppressed(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
  name: BAD # occ:ignore some-other-code
`
	s := New([]byte(src))
	if s.Suppressed(2, "name-invalid") {
		t.Fatal("name-invalid must not be suppressed by a different code")
	}
}

func TestIgnoreFileSingleCode(t *testing.T) {
	src := `# occ:ignore-file location-not-found
apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: app
`
	s := New([]byte(src))
	if !s.Suppressed(5, "location-not-found") {
		t.Fatal("expected file-wide code suppressed")
	}
	if s.Suppressed(5, "name-invalid") {
		t.Fatal("did not expect unrelated code suppressed file-wide")
	}
	if !s.Suppressed(0, "location-not-found") {
		t.Fatal("expected file-wide code to suppress unknown-line diagnostic")
	}
}

func TestIgnoreFileAll(t *testing.T) {
	src := `# occ:ignore-file
apiVersion: openchoreo.dev/v1alpha1
  name: BAD
`
	s := New([]byte(src))
	for line := 1; line <= 3; line++ {
		if !s.Suppressed(line, "name-invalid") {
			t.Fatalf("expected line %d suppressed by file-wide all marker", line)
		}
	}
	if !s.Suppressed(0, "whatever") {
		t.Fatal("expected unknown-line diagnostic suppressed by file-wide all")
	}
}

func TestIgnoreFileAnywhere(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
kind: Component
metadata:
  name: app
# occ:ignore-file location-not-found
`
	s := New([]byte(src))
	if !s.Suppressed(4, "location-not-found") {
		t.Fatal("expected marker in the middle/bottom of the file to apply file-wide")
	}
}

func TestIgnoreFileTakesPrecedence(t *testing.T) {
	src := `# occ:ignore-file
  name: BAD
`
	s := New([]byte(src))
	if !s.Suppressed(2, "name-invalid") {
		t.Fatal("occ:ignore-file must not be parsed as a line marker")
	}
}

func TestQuotedStringNotComment(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
metadata:
  description: "a note # occ:ignore name-invalid"
  name: BAD
`
	s := New([]byte(src))
	if s.Suppressed(3, "name-invalid") {
		t.Fatal("marker inside a double-quoted scalar must not suppress")
	}
	if s.Suppressed(4, "name-invalid") {
		t.Fatal("line 4 must remain unmarked (no accidental file-wide rule)")
	}
}

func TestMarkerRequiresHash(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
  name: occ:ignore name-invalid
`
	s := New([]byte(src))
	if s.Suppressed(2, "name-invalid") {
		t.Fatal("marker without a preceding '#' comment must not suppress")
	}
}

func TestSubstringNotMarker(t *testing.T) {
	src := `apiVersion: openchoreo.dev/v1alpha1
  name: xocc:ignore name-invalid
`
	s := New([]byte(src))
	if s.Suppressed(2, "name-invalid") {
		t.Fatal("occ:ignore not in a comment must not suppress")
	}
}

func TestEmptySource(t *testing.T) {
	s := New([]byte(""))
	if s.Suppressed(1, "anything") {
		t.Fatal("empty source must not suppress")
	}
}
