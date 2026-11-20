// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/openchoreo/openchoreo/internal/pathutil"
)

// resolveWorkspaceRoots extracts the working roots from an initialize request.
// Modern multi-root clients (e.g. VS Code multi-root workspaces) send
// workspaceFolders with rootUri left null; legacy single-folder clients send
// only rootUri. Returns the normalized, non-overlapping roots.
func resolveWorkspaceRoots(params *protocol.InitializeParams) []string {
	if params == nil {
		return nil
	}

	var roots []string
	if len(params.WorkspaceFolders) > 0 {
		for _, folder := range params.WorkspaceFolders {
			if path := fileURIToPath(folder.URI); path != "" {
				roots = append(roots, path)
			}
		}
	} else if params.RootURI != nil {
		if path := fileURIToPath(*params.RootURI); path != "" {
			roots = []string{path}
		}
	}

	return NormalizeWorkspaceRoots(roots)
}

// NormalizeWorkspaceRoots cleans and deduplicates workspace roots: paths are
// made absolute, exact duplicates are removed, and any root that is fully
// contained inside another root is dropped so files are never scanned or
// watched twice.
//
// Exported for the tests in internal/lsp/tests.
func NormalizeWorkspaceRoots(paths []string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		abs, err := pathutil.Normalize(p)
		if err != nil {
			continue
		}

		duplicate := false
		for _, existing := range out {
			if pathutil.SamePath(existing, abs) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}

		// Skip abs if it is contained in an already-accepted root.
		contained := false
		for _, existing := range out {
			if isSubpath(abs, existing) {
				contained = true
				break
			}
		}
		if contained {
			continue
		}

		// Drop existing roots now contained by the new, deeper abs.
		kept := out[:0]
		for _, existing := range out {
			if !isSubpath(existing, abs) {
				kept = append(kept, existing)
			}
		}
		out = kept
		out = append(out, abs)
	}
	return out
}

// isSubpath reports whether child lies inside parent (or equals it). The
// ordering matters for the leaf-of-path check: a directory that merely shares
// a name prefix ("/a/b" vs "/a/beta") must not count as a subpath.
func isSubpath(child, parent string) bool { return pathutil.Within(parent, child) }
