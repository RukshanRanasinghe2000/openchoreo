// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Package pathutil defines the filesystem/URI boundary shared by occ lint and
// occ lsp. Normalize is lexical, not symlink resolution. Within is project
// scoping, never a security boundary against symlinks.
package pathutil

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// Normalize returns an absolute, cleaned native path without resolving symlinks.
func Normalize(path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", fmt.Errorf("invalid empty or NUL-containing path")
	}
	return filepath.Abs(path)
}

// Clean preserves relative paths for CLI display and discovery results.
func Clean(path string) string { return filepath.Clean(path) }

// Absolute is a compatibility adapter for callers whose interface cannot return
// an error. Failed normalization never changes an empty path into the CWD.
func Absolute(path string) string {
	if path == "" {
		return ""
	}
	p, err := Normalize(path)
	if err != nil {
		return Clean(path)
	}
	return p
}

// ToFileURI encodes a native path. Relative paths are resolved against the CWD.
func ToFileURI(path string) (string, error) {
	p, err := Normalize(path)
	if err != nil {
		return "", err
	}
	host, uriPath, err := encodeNative(p)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Host: host, Path: uriPath}).String(), nil
}

// FromFileURI accepts absolute, hierarchical file URIs only. url.Parse decodes
// the path once; a literal percent in a filename must not be decoded again.
func FromFileURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid file URI: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(uri, "#") || strings.Contains(u.Host, ":") || strings.IndexByte(u.Path, 0) >= 0 {
		return "", fmt.Errorf("invalid filesystem URI %q", uri)
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("file URI must contain an absolute path")
	}
	p, err := decodeNative(u.Host, u.Path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("file URI is not a native absolute path")
	}
	return filepath.Clean(p), nil
}

// FileURI and NativePath adapt older string-only interfaces. Invalid input
// returns empty, never the original URI masquerading as a filesystem path.
func FileURI(path string) string   { u, _ := ToFileURI(path); return u }
func NativePath(uri string) string { p, _ := FromFileURI(uri); return p }

// PathKey is internal identity, never a filename for display or opening. Case
// policy lives entirely in this package; filenames retain their spelling.
func PathKey(path string) string {
	p, err := Normalize(path)
	if err != nil {
		return ""
	}
	return nativeKey(p)
}

func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ka, kb := PathKey(a), PathKey(b)
	return ka != "" && ka == kb
}

// Within includes root itself and rejects sibling prefixes and other volumes.
func Within(root, path string) bool {
	r, p := PathKey(root), PathKey(path)
	if r == "" || p == "" {
		return false
	}
	// filepath.Rel on Windows folds case even for case-sensitive directories.
	// Require the already policy-aware keys to share an exact component prefix.
	prefix := r
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if p != r && !strings.HasPrefix(p, prefix) {
		return false
	}
	rel, err := filepath.Rel(r, p)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// DocumentKey canonicalizes local URI identity. Virtual document identities are
// opaque to filesystem logic and are returned byte-for-byte unchanged.
func DocumentKey(uri string) string {
	p, err := FromFileURI(uri)
	if err != nil {
		return uri
	}
	return FileURI(PathKey(p))
}

// CanonicalURI normalizes encoding and native path spelling without filesystem
// case-policy queries. It is also stable after directories are deleted.
func CanonicalURI(uri string) string {
	p, err := FromFileURI(uri)
	if err != nil {
		return uri
	}
	return FileURI(p)
}
