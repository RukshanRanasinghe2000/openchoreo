// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/openchoreo/openchoreo/internal/pathutil"
)

func copyFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copyTree(source, destination string, skip func(string, fs.DirEntry) bool) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative != "." && skip != nil && skip(relative, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return root.Symlink(link, relative)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source file type: %s", path)
		}
		return copyFile(path, target, info.Mode())
	})
}

func skipSource(relative string, entry fs.DirEntry) bool {
	switch entry.Name() {
	case ".git", "bin", "node_modules", ".cache", ".vscode", ".idea", "__pycache__":
		return true
	}
	if filepath.ToSlash(filepath.Dir(relative)) == "tools/templates" {
		return entry.Name() == "cel-envs" || entry.Name() == manifestFile || strings.HasPrefix(entry.Name(), "template-v")
	}
	return false
}

func (c *coordinator) prepare(snapshotPath, workspace string) error {
	if _, err := c.validate(snapshotPath); err != nil {
		return err
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	if pathutil.Within(c.root, workspace) || pathutil.Within(snapshotPath, workspace) {
		return fmt.Errorf("build workspace must be outside the source checkout and snapshot")
	}
	if _, err := os.Lstat(workspace); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("build workspace must be a new directory: %s", workspace)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(workspace)
		}
	}()
	if err := copyTree(c.root, workspace, skipSource); err != nil {
		return err
	}
	destination := filepath.Join(workspace, "tools", "templates")
	if err := copyTree(snapshotPath, destination, func(relative string, _ fs.DirEntry) bool {
		return relative == metadataFile
	}); err != nil {
		return err
	}
	success = true
	fmt.Fprintln(c.log, "Prepared isolated source workspace:", workspace)
	return nil
}
