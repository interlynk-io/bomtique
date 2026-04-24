// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
)

func newValidateCmd() *cobra.Command {
	f := &commonFlags{}
	cmd := &cobra.Command{
		Use:   "validate [paths...]",
		Short: "Validate Component Manifest v1 inputs without emitting an SBOM",
		Long: `validate parses every input manifest and applies the §13.1 structural and
§13.2 semantic rules (including filesystem checks for hash file/path targets).
Exit code 0 on success, 1 on any validation error, 2 on usage error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.ErrOrStderr(), f, args)
		},
	}
	f.attach(cmd)
	return cmd
}

func runValidate(stderr io.Writer, f *commonFlags, args []string) error {
	diag.Reset()

	manifests, err := readManifests(args, verboseWriter(stderr, f.Verbose))
	if err != nil {
		return newExitErr(exitIOError, err)
	}

	errs := validate.ProcessingSet(manifests, validate.Options{
		MaxFileSize: f.MaxFileSize,
	})
	if len(errs) > 0 {
		printValidationErrors(stderr, errs)
		return newExitErr(exitValidationError, fmt.Errorf("manifest validation failed: %d error(s)", len(errs)))
	}

	if f.WarningsAsErrors && diag.Count() > 0 {
		return newExitErr(exitWarningsError,
			fmt.Errorf("--warnings-as-errors: %d warning(s) emitted", diag.Count()))
	}
	return nil
}
