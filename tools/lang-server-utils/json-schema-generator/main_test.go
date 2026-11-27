// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/templatever"
)

func TestLatestVersionFromChangelog(t *testing.T) {
	// The version is the first "## vX.Y.Z" heading, pre-release stripped.
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "plain version",
			body: "# Changelog\n\nAll notable changes...\n\n## v1.3.0\n\n### Features\n",
			want: testTemplateV130,
		},
		{
			name: "pre-release tag strips to stable version",
			body: "# Changelog\n\n## v1.2.0-rc.1\n\nChanges...\n",
			want: "v1.2.0",
		},
		{
			name: "alpha tag strips to stable version",
			body: "## v1.1.0-alpha-1\n\nSome change\n",
			want: "v1.1.0",
		},
		{
			name: "ignores non-version headings before the first release",
			body: "# Changelog\n\n## Contributing\n\nNothing here.\n\n## v1.0.0\n",
			want: "v1.0.0",
		},
		{
			name:    "no heading",
			body:    "# Changelog\n\nNothing worth noting.\n",
			wantErr: true,
		},
		{
			name:    "missing file",
			body:    "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "CHANGELOG.md")
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := latestVersionFromChangelog(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateVersionFile(t *testing.T) {
	// The manifest lists folder-backed versions plus the generated one,
	// sorted ascending with the greatest as latest.
	dir := t.TempDir()
	for _, v := range []string{"template-v1.2.0", "template-v1.3.0"} {
		if err := os.Mkdir(filepath.Join(dir, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := updateVersionFile(dir, []string{testTemplateV130})
	if err != nil {
		t.Fatalf("updateVersionFile: %v", err)
	}
	if latest != testTemplateV130 {
		t.Errorf("updates latest = %q, want v1.3.0", latest)
	}

	data, err := os.ReadFile(filepath.Join(dir, versionFileName))
	if err != nil {
		t.Fatal(err)
	}
	got, err := templatever.Parse(data)
	if err != nil {
		t.Fatalf("parse manifest: %v (raw: %s)", err, data)
	}

	if got.Latest != testTemplateV130 {
		t.Errorf("latest = %q, want v1.3.0", got.Latest)
	}
	if len(got.Versions) != 2 || got.Versions[0] != "v1.2.0" || got.Versions[1] != testTemplateV130 {
		t.Errorf("versions = %v, want [v1.2.0 v1.3.0]", got.Versions)
	}
}

func TestUpdateVersionFileDropsStaleVersions(t *testing.T) {
	// A version whose folder no longer exists is dropped from the manifest.
	dir := t.TempDir()
	for _, v := range []string{"template-v1.1.0", "template-v1.3.0"} {
		if err := os.Mkdir(filepath.Join(dir, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, "template-v1.1.0")); err != nil {
		t.Fatal(err)
	}
	if _, err := updateVersionFile(dir, []string{testTemplateV130}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, versionFileName))
	if err != nil {
		t.Fatal(err)
	}
	got, err := templatever.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 || got.Versions[0] != testTemplateV130 {
		t.Errorf("versions = %v, want [v1.3.0]", got.Versions)
	}
}

func TestUpdateVersionFileOlderTagKeepsLatest(t *testing.T) {
	// Regenerating an older tag must not demote the latest version.
	dir := t.TempDir()
	for _, v := range []string{"template-v1.2.0", "template-v1.3.0"} {
		if err := os.Mkdir(filepath.Join(dir, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := updateVersionFile(dir, []string{"v1.2.0"})
	if err != nil {
		t.Fatalf("updateVersionFile: %v", err)
	}
	if latest != testTemplateV130 {
		t.Errorf("latesst = %q, want v1.3.0", latest)
	}

	data, err := os.ReadFile(filepath.Join(dir, versionFileName))
	if err != nil {
		t.Fatal(err)
	}
	got, err := templatever.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Latest != testTemplateV130 {
		t.Errorf("latest = %q, want v1.3.0", got.Latest)
	}
}

func TestVersionDir(t *testing.T) {
	if got := versionDir("v1.2.0"); got != "template-v1.2.0" {
		t.Errorf("versionDir(v1.2.0) = %q", got)
	}
}
