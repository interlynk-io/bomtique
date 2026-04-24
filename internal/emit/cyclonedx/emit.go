// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"encoding/json"
	"fmt"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// ReachableComponent pairs a pool component with the manifest directory
// that sourced it. Path-bearing fields — license.file,
// license.texts[].file, hash.file, hash.path, patch diff.url (local) —
// are resolved against ManifestDir per §12.4.
type ReachableComponent struct {
	Component   *manifest.Component
	ManifestDir string
}

// EmitInput is the full input to one CycloneDX emission — one primary
// plus the reachable subset of the pool, each tagged with its source
// manifest directory so file-form hashes, license texts, and patch
// diffs resolve against the right base.
type EmitInput struct {
	Primary    *manifest.Component
	PrimaryDir string
	Reachable  []ReachableComponent
}

// Options controls emitter behaviour. The zero value — 10 MiB cap, no
// pretty indent — matches the CI-friendly default.
type Options struct {
	// MaxFileSize is the per-read cap enforced by safefs.Open when the
	// emitter needs to embed file contents (license texts, patch
	// diffs). Zero or negative uses safefs.DefaultMaxFileSize (10 MiB).
	MaxFileSize int64

	// Indent, when non-zero, switches from compact json.Marshal output
	// to indented json.MarshalIndent with two-space indentation. The
	// canonical form (for the M8 determinism harness) will use JCS
	// canonicalisation instead; Indent is for human readers only.
	Indent bool

	// SourceDateEpoch, when non-nil, overrides the emission timestamp.
	// M8 will wire this to the `SOURCE_DATE_EPOCH` env. For M7 the
	// emitter leaves timestamp empty unless the caller sets it here.
	SourceDateEpoch *string
}

// Emit produces the CycloneDX 1.7 JSON bytes for one primary.
// Determinism-critical details (sorting, UUIDv5 serial, canonical
// encoding) land in M8; this pass produces a conforming-shape document
// whose field ordering is stable because every struct is fully
// modelled.
func Emit(in EmitInput, opts Options) ([]byte, error) {
	if in.Primary == nil {
		return nil, fmt.Errorf("cyclonedx: nil primary")
	}
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = safefs.DefaultMaxFileSize
	}

	// Assign bom-refs up-front so dependency resolution below can
	// reference them. Includes collision detection.
	refs, err := assignBOMRefs(in)
	if err != nil {
		return nil, err
	}

	primary, err := buildComponent(*in.Primary, in.PrimaryDir, refs.primary, true /* isPrimary */, opts)
	if err != nil {
		return nil, err
	}

	components := make([]cdxComponent, 0, len(in.Reachable))
	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		comp, err := buildComponent(*rc.Component, rc.ManifestDir, refs.pool[i], false /* isPrimary */, opts)
		if err != nil {
			return nil, err
		}
		components = append(components, comp)
	}

	deps := buildDependencies(in, refs)

	bom := cdxBOM{
		BOMFormat:   bomFormat,
		SpecVersion: specVersion,
		Version:     1,
		Metadata: &cdxMetadata{
			Timestamp:  timestampFromOpts(opts),
			Component:  &primary,
			Lifecycles: buildLifecycles(in.Primary.Lifecycles),
		},
		Components:   components,
		Dependencies: deps,
	}

	if opts.Indent {
		return json.MarshalIndent(bom, "", "  ")
	}
	return json.Marshal(bom)
}

// timestampFromOpts returns the emission timestamp.  In M7 we only
// honour an explicitly-set override on Options; M8 will add
// SOURCE_DATE_EPOCH env handling and ISO 8601 UTC-second formatting.
func timestampFromOpts(opts Options) string {
	if opts.SourceDateEpoch != nil {
		return *opts.SourceDateEpoch
	}
	return ""
}

// buildLifecycles maps manifest Lifecycle entries onto the CycloneDX
// metadata.lifecycles array. When the primary omits lifecycles, the
// spec (§7.5) says the default is `[{phase: build}]`; the emitter
// inserts that default so the SBOM records *why* it was generated.
func buildLifecycles(in []manifest.Lifecycle) []cdxLifecycle {
	if len(in) == 0 {
		return []cdxLifecycle{{Phase: "build"}}
	}
	out := make([]cdxLifecycle, 0, len(in))
	for _, l := range in {
		out = append(out, cdxLifecycle{Phase: l.Phase})
	}
	return out
}
