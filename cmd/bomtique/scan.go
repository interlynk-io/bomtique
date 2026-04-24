// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/emit/cyclonedx"
	"github.com/interlynk-io/bomtique/internal/emit/spdx"
	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
	"github.com/interlynk-io/bomtique/internal/pool"
	"github.com/interlynk-io/bomtique/internal/schema"
	vendored "github.com/interlynk-io/bomtique/schemas"
)

// scanFlags layers output-destination flags on top of the emit-wide
// set (which already carries --tag, --source-date-epoch,
// --output-validate, plus the common filesystem cap + warnings
// plumbing).
type scanFlags struct {
	emitFlags
	OutDir string
	Format string
}

func newScanCmd() *cobra.Command {
	f := &scanFlags{}
	cmd := &cobra.Command{
		Use:   "scan [paths...]",
		Short: "Scan Component Manifest v1 inputs and emit SBOMs",
		Long: `scan parses every primary and components manifest from the paths supplied,
builds the shared pool, resolves each primary's reachable closure, and emits one
SBOM per primary. By default the SBOMs go to stdout as newline-delimited JSON
(one compact JSON per line); pass --out <dir> to write per-primary files named
<name>-<version>.cdx.json (or <name>.cdx.json when the primary carries no
version).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, args)
		},
	}
	f.attachEmit(cmd)
	cmd.Flags().StringVarP(&f.OutDir, "out", "o", "", "write per-primary SBOMs into this directory instead of stdout")
	cmd.Flags().StringVar(&f.Format, "format", "cyclonedx", "output format (cyclonedx | spdx)")
	return cmd
}

func runScan(stdout, stderr io.Writer, f *scanFlags, args []string) error {
	switch f.Format {
	case "cyclonedx", "spdx":
	default:
		return newExitErr(exitUsageError, fmt.Errorf("unknown --format %q (valid: cyclonedx, spdx)", f.Format))
	}
	if f.FollowSymlinks {
		_, _ = fmt.Fprintln(stderr, "warning: --follow-symlinks is accepted but safefs has no opt-in path today; symlinks will still be refused")
		diag.Warn("--follow-symlinks requested but safefs opt-in is not yet wired — refusing symlinks as usual (§18.2)")
	}

	diag.Reset()

	manifests, err := readManifests(args, verboseWriter(stderr, f.Verbose))
	if err != nil {
		return newExitErr(exitIOError, err)
	}
	ps := partition(manifests)

	if errs := validate.ProcessingSet(manifests, validate.Options{
		MaxFileSize: f.MaxFileSize,
	}); len(errs) > 0 {
		printValidationErrors(stderr, errs)
		return newExitErr(exitValidationError, fmt.Errorf("manifest validation failed: %d error(s)", len(errs)))
	}

	if len(ps.Primaries) == 0 {
		return newExitErr(exitValidationError, fmt.Errorf("processing set contains no primary manifests (§12.1)"))
	}

	if f.OutDir != "" {
		if err := os.MkdirAll(f.OutDir, 0o755); err != nil {
			return newExitErr(exitIOError, fmt.Errorf("mkdir %s: %w", f.OutDir, err))
		}
	}

	p, err := pool.Build(ps.Components)
	if err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("pool: %w", err))
	}
	p.Components = filterByTags(p.Components, f.Tags)

	prov, err := buildProvenanceIndex(ps.Components)
	if err != nil {
		return newExitErr(exitValidationError, err)
	}

	idx, err := graph.NewPoolIndex(p)
	if err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("graph index: %w", err))
	}

	var validator *schema.Validator
	if f.OutputValidate {
		validator, err = buildValidator(f.Format)
		if err != nil {
			return newExitErr(exitValidationError, err)
		}
	}

	multi := len(ps.Primaries) > 1
	for _, pm := range ps.Primaries {
		if err := emitOne(stdout, stderr, f, pm, p, idx, prov, multi, validator); err != nil {
			return err
		}
	}

	if f.WarningsAsErrors && diag.Count() > 0 {
		return newExitErr(exitWarningsError,
			fmt.Errorf("--warnings-as-errors: %d warning(s) emitted", diag.Count()))
	}
	return nil
}

// emitOne runs pool distinctness + reachability + SBOM emission (in
// the requested format) for a single primary manifest and writes the
// result out.  When `validator` is non-nil (i.e. `--output-validate`
// was requested), the emitted bytes are checked against the vendored
// schema before being written, and a schema failure aborts the run.
func emitOne(stdout, stderr io.Writer, f *scanFlags, pm *manifest.Manifest, p *pool.Pool, idx *graph.PoolIndex, prov provenanceIndex, multiPrimary bool, validator *schema.Validator) error {
	primary := &pm.Primary.Primary

	if err := pool.CheckPrimaryDistinct(primary, p); err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("%s: %w", pm.Path, err))
	}

	r, err := graph.PerPrimary(idx, primary, multiPrimary)
	if err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("%s: %w", pm.Path, err))
	}

	primaryDir := filepath.Dir(pm.Path)

	var data []byte
	switch f.Format {
	case "cyclonedx":
		data, err = emitCycloneDX(f, primary, primaryDir, p, r, prov)
	case "spdx":
		data, err = emitSPDX(f, primary, primaryDir, p, r, prov)
	}
	if err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("%s: emit: %w", pm.Path, err))
	}

	if validator != nil {
		if err := validator.Validate(data); err != nil {
			return newExitErr(exitValidationError,
				fmt.Errorf("%s: output schema validation failed: %w", pm.Path, err))
		}
	}

	if f.OutDir == "" {
		// Default: NDJSON to stdout, one compact JSON per line.
		if _, err := stdout.Write(data); err != nil {
			return newExitErr(exitIOError, fmt.Errorf("stdout write: %w", err))
		}
		if _, err := stdout.Write([]byte{'\n'}); err != nil {
			return newExitErr(exitIOError, fmt.Errorf("stdout write: %w", err))
		}
		return nil
	}

	name := filenameFor(primary, f.Format)
	out := filepath.Join(f.OutDir, name)
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return newExitErr(exitIOError, fmt.Errorf("write %s: %w", out, err))
	}
	_, _ = fmt.Fprintln(stderr, "wrote", out)
	return nil
}

func emitCycloneDX(f *scanFlags, primary *manifest.Component, primaryDir string, p *pool.Pool, r *graph.Reachability, prov provenanceIndex) ([]byte, error) {
	reachable := make([]cyclonedx.ReachableComponent, 0, len(r.Components))
	for _, poolIdx := range r.Components {
		c := &p.Components[poolIdx]
		reachable = append(reachable, cyclonedx.ReachableComponent{
			Component:   c,
			ManifestDir: prov.manifestDirFor(c),
		})
	}
	opts := cyclonedx.Options{MaxFileSize: f.MaxFileSize, ToolVersion: version}
	if f.SourceDateEpochSet {
		v := f.SourceDateEpoch
		opts.SourceDateEpoch = &v
	}
	return cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:    primary,
		PrimaryDir: primaryDir,
		Reachable:  reachable,
	}, opts)
}

func emitSPDX(f *scanFlags, primary *manifest.Component, primaryDir string, p *pool.Pool, r *graph.Reachability, prov provenanceIndex) ([]byte, error) {
	reachable := make([]spdx.ReachableComponent, 0, len(r.Components))
	for _, poolIdx := range r.Components {
		c := &p.Components[poolIdx]
		reachable = append(reachable, spdx.ReachableComponent{
			Component:   c,
			ManifestDir: prov.manifestDirFor(c),
		})
	}
	opts := spdx.Options{MaxFileSize: f.MaxFileSize, ToolVersion: version}
	if f.SourceDateEpochSet {
		v := f.SourceDateEpoch
		opts.SourceDateEpoch = &v
	}
	return spdx.Emit(spdx.EmitInput{
		Primary:    primary,
		PrimaryDir: primaryDir,
		Reachable:  reachable,
	}, opts)
}

// filenameFor produces "<name>-<version>.<ext>" (or "<name>.<ext>"
// when version is absent), sanitising characters that aren't safe in
// filenames. `format` picks the extension — `.cdx.json` for cyclonedx,
// `.spdx.json` for spdx.
func filenameFor(c *manifest.Component, format string) string {
	ext := ".cdx.json"
	if format == "spdx" {
		ext = ".spdx.json"
	}
	name := sanitizeFilename(c.Name)
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return name + "-" + sanitizeFilename(strings.TrimSpace(*c.Version)) + ext
	}
	return name + ext
}

func sanitizeFilename(s string) string {
	if s == "" {
		return "unnamed"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			if r < 0x20 {
				b.WriteByte('_')
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func printValidationErrors(stderr io.Writer, errs []validate.Error) {
	for _, e := range errs {
		_, _ = fmt.Fprintln(stderr, "validation:", e.Error())
	}
}

// buildValidator returns a schema.Validator for the chosen output
// format. `--output-validate` wires this up once at the start of a
// run and reuses it across every emitted primary.
func buildValidator(format string) (*schema.Validator, error) {
	switch format {
	case "cyclonedx":
		fsys, entry, err := vendored.CycloneDX17()
		if err != nil {
			return nil, fmt.Errorf("schema bundle: %w", err)
		}
		v, err := schema.New(fsys, entry)
		if err != nil {
			return nil, fmt.Errorf("compile CycloneDX schema: %w", err)
		}
		return v, nil
	case "spdx":
		fsys, entry, err := vendored.SPDX23()
		if err != nil {
			return nil, fmt.Errorf("schema bundle: %w", err)
		}
		v, err := schema.New(fsys, entry)
		if err != nil {
			return nil, fmt.Errorf("compile SPDX schema: %w", err)
		}
		return v, nil
	}
	return nil, fmt.Errorf("schema validator: no schema bundled for format %q", format)
}
