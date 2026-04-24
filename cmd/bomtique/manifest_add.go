// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest/mutate"
)

type addFlags struct {
	Dir      string
	Into     string
	Primary  bool
	From     string
	External []string

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

	VendoredAt       string
	Extensions       []string
	UpstreamName     string
	UpstreamVersion  string
	UpstreamPurl     string
	UpstreamSupplier string
	UpstreamWebsite  string
	UpstreamVCS      string
}

func newManifestAddCmd() *cobra.Command {
	f := &addFlags{}
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a component to a components manifest or a ref to the primary's depends-on",
		Long: `add builds a Component from flag values (optionally layered on top of a
--from <path|-> base) and writes it to the nearest components manifest. Use
--primary to append to the primary manifest's depends-on list instead.

The default target is the first .components.json (or .components.csv) found
walking up from the starting directory; if none exists, one is created
alongside the primary. Override with --into <path>.

CSV targets reject fields CSV cannot represent (external_references,
structured license texts, directory-form hashes, pedigree, lifecycles); the
error suggests rerunning with --into <json-path>.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestAdd(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, cmd.InOrStdin())
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().StringVar(&f.Into, "into", "", "explicit target components manifest path")
	cmd.Flags().BoolVar(&f.Primary, "primary", false, "append to the primary manifest's depends-on instead of a pool")
	cmd.Flags().StringVar(&f.From, "from", "", "path to a Component JSON file, or - for stdin")
	cmd.Flags().StringArrayVar(&f.External, "external", nil, "additional external reference as type=url (repeatable)")

	cmd.Flags().StringVar(&f.Name, "name", "", "component name")
	cmd.Flags().StringVar(&f.Version, "version", "", "component version")
	cmd.Flags().StringVar(&f.Type, "type", "", "component type (default: library)")
	cmd.Flags().StringVar(&f.Description, "description", "", "human-readable description")
	cmd.Flags().StringVar(&f.License, "license", "", "SPDX license expression")
	cmd.Flags().StringVar(&f.Purl, "purl", "", "Package URL")
	cmd.Flags().StringVar(&f.CPE, "cpe", "", "CPE 2.3 identifier")
	cmd.Flags().StringVar(&f.Scope, "scope", "", "required|optional|excluded (pool only)")

	cmd.Flags().StringVar(&f.Supplier, "supplier", "", "supplier name")
	cmd.Flags().StringVar(&f.SupplierEmail, "supplier-email", "", "supplier email")
	cmd.Flags().StringVar(&f.SupplierURL, "supplier-url", "", "supplier website URL")

	cmd.Flags().StringVar(&f.Website, "website", "", "external reference (website)")
	cmd.Flags().StringVar(&f.VCS, "vcs", "", "external reference (vcs)")
	cmd.Flags().StringVar(&f.Distribution, "distribution", "", "external reference (distribution)")
	cmd.Flags().StringVar(&f.IssueTracker, "issue-tracker", "", "external reference (issue-tracker)")

	cmd.Flags().StringArrayVar(&f.DependsOn, "depends-on", nil, "depends-on ref (repeatable)")
	cmd.Flags().StringArrayVar(&f.Tags, "tag", nil, "component tag (repeatable)")

	cmd.Flags().StringVar(&f.VendoredAt, "vendored-at", "", "relative path to the vendored source directory (§9.3)")
	cmd.Flags().StringSliceVar(&f.Extensions, "ext", nil, "hash extension filter (repeatable or comma-separated): c,h,cpp")
	cmd.Flags().StringVar(&f.UpstreamName, "upstream-name", "", "upstream ancestor name (§9.1)")
	cmd.Flags().StringVar(&f.UpstreamVersion, "upstream-version", "", "upstream ancestor version")
	cmd.Flags().StringVar(&f.UpstreamPurl, "upstream-purl", "", "upstream ancestor purl")
	cmd.Flags().StringVar(&f.UpstreamSupplier, "upstream-supplier", "", "upstream ancestor supplier name")
	cmd.Flags().StringVar(&f.UpstreamWebsite, "upstream-website", "", "upstream ancestor website URL")
	cmd.Flags().StringVar(&f.UpstreamVCS, "upstream-vcs", "", "upstream ancestor VCS URL")

	return cmd
}

func runManifestAdd(stdout, stderr io.Writer, f *addFlags, stdin io.Reader) error {
	externals, err := parseExternals(f.External)
	if err != nil {
		return newExitErr(exitUsageError, err)
	}
	opts := mutate.AddOptions{
		FromDir:       f.Dir,
		Into:          f.Into,
		Primary:       f.Primary,
		FromPath:      f.From,
		FromReader:    stdin,
		Name:          f.Name,
		Version:       f.Version,
		Type:          f.Type,
		Description:   f.Description,
		License:       f.License,
		Purl:          f.Purl,
		CPE:           f.CPE,
		Scope:         f.Scope,
		Supplier:      f.Supplier,
		SupplierEmail: f.SupplierEmail,
		SupplierURL:   f.SupplierURL,
		Website:       f.Website,
		VCS:           f.VCS,
		Distribution:  f.Distribution,
		IssueTracker:  f.IssueTracker,
		ExternalRefs:  externals,
		DependsOn:     f.DependsOn,
		Tags:          f.Tags,

		VendoredAt:       f.VendoredAt,
		Extensions:       f.Extensions,
		UpstreamName:     f.UpstreamName,
		UpstreamVersion:  f.UpstreamVersion,
		UpstreamPurl:     f.UpstreamPurl,
		UpstreamSupplier: f.UpstreamSupplier,
		UpstreamWebsite:  f.UpstreamWebsite,
		UpstreamVCS:      f.UpstreamVCS,
	}

	// Wire the diag sink to our stderr so --from override notes and
	// --scope-on-primary warnings show up in the cobra test buffers.
	diag.SetSink(stderr)
	diag.Reset()
	defer func() {
		diag.SetSink(nil)
	}()

	res, err := mutate.Add(opts)
	if err != nil {
		return mapAddError(stderr, err)
	}

	switch {
	case res.ToPrimary && res.AlreadyPresent:
		_, _ = fmt.Fprintf(stdout, "unchanged %s (ref %q already in depends-on)\n", res.Path, res.Ref)
	case res.ToPrimary:
		_, _ = fmt.Fprintf(stdout, "added %s to %s depends-on\n", res.Ref, res.Path)
	case res.Created:
		_, _ = fmt.Fprintf(stdout, "created %s\n", res.Path)
	default:
		_, _ = fmt.Fprintf(stdout, "updated %s\n", res.Path)
	}
	return nil
}

// parseExternals parses --external flag values into ExternalRefSpec
// entries. Each value MUST be of the form `<type>=<url>`; malformed
// entries are a usage error.
func parseExternals(raw []string) ([]mutate.ExternalRefSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]mutate.ExternalRefSpec, 0, len(raw))
	for _, s := range raw {
		eq := strings.IndexByte(s, '=')
		if eq <= 0 || eq == len(s)-1 {
			return nil, fmt.Errorf("--external %q: expected <type>=<url>", s)
		}
		out = append(out, mutate.ExternalRefSpec{
			Type: strings.TrimSpace(s[:eq]),
			URL:  strings.TrimSpace(s[eq+1:]),
		})
	}
	return out, nil
}

func mapAddError(stderr io.Writer, err error) error {
	var coll *mutate.ErrIdentityCollision
	if errors.As(err, &coll) {
		return newExitErr(exitValidationError, err)
	}
	var ve *mutate.ErrInitValidation
	if errors.As(err, &ve) {
		for _, e := range ve.Errors {
			_, _ = fmt.Fprintln(stderr, "validation:", e.Error())
		}
		return newExitErr(exitValidationError, fmt.Errorf("manifest validation failed: %d error(s)", len(ve.Errors)))
	}
	if errors.Is(err, mutate.ErrInvalidRef) ||
		errors.Is(err, mutate.ErrPrimaryNotFound) {
		return newExitErr(exitValidationError, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		return newExitErr(exitIOError, err)
	}
	return newExitErr(exitValidationError, err)
}
