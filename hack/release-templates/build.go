// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

type binaryInventory struct {
	Version  string            `json:"version"`
	Latest   string            `json:"latest"`
	Versions []string          `json:"versions"`
	Files    map[string]string `json:"files"`
}

func (c *coordinator) verify(snapshotPath, binary string) error {
	meta, err := c.validate(snapshotPath)
	if err != nil {
		return err
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return err
	}
	isolated, err := os.MkdirTemp("", "occ-release-verification-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(isolated)
	if runtime.GOOS == windowsOS && !strings.EqualFold(filepath.Ext(binary), ".exe") {
		// Packaging historically expects an extensionless occ file on every OS.
		// Use a temporary .exe to verify it natively without renaming that artifact.
		executable := filepath.Join(isolated, "occ.exe")
		if err := copyFile(binary, executable, 0o755); err != nil {
			return err
		}
		binary = executable
	}
	env := map[string]string{"HOME": isolated, "USERPROFILE": isolated}
	data, err := c.run(isolated, env, binary, "version", "--templates")
	if err != nil {
		return err
	}
	var actual binaryInventory
	if err := json.Unmarshal(data, &actual); err != nil {
		return fmt.Errorf("parse embedded inventory: %w", err)
	}
	if actual.Version != meta.ReleaseVersion || actual.Latest != meta.TemplateVersion ||
		!slices.Equal(actual.Versions, []string{meta.TemplateVersion}) || !maps.Equal(actual.Files, meta.Files) {
		return fmt.Errorf("compiled occ does not embed exactly the validated release snapshot")
	}
	listing, err := c.run(isolated, env, binary, "lint", "schema", "list")
	if err != nil {
		return err
	}
	if !strings.Contains(string(listing), meta.TemplateVersion) {
		return fmt.Errorf("CLI cannot discover its embedded release template")
	}
	unavailable := "v0.0.0"
	if meta.TemplateVersion == unavailable {
		unavailable = "v0.0.1"
	}
	result, err := c.run(isolated, env, binary, "lint", "schema", "select", unavailable)
	if err == nil || !strings.Contains(strings.ToLower(string(result)), "available") {
		return fmt.Errorf("CLI did not clearly reject an unavailable template version")
	}
	fmt.Fprintf(c.log, "Verified compiled occ: %s (%d embedded files)\n", binary, len(actual.Files))
	return nil
}

func releasePlatforms(value string) ([]string, error) {
	native := runtime.GOOS + "/" + runtime.GOARCH
	targets := strings.Fields(value)
	if len(targets) == 0 {
		targets = []string{native}
	}
	for _, target := range targets {
		parts := strings.Split(target, "/")
		if len(parts) != 2 || !slices.Contains([]string{"linux", "darwin", windowsOS}, parts[0]) ||
			!slices.Contains([]string{"amd64", "arm64"}, parts[1]) {
			return nil, fmt.Errorf("unsupported release platform %q", target)
		}
	}
	return targets, nil
}

type buildMetadata struct {
	revision  string
	timestamp string
}

func (c *coordinator) buildMetadata() (buildMetadata, error) {
	data, err := c.run(c.root, nil, "git", "rev-parse", "--short=8", "HEAD")
	if err != nil {
		return buildMetadata{}, err
	}
	revision := strings.TrimSpace(string(data))
	epoch := os.Getenv("SOURCE_DATE_EPOCH")
	if epoch == "" {
		data, err = c.run(c.root, nil, "git", "show", "-s", "--format=%ct", "HEAD")
		if err != nil {
			return buildMetadata{}, err
		}
		epoch = strings.TrimSpace(string(data))
	}
	seconds, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return buildMetadata{}, fmt.Errorf("invalid build timestamp %q: %w", epoch, err)
	}
	timestamp := time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05-0700")
	return buildMetadata{revision: revision, timestamp: timestamp}, nil
}

func (c *coordinator) build(snapshotPath, platforms, output string) error {
	meta, err := c.validate(snapshotPath)
	if err != nil {
		return err
	}
	targets, err := releasePlatforms(platforms)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	metadata, err := c.buildMetadata()
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "occ-release-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	workspace := filepath.Join(temporary, "source")
	if err := c.prepare(snapshotPath, workspace); err != nil {
		return err
	}
	native := runtime.GOOS + "/" + runtime.GOARCH
	seen := make(map[string]bool)
	for _, target := range append([]string{native}, targets...) {
		if seen[target] {
			continue
		}
		seen[target] = true
		destination := output
		if !slices.Contains(targets, target) {
			destination = filepath.Join(temporary, "verifier")
		}
		args := []string{
			"go.build.occ", "GO_CURRENT_PLATFORM=" + target, "GO_BUILD_OUTPUT_DIR=" + destination,
			"RELEASE_VERSION=" + meta.ReleaseVersion, "GIT_REV=" + metadata.revision,
			"GO_BUILD_FLAGS=-trimpath -buildvcs=false", "GO_BUILD_TIME=" + metadata.timestamp,
		}
		data, err := c.run(workspace, nil, "make", args...)
		if err != nil {
			return err
		}
		fmt.Fprint(c.log, string(data))
		if target == native {
			binary := filepath.Join(destination, filepath.FromSlash(target), "occ")
			if err := c.verify(snapshotPath, binary); err != nil {
				return err
			}
		}
	}
	return nil
}
