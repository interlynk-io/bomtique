// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/manifest/mutate"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
)

// initFlags mirrors mutate.InitOptions. Cobra populates it via flag
// parsing; runManifestInit translates it into the options struct and
// invokes mutate.Init.
type initFlags struct {
	Dir           string
	Force         bool
	Name          string
	Version       string
	Type          string
	Description   string
	License       string
	Purl          string
	CPE           string
	Supplier      string
	SupplierEmail string
	SupplierURL   string
	Website       string
	VCS           string
	Distribution  string
	IssueTracker  string
}

func newManifestInitCmd() *cobra.Command {
	f := &initFlags{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new .primary.json in the target directory",
		Long: `init writes a new primary manifest (.primary.json) using the supplied
flag values. It runs the same semantic validation scan applies before writing,
so missing required fields or invalid SPDX expressions are caught up-front.

The command does NOT create a .components.json — §5.2 forbids an empty pool.
The first 'bomtique manifest add' call creates that file on demand.

--force overwrites an existing .primary.json. Unknown top-level keys and
unknown fields on the primary component are carried over across re-init.

On success prints 'wrote <path>' or 'overwrote <path>' to stdout.

Examples:
  bomtique manifest init --name acme-app --version 1.0.0 --license Apache-2.0

  bomtique manifest init --name acme-app --version 1.0.0 \
    --license Apache-2.0 \
    --purl pkg:github/acme/app@1.0.0 \
    --supplier "Acme Corp" --supplier-email ops@acme.example \
    --website https://acme.example --vcs https://github.com/acme/app \
    --issue-tracker https://github.com/acme/app/issues

  bomtique manifest init -C ./new-service --name svc --version 0.1.0 --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestInit(cmd.OutOrStdout(), cmd.ErrOrStderr(), f)
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().BoolVar(&f.Force, "force", false, "overwrite an existing .primary.json")

	cmd.Flags().StringVar(&f.Name, "name", "", "primary component name (required)")
	cmd.Flags().StringVar(&f.Version, "version", "", "primary component version")
	cmd.Flags().StringVar(&f.Type, "type", "",
		"component type: library|application|framework|container|operating-system|"+
			"device|firmware|file|platform|device-driver|machine-learning-model|data "+
			"(default: application)")
	cmd.Flags().StringVar(&f.Description, "description", "", "human-readable description")
	cmd.Flags().StringVar(&f.License, "license", "", "SPDX license expression (e.g. Apache-2.0, MIT OR GPL-2.0)")
	cmd.Flags().StringVar(&f.Purl, "purl", "", "Package URL identifying the primary (e.g. pkg:github/acme/app@1.0.0)")
	cmd.Flags().StringVar(&f.CPE, "cpe", "", "CPE 2.3 identifier (e.g. cpe:2.3:a:acme:app:1.0.0:*:*:*:*:*:*:*)")

	cmd.Flags().StringVar(&f.Supplier, "supplier", "", "supplier name")
	cmd.Flags().StringVar(&f.SupplierEmail, "supplier-email", "", "supplier email")
	cmd.Flags().StringVar(&f.SupplierURL, "supplier-url", "", "supplier website URL")

	cmd.Flags().StringVar(&f.Website, "website", "", "external reference (website)")
	cmd.Flags().StringVar(&f.VCS, "vcs", "", "external reference (vcs)")
	cmd.Flags().StringVar(&f.Distribution, "distribution", "", "external reference (distribution)")
	cmd.Flags().StringVar(&f.IssueTracker, "issue-tracker", "", "external reference (issue-tracker)")

	if err := cmd.MarkFlagRequired("name"); err != nil {
		// Only fails if "name" isn't a registered flag, which can't
		// happen given the line above.
		panic(err)
	}
	return cmd
}

func runManifestInit(stdout, stderr io.Writer, f *initFlags) error {
	opts := mutate.InitOptions{
		Dir:           f.Dir,
		Force:         f.Force,
		Name:          f.Name,
		Version:       f.Version,
		Type:          f.Type,
		Description:   f.Description,
		License:       f.License,
		Purl:          f.Purl,
		CPE:           f.CPE,
		Supplier:      f.Supplier,
		SupplierEmail: f.SupplierEmail,
		SupplierURL:   f.SupplierURL,
		Website:       f.Website,
		VCS:           f.VCS,
		Distribution:  f.Distribution,
		IssueTracker:  f.IssueTracker,
	}

	res, err := mutate.Init(opts)
	if err != nil {
		return mapInitError(stderr, err)
	}

	verb := "wrote"
	if res.Overwrote {
		verb = "overwrote"
	}
	_, _ = fmt.Fprintf(stdout, "%s %s\n", verb, res.Path)
	return nil
}

func mapInitError(stderr io.Writer, err error) error {
	if errors.Is(err, mutate.ErrPrimaryExists) {
		return newExitErr(exitValidationError, err)
	}
	var ve *mutate.ErrInitValidation
	if errors.As(err, &ve) {
		for _, e := range ve.Errors {
			_, _ = fmt.Fprintln(stderr, "validation:", validateErrLine(e))
		}
		return newExitErr(exitValidationError, fmt.Errorf("manifest validation failed: %d error(s)", len(ve.Errors)))
	}
	return newExitErr(exitIOError, err)
}

func validateErrLine(e validate.Error) string {
	return e.Error()
}
