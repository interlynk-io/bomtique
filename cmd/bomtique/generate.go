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
	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// generateFlags layers output-specific flags on top of the common set.
type generateFlags struct {
	commonFlags
	OutDir string
	Stdout bool
	Format string
}

func newGenerateCmd() *cobra.Command {
	f := &generateFlags{}
	cmd := &cobra.Command{
		Use:   "generate [paths...]",
		Short: "Generate SBOMs from Component Manifest v1 inputs",
		Long: `generate parses every primary and components manifest from the paths supplied,
builds the shared pool, resolves each primary's reachable closure, and writes one
SBOM per primary. Output filename is <name>-<version>.cdx.json (or <name>.cdx.json
when the primary carries no version). Use --stdout to concatenate as newline-
delimited JSON instead of writing files.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, args)
		},
	}
	f.commonFlags.attach(cmd)
	cmd.Flags().StringVarP(&f.OutDir, "out", "o", "./sbom", "output directory for per-primary SBOMs")
	cmd.Flags().BoolVar(&f.Stdout, "stdout", false, "write NDJSON to stdout instead of files")
	cmd.Flags().StringVar(&f.Format, "format", "cyclonedx", "output format (cyclonedx | spdx)")
	return cmd
}

func runGenerate(stdout, stderr io.Writer, f *generateFlags, args []string) error {
	switch f.Format {
	case "cyclonedx":
	case "spdx":
		return newExitErr(exitUsageError, fmt.Errorf("--format spdx is not yet implemented (TASKS.md M10)"))
	default:
		return newExitErr(exitUsageError, fmt.Errorf("unknown --format %q (valid: cyclonedx, spdx)", f.Format))
	}
	if f.FollowSymlinks {
		fmt.Fprintln(stderr, "warning: --follow-symlinks is accepted but safefs has no opt-in path today; symlinks will still be refused")
		diag.Warn("--follow-symlinks requested but safefs opt-in is not yet wired — refusing symlinks as usual (§18.2)")
	}
	if f.OutputValidate {
		fmt.Fprintln(stderr, "warning: --output-validate is accepted but no schema is bundled yet; skipping validation")
	}

	diag.Reset()

	manifests, err := readManifests(args)
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

	if !f.Stdout {
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

	multi := len(ps.Primaries) > 1
	for _, pm := range ps.Primaries {
		if err := emitOne(stdout, stderr, f, pm, p, idx, prov, multi); err != nil {
			return err
		}
	}

	if f.WarningsAsErrors && diag.Count() > 0 {
		return newExitErr(exitWarningsError,
			fmt.Errorf("--warnings-as-errors: %d warning(s) emitted", diag.Count()))
	}
	return nil
}

// emitOne runs pool distinctness + reachability + CycloneDX emission
// for a single primary manifest and writes the result out.
func emitOne(stdout, stderr io.Writer, f *generateFlags, pm *manifest.Manifest, p *pool.Pool, idx *graph.PoolIndex, prov provenanceIndex, multiPrimary bool) error {
	primary := &pm.Primary.Primary

	if err := pool.CheckPrimaryDistinct(primary, p); err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("%s: %w", pm.Path, err))
	}

	r, err := graph.PerPrimary(idx, primary, multiPrimary)
	if err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("%s: %w", pm.Path, err))
	}

	reachable := make([]cyclonedx.ReachableComponent, 0, len(r.Components))
	for _, poolIdx := range r.Components {
		c := &p.Components[poolIdx]
		reachable = append(reachable, cyclonedx.ReachableComponent{
			Component:   c,
			ManifestDir: prov.manifestDirFor(c),
		})
	}

	emitOpts := cyclonedx.Options{MaxFileSize: f.MaxFileSize}
	if f.SourceDateEpochSet {
		v := f.SourceDateEpoch
		emitOpts.SourceDateEpoch = &v
	}

	data, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:    primary,
		PrimaryDir: filepath.Dir(pm.Path),
		Reachable:  reachable,
	}, emitOpts)
	if err != nil {
		return newExitErr(exitValidationError, fmt.Errorf("%s: emit: %w", pm.Path, err))
	}

	if f.Stdout {
		// NDJSON: one compact JSON per line.
		if _, err := stdout.Write(data); err != nil {
			return newExitErr(exitIOError, fmt.Errorf("stdout write: %w", err))
		}
		if _, err := stdout.Write([]byte{'\n'}); err != nil {
			return newExitErr(exitIOError, fmt.Errorf("stdout write: %w", err))
		}
		return nil
	}

	name := filenameFor(primary)
	out := filepath.Join(f.OutDir, name)
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return newExitErr(exitIOError, fmt.Errorf("write %s: %w", out, err))
	}
	fmt.Fprintln(stderr, "wrote", out)
	return nil
}

// filenameFor produces "<name>-<version>.cdx.json" (or "<name>.cdx.json"
// when version is absent), sanitising characters that aren't safe in
// filenames. The primary's name comes from a validated manifest so the
// sanitisation is defensive rather than strictly necessary.
func filenameFor(c *manifest.Component) string {
	name := sanitizeFilename(c.Name)
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return name + "-" + sanitizeFilename(strings.TrimSpace(*c.Version)) + ".cdx.json"
	}
	return name + ".cdx.json"
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
		fmt.Fprintln(stderr, "validation:", e.Error())
	}
}
