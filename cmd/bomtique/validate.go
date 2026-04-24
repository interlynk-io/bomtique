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
		Long: `validate parses every input manifest and applies the full structural and
semantic rule set scan runs, without emitting any SBOM. It checks:

  * schema markers, required fields, and enum values (spec §13.1);
  * identity rules across the pool — §11 collisions are hard failures;
  * filesystem reachability for 'hashes[].file' / '.path' targets and for
    license-text 'file' attachments (capped by --max-file-size);
  * purl canonical form and SPDX license expression syntax.

Hashes are NOT recomputed here (that is a scan-time concern), but missing
or path-escaping targets are flagged.

On success prints one stderr line: 'ok: N manifest(s) validated (P primary,
C components)'.

Exit codes: see 'bomtique --help'. In particular:
  0  all manifests valid
  1  one or more manifests failed structural or semantic checks
  2  usage error (unknown flag, bad path)
  3  I/O error reading a manifest
  4  --warnings-as-errors triggered at least one warning

Examples:
  bomtique validate
  bomtique validate ./team-a ./team-b
  bomtique validate --warnings-as-errors .primary.json .components.json`,
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

	ps := partition(manifests)
	_, _ = fmt.Fprintf(stderr, "ok: %d manifest(s) validated (%d primary, %d components)\n",
		len(manifests), len(ps.Primaries), len(ps.Components))
	return nil
}
