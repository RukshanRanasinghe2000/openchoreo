// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package codedefinition

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// projectRoots is a shared, concurrency-safe cache mapping a file's directory
// to the git working-tree root that contains it ("" when it is not in a repo).
// Sibling checkouts inside one workspace are separate git repositories, so the
// git root reliably identifies the project a file belongs to.
var projectRoots = &projectRootResolver{cache: map[string]string{}}

type projectRootResolver struct {
	mu    sync.Mutex
	cache map[string]string
}

// rootFor returns the git working-tree root containing uri, or "" when uri is
// not a file URI or does not live inside a git repository. The URI is
// percent-decoded first so client-provided escapes (%26, %20) resolve to the
// real directory on disk.
func (r *projectRootResolver) rootFor(uri string) string {
	path := fileURIToPath(uri)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)

	r.mu.Lock()
	root, ok := r.cache[dir]
	r.mu.Unlock()
	if ok {
		return root
	}

	root = gitTopLevel(dir)

	r.mu.Lock()
	r.cache[dir] = root
	r.mu.Unlock()
	return root
}

func gitTopLevel(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
