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
	"github.com/interlynk-io/bomtique/internal/regfetch"
)

type addFlags struct {
	Dir      string
	Into     string
	Primary  bool
	From     string
	External []string
	Ref      string

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
		Long: `add builds a Component and writes it to the nearest components manifest
(pool). With --primary it instead appends a ref to the primary's depends-on.

Target resolution (pool mode):
  default:   nearest .components.json or .components.csv walking up from -C
             (or CWD). Created alongside the primary if none exists.
  --into:    explicit path; honoured verbatim.

Input sources (merged in order, later layers win):
  1. --from <path|->   a JSON file containing a bare Component object, or
                       '-' to read one from stdin. Fields not in the file
                       are left unset.
  2. --ref <ref>       importer fetch. The ref is a purl (pkg:<type>/...)
                       or a registry URL (github.com, gitlab.com, npmjs.com,
                       pypi.org, crates.io); see 'bomtique manifest --help'
                       for the supported shapes. When --ref is set but no
                       importer matches, add fails. When --ref is unset, no
                       fetch happens. Set BOMTIQUE_OFFLINE=1 to validate
                       the ref against the importer set without making the
                       HTTP call.
  3. CLI flags         every --name/--version/--license/... value supplied.
                       Each override prints one 'warning: flag --X overrode
                       field Y from --from input' line on stderr.

License with per-ID texts / files (§6.3): supply them in a --from JSON file
under the 'license.texts[]' array. Each entry needs 'id' plus exactly one
of 'text' (inline) or 'file' (relative path, read at scan time).

Hash directives (§8): --vendored-at installs a directory-form hash for you
(see below). Other hash shapes — literal values, explicit file targets,
extra algorithms — must come in via --from so all fields line up.

Vendored components (§9.3): --vendored-at <dir> synthesises three fields
at once:
  * a repo-local purl derived from the primary's purl (unless --purl was
    set explicitly);
  * one directory-form SHA-256 hash directive on that path (digest is
    computed at scan time, not now — edit freely until then);
  * pedigree.ancestors[0] built from the --upstream-* flags. --upstream-name
    and one of --upstream-version / --upstream-purl are required when any
    --upstream-* flag is supplied.
--ext filters the directory-hash walk to a case-insensitive extension set
(e.g. --ext c,h,cpp). Ignored when --vendored-at points at a regular file.

CSV targets reject fields CSV cannot represent (external_references,
structured license texts, directory-form hashes, pedigree, lifecycles).
The error tells you to rerun with --into <json-path>.

Identity collisions against the existing pool (§11) are rejected hard.

On success prints one of (to stdout):
  created <path>                                 new components file was seeded
  updated <path>                                 pool entry appended
  added <ref> to <primary> depends-on            --primary path
  unchanged <primary> (ref "<ref>" already in depends-on)

Examples:
  # flag-driven pool add
  bomtique manifest add \
    --name libx --version 1.0 --license MIT \
    --purl pkg:generic/acme/libx@1.0

  # from a JSON file (license.texts, hashes, pedigree come from the file)
  bomtique manifest add --from ./incoming/libx.json

  # registry fetch via purl
  bomtique manifest add --ref pkg:npm/express@4.18.2

  # registry fetch via URL
  bomtique manifest add --ref https://www.npmjs.com/package/express/v/4.18.2

  # vendored component with directory-hash and upstream ancestor
  bomtique manifest add \
    --name vendor-libx --version 2.4.0 \
    --vendored-at ./src/vendor-libx --ext c,h \
    --upstream-name libx --upstream-version 2.4.0 \
    --upstream-purl pkg:github/upstream-org/libx@2.4.0 \
    --upstream-supplier "Upstream Inc"

  # append to the primary's depends-on instead of the pool
  bomtique manifest add --primary \
    --name libx --version 1.0 --purl pkg:generic/acme/libx@1.0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestAdd(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, cmd.InOrStdin())
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().StringVar(&f.Into, "into", "", "explicit target components manifest path (overrides auto-discovery)")
	cmd.Flags().BoolVar(&f.Primary, "primary", false, "append to the primary manifest's depends-on instead of the pool")
	cmd.Flags().StringVar(&f.From, "from", "",
		"path to a Component JSON file (bare Component object), or '-' to read one from stdin")
	cmd.Flags().StringArrayVar(&f.External, "external", nil,
		"extra external reference as type=url (repeatable); type is one of website|vcs|documentation|"+
			"issue-tracker|distribution|support|release-notes|advisories|other")
	cmd.Flags().StringVar(&f.Ref, "ref", "",
		"importer ref: a purl (pkg:<type>/<name>[@<ver>]) or registry URL "+
			"(github.com, gitlab.com, npmjs.com, pypi.org, crates.io). When "+
			"set, bomtique fetches metadata from the matching importer; flag "+
			"values override fetched fields. Set BOMTIQUE_OFFLINE=1 to skip "+
			"the HTTP call.")

	cmd.Flags().StringVar(&f.Name, "name", "", "component name")
	cmd.Flags().StringVar(&f.Version, "version", "", "component version")
	cmd.Flags().StringVar(&f.Type, "type", "",
		"component type: library|application|framework|container|operating-system|"+
			"device|firmware|file|platform|device-driver|machine-learning-model|data "+
			"(default: library)")
	cmd.Flags().StringVar(&f.Description, "description", "", "human-readable description")
	cmd.Flags().StringVar(&f.License, "license", "", "SPDX license expression (use --from for structured license texts)")
	cmd.Flags().StringVar(&f.Purl, "purl", "", "literal Package URL to record on the component (use --ref to drive a registry fetch)")
	cmd.Flags().StringVar(&f.CPE, "cpe", "", "CPE 2.3 identifier")
	cmd.Flags().StringVar(&f.Scope, "scope", "", "pool-entry scope: required|optional|excluded (ignored on --primary)")

	cmd.Flags().StringVar(&f.Supplier, "supplier", "", "supplier name")
	cmd.Flags().StringVar(&f.SupplierEmail, "supplier-email", "", "supplier email")
	cmd.Flags().StringVar(&f.SupplierURL, "supplier-url", "", "supplier website URL")

	cmd.Flags().StringVar(&f.Website, "website", "", "external reference (website)")
	cmd.Flags().StringVar(&f.VCS, "vcs", "", "external reference (vcs)")
	cmd.Flags().StringVar(&f.Distribution, "distribution", "", "external reference (distribution)")
	cmd.Flags().StringVar(&f.IssueTracker, "issue-tracker", "", "external reference (issue-tracker)")

	cmd.Flags().StringArrayVar(&f.DependsOn, "depends-on", nil,
		"depends-on ref as pkg:<type>/<name>[@<ver>] or <name>@<ver> (repeatable)")
	cmd.Flags().StringArrayVar(&f.Tags, "tag", nil, "component tag for --tag filtering at scan time (repeatable)")

	cmd.Flags().StringVar(&f.VendoredAt, "vendored-at", "",
		"relative path to vendored source dir; installs a directory-form SHA-256 hash directive")
	cmd.Flags().StringSliceVar(&f.Extensions, "ext", nil,
		"directory-hash extension filter (case-insensitive, comma-separated): e.g. c,h,cpp")
	cmd.Flags().StringVar(&f.UpstreamName, "upstream-name", "",
		"ancestor name for pedigree.ancestors[0] (required with any --upstream-* flag)")
	cmd.Flags().StringVar(&f.UpstreamVersion, "upstream-version", "",
		"ancestor version (one of --upstream-version / --upstream-purl is required)")
	cmd.Flags().StringVar(&f.UpstreamPurl, "upstream-purl", "", "ancestor purl (must differ from the component's own purl)")
	cmd.Flags().StringVar(&f.UpstreamSupplier, "upstream-supplier", "", "ancestor supplier name")
	cmd.Flags().StringVar(&f.UpstreamWebsite, "upstream-website", "", "ancestor website URL")
	cmd.Flags().StringVar(&f.UpstreamVCS, "upstream-vcs", "", "ancestor VCS URL")

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

		Ref: f.Ref,
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
	if errors.Is(err, regfetch.ErrUnsupportedRef) ||
		errors.Is(err, regfetch.ErrNetwork) ||
		errors.Is(err, regfetch.ErrNotFound) ||
		errors.Is(err, regfetch.ErrRateLimited) ||
		errors.Is(err, regfetch.ErrResponseTooLarge) {
		return newExitErr(exitValidationError, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		return newExitErr(exitIOError, err)
	}
	return newExitErr(exitValidationError, err)
}
