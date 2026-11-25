// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type commandRunner func(dir string, env map[string]string, name string, args ...string) ([]byte, error)

const windowsOS = "windows"

type coordinator struct {
	root string
	log  io.Writer
	run  commandRunner
}

func newCoordinator(root string, log io.Writer) (*coordinator, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	c := &coordinator{root: root, log: log}
	c.run = c.runCommand
	return c, nil
}

func (c *coordinator) runCommand(dir string, overrides map[string]string, name string, args ...string) ([]byte, error) {
	fmt.Fprintf(c.log, "+ %s %s\n", name, strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	//nolint:gosec // Release tooling executes the selected Go/Make/CLI argument vector without a shell.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = commandEnv(overrides)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), fmt.Errorf("%s failed: %w\n%s", name, err, stderr.String())
	}
	fmt.Fprint(c.log, stderr.String())
	return stdout.Bytes(), nil
}

func commandEnv(overrides map[string]string) []string {
	env := os.Environ()
	for key, value := range overrides {
		filtered := env[:0]
		for _, entry := range env {
			name, _, _ := strings.Cut(entry, "=")
			same := name == key || runtime.GOOS == windowsOS && strings.EqualFold(name, key)
			if !same {
				filtered = append(filtered, entry)
			}
		}
		filtered = append(filtered, key+"="+value)
		env = filtered
	}
	return env
}

var releaseVersionRE = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

func readVersion(root string) (string, string, error) {
	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return "", "", err
	}
	version := strings.TrimSpace(string(data))
	match := releaseVersionRE.FindStringSubmatch(version)
	if match == nil {
		return "", "", fmt.Errorf("invalid VERSION %q: expected SemVer without a leading v", version)
	}
	for _, part := range strings.Split(match[4], ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return "", "", fmt.Errorf("invalid VERSION: numeric prerelease identifiers cannot have leading zeroes")
		}
	}
	return version, "v" + strings.Join(match[1:4], "."), nil
}
