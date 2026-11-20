// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func encodeNative(path string) (string, string, error) {
	// Device namespaces are not ordinary file URI resources.
	if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
		return "", "", fmt.Errorf("Windows device paths cannot be represented as file URIs")
	}
	p := filepath.ToSlash(path)
	if strings.HasPrefix(p, "//") {
		host, tail, ok := strings.Cut(p[2:], "/")
		if !ok || host == "" || tail == "" {
			return "", "", fmt.Errorf("UNC path requires a server and share")
		}
		return host, "/" + tail, nil
	}
	return "", "/" + p, nil
}
func decodeNative(host, path string) (string, error) {
	if strings.Contains(path, `\`) {
		return "", fmt.Errorf("backslashes are not URI path separators")
	}
	if host != "" && !strings.EqualFold(host, "localhost") {
		if strings.ContainsAny(host, `\/`) || host == "." || host == ".." || path == "/" {
			return "", fmt.Errorf("invalid UNC file URI")
		}
		share := strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
		if share == "" || share == "." || share == ".." {
			return "", fmt.Errorf("invalid UNC share")
		}
		return filepath.FromSlash("//" + host + path), nil
	}
	if len(path) < 4 || path[0] != '/' || path[2] != ':' || path[3] != '/' || !isDrive(path[1]) {
		return "", fmt.Errorf("Windows file URI requires a drive or UNC host")
	}
	return filepath.FromSlash(path[1:]), nil
}
func isDrive(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// Determine case policy per parent directory, not per operating system. NTFS
// directories can opt into case-sensitive names. Unknown policies preserve case
// rather than merging distinct files. No policy cache survives configuration
// changes. Unknown/missing parents preserve case.
func nativeKey(path string) string {
	volume := filepath.VolumeName(path)
	rest := strings.TrimLeft(path[len(volume):], `\`)
	parent := volume + `\`
	key := strings.ToLower(volume) + `\`
	sensitive := true
	for _, part := range strings.Split(rest, `\`) {
		if part == "" {
			continue
		}
		flag, known := directoryCaseSensitive(parent)
		sensitive = !known || flag
		component := part
		if !sensitive {
			component = strings.ToLower(component)
		}
		key = filepath.Join(key, component)
		parent = filepath.Join(parent, part)
	}
	return key
}
func directoryCaseSensitive(path string) (bool, bool) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true, false
	}
	h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return true, false
	}
	defer windows.CloseHandle(h)
	var flags uint32
	err = windows.GetFileInformationByHandleEx(h, windows.FileCaseSensitiveInfo, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	return flags&1 != 0, err == nil
}
