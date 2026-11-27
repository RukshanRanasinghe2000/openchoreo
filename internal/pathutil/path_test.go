// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"plain.yaml", "with space.yaml", "日本語 & café.yaml", "literal%20#value.yaml", "mixed%2520.yaml"} {
		p := filepath.Join(root, name)
		u, err := ToFileURI(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := FromFileURI(u)
		if err != nil || got != p {
			t.Fatalf("%q => %q => %q (%v)", p, u, got, err)
		}
		if strings.ContainsAny(u, " #") || strings.Contains(u, `\`) {
			t.Fatalf("unescaped URI %s", u)
		}
		// localhost and path-dot aliases have one identity.
		local := strings.Replace(u, "file:///", "file://localhost/", 1)
		if DocumentKey(local) != DocumentKey(u) {
			t.Fatalf("alias mismatch: %s", local)
		}
	}
	p := filepath.Join(root, "literal%20#value.yaml")
	u, _ := ToFileURI(p)
	if !strings.Contains(u, "%2520%23") {
		t.Fatal("percent was not encoded exactly once", u)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{`literal\backslash.yaml`, "question?.yaml"} {
			p := filepath.Join(root, name)
			u, err := ToFileURI(p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := FromFileURI(u)
			if err != nil || got != p {
				t.Fatalf("Unix filename lost: %q %q %v", u, got, err)
			}
		}
	}
}
func TestRelativeAndContainment(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "project", "nested", "file.yaml")
	if !Within(root, child) || !Within(root, root) || Within(filepath.Join(root, "proj"), child) || Within(child, root) || Within("", child) {
		t.Fatal("invalid containment")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join("relative", "..", "sample.yaml")
	got, err := Normalize(relative)
	if err != nil || got != filepath.Join(cwd, "sample.yaml") {
		t.Fatal(got, err)
	}
	u, err := ToFileURI(relative)
	if err != nil || NativePath(u) != got {
		t.Fatal(u, err)
	}
	volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
	if !Within(volumeRoot, root) || !SamePath(volumeRoot, filepath.Clean(volumeRoot)) {
		t.Fatal("filesystem root mishandled")
	}
	if _, err := Normalize(""); err == nil {
		t.Fatal("empty path became CWD")
	}
}
func TestInvalidAndVirtualURIs(t *testing.T) {
	for _, u := range []string{"", "file:relative.yaml", "file://host", "file:///bad%zz", "file:///name?query", "file:///name?", "file:///name#fragment", "file:///name#", "file://user@localhost/path", "file://host:99/share/path", "file:///nul%00.yaml", "https://host/file.yaml", "openchoreo://ctx/namespaces/default/component/a.yaml"} {
		if _, err := FromFileURI(u); err == nil {
			t.Errorf("accepted malformed/non-file URI %q", u)
		}
		if NativePath(u) != "" {
			t.Errorf("URI treated as a native path: %q", u)
		}
	}
	virtual := "openchoreo://ctx/namespaces/default/component/a%20b.yaml"
	if DocumentKey(virtual) != virtual {
		t.Fatal("virtual identity changed")
	}
	if SamePath("", "") {
		t.Fatal("empty paths compare as CWD")
	}
}
func TestSymlinkSpellingIsNotResolved(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if SamePath(target, link) {
		t.Fatal("normalization resolved symlink")
	}
	if Within(target, filepath.Join(link, "child")) {
		t.Fatal("lexical containment followed symlink")
	}
	got, err := FromFileURI(FileURI(link))
	if err != nil || got != link {
		t.Fatal(got, err)
	}
}
