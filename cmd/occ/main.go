// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/openchoreo/openchoreo/internal/occ/cmd/config"
	"github.com/openchoreo/openchoreo/internal/occ/cmd/lint"
	"github.com/openchoreo/openchoreo/internal/occ/root"
)

func main() {
	rootCmd := root.BuildRootCmd()
	rootCmd.SilenceUsage = true
	// Errors are reported below rather than by cobra, so that a command that has
	// already written its own diagnostic (occ lint) is not followed by a second,
	// generic one.
	rootCmd.SilenceErrors = true

	// Initialize occ execution environment
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Commands that work purely on local files, such as `occ lint`, need no
		// context. Without this guard EnsureContext would write a default
		// ~/.openchoreo/config on a machine that has never logged in, just to
		// validate a file. The check walks ancestors, because cobra hands us
		// the leaf that ran and does not inherit annotations.
		if !config.ShouldSkipContextBootstrap(cmd) {
			// Initialize default context if none exists
			if err := config.EnsureContext(); err != nil {
				return err
			}
		}

		// Apply context defaults to command flags
		return config.ApplyContextDefaults(cmd)
	}

	// NormalizeArgs is a no-op outside `occ lint vali`, so it is safe to apply
	// to occ's whole argument list. The linter accepts single-dash spellings of
	// its long-only flags ('-dry-run', '-obj'), and pflag would otherwise read
	// those as shorthand clusters and reject them.
	rootCmd.SetArgs(lint.NormalizeArgs(os.Args[1:]))

	if err := rootCmd.Execute(); err != nil {
		// `occ lint` reports three outcomes - 0 clean, 1 findings, 2 usage
		// error - and CI gates are built on that distinction. It has already
		// written its own diagnostic, so the status is the only thing to carry
		// out; printing the error again would append a meaningless
		// "Error: exit status 1" to real output.
		if code, deliberate := lint.ExitStatus(err); deliberate {
			os.Exit(code)
		}
		// Every other command relies on cobra to report its failure, so
		// reproduce the same line on the same stream.
		fmt.Fprintf(rootCmd.ErrOrStderr(), "Error: %v\n", err)
		os.Exit(1)
	}
}
