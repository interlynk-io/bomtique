// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Binary bomtique is the reference consumer for Component Manifest v1.
// Scope and flag surface are defined in TASKS.md milestone M9.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

// Exit codes defined by M9:
//
//	0 — success (no errors, no warnings-as-errors trigger)
//	1 — validation / semantic error in a manifest
//	2 — CLI usage error (wrong flags, missing args, etc.)
//	3 — I/O error (read/write failure, missing file we needed)
//	4 — --warnings-as-errors triggered at least one warning
const (
	exitOK              = 0
	exitValidationError = 1
	exitUsageError      = 2
	exitIOError         = 3
	exitWarningsError   = 4
)

// exitErr wraps an error with an explicit process exit code. Commands
// return it when they want a specific code other than the default
// error → 1 mapping cobra gives us.
type exitErr struct {
	code int
	err  error
}

func (e *exitErr) Error() string { return e.err.Error() }
func (e *exitErr) Unwrap() error { return e.err }

func newExitErr(code int, err error) *exitErr {
	if err == nil {
		return nil
	}
	return &exitErr{code: code, err: err}
}

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		// Use the root command's stderr writer so tests that do
		// `cmd.SetErr(buf)` can capture the final error line too.
		_, _ = fmt.Fprintln(root.ErrOrStderr(), "error:", err)
		var ee *exitErr
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		os.Exit(exitValidationError)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bomtique",
		Short:         "Hand-authored SBOM toolkit (Component Manifest v1 consumer)",
		Long:          "bomtique reads Component Manifest v1 files and emits one CycloneDX (or SPDX) SBOM per primary manifest.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newScanCmd(),
		newValidateCmd(),
		newManifestCmd(),
	)
	return root
}
