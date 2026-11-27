// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package pathutil

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

func TestWindowsURIConversions(t *testing.T) {
	for _, tt := range []struct{ path, uri string }{
		{`C:\`, "file:///C:/"},
		{`C:\My Project\日本語 % #?.yaml`, "file:///C:/My%20Project/%E6%97%A5%E6%9C%AC%E8%AA%9E%20%25%20%23%3F.yaml"},
		{`C:/mixed\separators/file.yaml`, "file:///C:/mixed/separators/file.yaml"},
		{`\\Server\Share\folder\file #%.yaml`, "file://Server/Share/folder/file%20%23%25.yaml"},
		{`\\Server\Share\`, "file://Server/Share/"},
	} {
		got, err := ToFileURI(tt.path)
		if err != nil || got != tt.uri {
			t.Errorf("encode %q: %q %v", tt.path, got, err)
		}
		p, err := FromFileURI(tt.uri)
		if err != nil || p != filepath.Clean(tt.path) {
			t.Errorf("decode %q: %q %v", tt.uri, p, err)
		}
	}
	p, err := FromFileURI("file:///c%3A/project/file.yaml")
	if err != nil || p != `c:\project\file.yaml` {
		t.Fatal(p, err)
	}
	for _, u := range []string{"file:///relative/path", "file://server/", "file:///C:/foo%5Cbar", "file://C:/project/file.yaml", "file://./C:/file.yaml", "file://server/../file.yaml"} {
		if _, err := FromFileURI(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	if Within(`C:\project`, `D:\project\file.yaml`) {
		t.Fatal("different volumes contained")
	}
	if Within(`\\server\share`, `\\server\other\file.yaml`) {
		t.Fatal("different shares contained")
	}
}
func TestWindowsCasePolicy(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "MixedCase.yaml")
	if err := os.WriteFile(p, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	sensitive, known := directoryCaseSensitive(root)
	if !known {
		t.Skip("filesystem does not expose directory case policy")
	}
	if !sensitive && !SamePath(p, filepath.Join(root, "mixedcase.yaml")) {
		t.Fatal("case-insensitive aliases separated")
	}
	// Enable directory-specific case sensitivity where the host allows it.
	dir := filepath.Join(root, "sensitive")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(dir)
	h, err := windows.CreateFile(name, windows.FILE_WRITE_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Skipf("case-sensitive directory setup unavailable: %v", err)
	}
	var flags uint32 = 1
	err = windows.SetFileInformationByHandle(h, windows.FileCaseSensitiveInfo, (*byte)(unsafe.Pointer(&flags)), 4)
	windows.CloseHandle(h)
	if err != nil {
		t.Skipf("case-sensitive directories unavailable: %v", err)
	}
	upper, lower := filepath.Join(dir, "Case.yaml"), filepath.Join(dir, "case.yaml")
	for _, p := range []string{upper, lower} {
		if err := os.WriteFile(p, []byte(p), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if SamePath(upper, lower) || DocumentKey(FileURI(upper)) == DocumentKey(FileURI(lower)) {
		t.Fatal("distinct case-sensitive files merged")
	}
	if Within(upper, lower) {
		t.Fatal("case-sensitive containment folded names")
	}
	if !Within(dir, upper) || !Within(dir, lower) {
		t.Fatal("case-sensitive containment failed")
	}
	if strings.Contains(FileURI(upper), "case.yaml") {
		t.Fatal("display casing changed")
	}
	// Removing a file must not change its identity while its parent still exists.
	key := PathKey(upper)
	if err := os.Remove(upper); err != nil {
		t.Fatal(err)
	}
	if PathKey(upper) != key {
		t.Fatal("deleted file identity changed")
	}
}
