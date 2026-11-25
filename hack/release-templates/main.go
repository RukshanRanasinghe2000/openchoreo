// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Command release-templates coordinates the existing Go template generators and
// builds occ from a validated, single-version snapshot in isolated source.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if err := execute(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release templates:", err)
		os.Exit(1)
	}
}

func execute(args []string, log io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: release-templates <generate|build|validate|prepare|verify> [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(log)
	root := flags.String("root", ".", "OpenChoreo source root containing VERSION")
	snapshot := flags.String("snapshot", "", "Validated snapshot (default: <root>/bin/release-templates)")
	workspace := flags.String("workspace", "", "New isolated source directory for prepare")
	binary := flags.String("binary", "", "Native occ executable for verify")
	output := flags.String("output", "", "Binary output directory (default: <root>/bin/dist)")
	platforms := flags.String("platforms", "", "Space-separated OS/architecture targets; default: native")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	c, err := newCoordinator(*root, log)
	if err != nil {
		return err
	}
	if *snapshot == "" {
		*snapshot = filepath.Join(c.root, "bin", "release-templates")
	}
	snapshotPath, err := filepath.Abs(*snapshot)
	if err != nil {
		return err
	}
	switch args[0] {
	case "generate":
		return c.generate(snapshotPath)
	case "validate":
		_, err := c.validate(snapshotPath)
		return err
	case "prepare":
		if *workspace == "" {
			return fmt.Errorf("prepare requires --workspace")
		}
		return c.prepare(snapshotPath, *workspace)
	case "verify":
		if *binary == "" {
			return fmt.Errorf("verify requires --binary")
		}
		return c.verify(snapshotPath, *binary)
	case "build":
		if *output == "" {
			*output = filepath.Join(c.root, "bin", "dist")
		}
		useSnapshot := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "snapshot" {
				useSnapshot = true
			}
		})
		if useSnapshot {
			return c.build(snapshotPath, *platforms, *output)
		}
		temporary, err := os.MkdirTemp("", "occ-release-templates-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temporary)
		snapshotPath = filepath.Join(temporary, "snapshot")
		if err := c.generate(snapshotPath); err != nil {
			return err
		}
		return c.build(snapshotPath, *platforms, *output)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
