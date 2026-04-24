// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Binary bomtique is the reference consumer for Component Manifest v1.
// Scope and flag surface are defined in TASKS.md milestone M9.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "bomtique",
		Short:         "Hand-authored SBOM toolkit (Component Manifest v1 consumer)",
		Long:          "bomtique reads Component Manifest v1 files and emits one CycloneDX (or SPDX) SBOM per primary manifest.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newGenerateCmd(),
		newValidateCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newGenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate [paths...]",
		Short: "Generate SBOMs from Component Manifest v1 inputs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("generate: not yet implemented (TASKS.md milestone M9)")
		},
	}
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [paths...]",
		Short: "Validate Component Manifest v1 inputs without emitting an SBOM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("validate: not yet implemented (TASKS.md milestone M9)")
		},
	}
}
