// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest/mutate"
	"github.com/interlynk-io/bomtique/internal/regfetch"
)

type updateFlags struct {
	Dir     string
	Into    string
	DryRun  bool
	ToVer   string
	ExtList []string
	Offline bool
	Online  bool

	Name          string
	Version       string
	Type          string
	Description   string
	License       string
	Purl          string
	CPE           string
	Scope         string
	Supplier      string
	SupplierEmail string
	SupplierURL   string
	Website       string
	VCS           string
	Distribution  string
	IssueTracker  string
	DependsOn     []string
	Tags          []string

	ClearLicense         bool
	ClearDescription     bool
	ClearSupplier        bool
	ClearPurl            bool
	ClearCPE             bool
	ClearScope           bool
	ClearExternalRefs    bool
	ClearDependsOn       bool
	ClearTags            bool
	ClearPedigreePatches bool
}

func newManifestUpdateCmd() *cobra.Command {
	f := &updateFlags{}
	cmd := &cobra.Command{
		Use:   "update <ref>",
		Short: "Update an existing component's metadata",
		Long: `update locates the component matching <ref> (a pkg: purl or name@version)
and applies the supplied field changes. Unset flags preserve the existing
values. pedigree.patches survives by default; pass --clear-pedigree-patches
to drop it.

--to <version> bumps the component's version. If the current purl carries
a version segment equal to the old version, the purl's version is bumped
in lockstep; otherwise the purl is left alone with a stderr note.

--clear-<field> null-outs the named optional field. This is separate from
the value flags so an empty string can never mean "clear this field".`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestUpdate(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, args[0])
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().StringVar(&f.Into, "into", "", "force the update to this components manifest path")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().StringVar(&f.ToVer, "to", "", "bump version (and purl version segment when they match)")
	cmd.Flags().StringArrayVar(&f.ExtList, "external", nil, "external reference as type=url (repeatable)")
	cmd.Flags().BoolVar(&f.Offline, "offline", false, "skip registry metadata refresh")
	cmd.Flags().BoolVar(&f.Online, "online", false, "require a registered importer to refresh the target's metadata")

	cmd.Flags().StringVar(&f.Name, "name", "", "rename component")
	cmd.Flags().StringVar(&f.Version, "version", "", "replace version (use --to for lockstep purl bump)")
	cmd.Flags().StringVar(&f.Type, "type", "", "replace type")
	cmd.Flags().StringVar(&f.Description, "description", "", "replace description")
	cmd.Flags().StringVar(&f.License, "license", "", "replace license expression")
	cmd.Flags().StringVar(&f.Purl, "purl", "", "replace purl")
	cmd.Flags().StringVar(&f.CPE, "cpe", "", "replace CPE")
	cmd.Flags().StringVar(&f.Scope, "scope", "", "replace scope")

	cmd.Flags().StringVar(&f.Supplier, "supplier", "", "replace supplier name")
	cmd.Flags().StringVar(&f.SupplierEmail, "supplier-email", "", "replace supplier email")
	cmd.Flags().StringVar(&f.SupplierURL, "supplier-url", "", "replace supplier URL")

	cmd.Flags().StringVar(&f.Website, "website", "", "replace website external reference")
	cmd.Flags().StringVar(&f.VCS, "vcs", "", "replace vcs external reference")
	cmd.Flags().StringVar(&f.Distribution, "distribution", "", "replace distribution external reference")
	cmd.Flags().StringVar(&f.IssueTracker, "issue-tracker", "", "replace issue-tracker external reference")

	cmd.Flags().StringArrayVar(&f.DependsOn, "depends-on", nil, "replace depends-on (repeatable)")
	cmd.Flags().StringArrayVar(&f.Tags, "tag", nil, "replace tags (repeatable)")

	cmd.Flags().BoolVar(&f.ClearLicense, "clear-license", false, "null out license")
	cmd.Flags().BoolVar(&f.ClearDescription, "clear-description", false, "null out description")
	cmd.Flags().BoolVar(&f.ClearSupplier, "clear-supplier", false, "null out supplier")
	cmd.Flags().BoolVar(&f.ClearPurl, "clear-purl", false, "null out purl")
	cmd.Flags().BoolVar(&f.ClearCPE, "clear-cpe", false, "null out cpe")
	cmd.Flags().BoolVar(&f.ClearScope, "clear-scope", false, "null out scope")
	cmd.Flags().BoolVar(&f.ClearExternalRefs, "clear-external-refs", false, "null out external_references")
	cmd.Flags().BoolVar(&f.ClearDependsOn, "clear-depends-on", false, "null out depends-on")
	cmd.Flags().BoolVar(&f.ClearTags, "clear-tags", false, "null out tags")
	cmd.Flags().BoolVar(&f.ClearPedigreePatches, "clear-pedigree-patches", false, "null out pedigree.patches")

	return cmd
}

func runManifestUpdate(stdout, stderr io.Writer, f *updateFlags, ref string) error {
	extRefs, err := parseExternals(f.ExtList)
	if err != nil {
		return newExitErr(exitUsageError, err)
	}

	diag.SetSink(stderr)
	diag.Reset()
	defer func() { diag.SetSink(nil) }()

	opts := mutate.UpdateOptions{
		FromDir:              f.Dir,
		Into:                 f.Into,
		Ref:                  ref,
		DryRun:               f.DryRun,
		ToVersion:            f.ToVer,
		Name:                 f.Name,
		Version:              f.Version,
		Type:                 f.Type,
		Description:          f.Description,
		License:              f.License,
		Purl:                 f.Purl,
		CPE:                  f.CPE,
		Scope:                f.Scope,
		Supplier:             f.Supplier,
		SupplierEmail:        f.SupplierEmail,
		SupplierURL:          f.SupplierURL,
		Website:              f.Website,
		VCS:                  f.VCS,
		Distribution:         f.Distribution,
		IssueTracker:         f.IssueTracker,
		ExternalRefs:         extRefs,
		DependsOn:            f.DependsOn,
		Tags:                 f.Tags,
		ClearLicense:         f.ClearLicense,
		ClearDescription:     f.ClearDescription,
		ClearSupplier:        f.ClearSupplier,
		ClearPurl:            f.ClearPurl,
		ClearCPE:             f.ClearCPE,
		ClearScope:           f.ClearScope,
		ClearExternalRefs:    f.ClearExternalRefs,
		ClearDependsOn:       f.ClearDependsOn,
		ClearTags:            f.ClearTags,
		ClearPedigreePatches: f.ClearPedigreePatches,

		Offline: f.Offline,
		Online:  f.Online,
	}

	res, err := mutate.Update(opts)
	if err != nil {
		return mapUpdateError(err)
	}

	verb := "updated"
	if res.DryRun {
		verb = "would update"
	}
	_, _ = fmt.Fprintf(stdout, "%s %s in %s (fields: %s)\n", verb, res.NewRef, res.Path, strings.Join(res.FieldsChanged, ","))
	if res.PurlVersionBumped {
		_, _ = fmt.Fprintf(stdout, "  purl bumped in lockstep: %s\n", res.NewRef)
	}
	return nil
}

func mapUpdateError(err error) error {
	var coll *mutate.ErrIdentityCollision
	if errors.As(err, &coll) {
		return newExitErr(exitValidationError, err)
	}
	var mm *mutate.ErrRemoveMultiMatch
	if errors.As(err, &mm) {
		return newExitErr(exitValidationError, err)
	}
	var ve *mutate.ErrInitValidation
	if errors.As(err, &ve) {
		return newExitErr(exitValidationError, err)
	}
	if errors.Is(err, mutate.ErrUpdateNotFound) ||
		errors.Is(err, mutate.ErrPrimaryNotFound) {
		return newExitErr(exitValidationError, err)
	}
	if errors.Is(err, regfetch.ErrUnsupportedRef) ||
		errors.Is(err, regfetch.ErrNetwork) ||
		errors.Is(err, regfetch.ErrNotFound) ||
		errors.Is(err, regfetch.ErrRateLimited) ||
		errors.Is(err, regfetch.ErrResponseTooLarge) {
		return newExitErr(exitValidationError, err)
	}
	return newExitErr(exitValidationError, err)
}
