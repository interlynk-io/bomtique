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
// pretty indent, SOURCE_DATE_EPOCH resolved from the environment —
// matches the CI-friendly default.
type Options struct {
	// MaxFileSize is the per-read cap enforced by safefs.Open when the
	// emitter needs to embed file contents (license texts, patch
	// diffs). Zero or negative uses safefs.DefaultMaxFileSize (10 MiB).
	MaxFileSize int64

	// Indent, when non-zero, switches from compact json.Marshal output
	// to indented json.MarshalIndent with two-space indentation. The
	// byte-identical determinism guarantee of §15 still holds across
	// two runs with the same Indent setting, but mixing indent modes
	// is of course not byte-identical.
	Indent bool

	// SourceDateEpoch overrides the `SOURCE_DATE_EPOCH` env variable.
	// When non-nil, the emitter sets `metadata.timestamp` to the ISO
	// 8601 UTC form of this epoch and derives `serialNumber` as a
	// UUIDv5 over the JCS-canonicalised `components[]` array. When
	// nil and the env var is set, the same treatment applies with the
	// env value; otherwise no timestamp is emitted and the serial
	// number is a UUIDv4 (random).
	SourceDateEpoch *int64
}

// Emit produces the CycloneDX 1.7 JSON bytes for one primary. The
// output is byte-identical across runs when:
//
//   - the inputs are unchanged;
//   - SOURCE_DATE_EPOCH (or Options.SourceDateEpoch) is set;
//   - referenced files haven't changed on disk (§15.4).
//
// Field ordering is stable because every type is a struct-per-field;
// array ordering follows §15.2; the serialNumber derives from the
// JCS-canonicalised components array per §15.3.
func Emit(in EmitInput, opts Options) ([]byte, error) {
	if in.Primary == nil {
		return nil, fmt.Errorf("cyclonedx: nil primary")
	}
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = safefs.DefaultMaxFileSize
	}

	epoch, err := resolveSourceDateEpoch(opts)
	if err != nil {
		return nil, err
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
			Component:  &primary,
			Lifecycles: buildLifecycles(in.Primary.Lifecycles),
		},
		Components:   components,
		Dependencies: deps,
	}

	// §15.2: sort components, dependencies, and nested arrays. Done
	// before timestamp / serial so the JCS digest sees the final
	// components[] order.
	sortBOM(&bom)

	if epoch != nil {
		bom.Metadata.Timestamp = formatTimestamp(*epoch)
		serial, err := computeDeterministicSerial(bom.Components)
		if err != nil {
			return nil, err
		}
		bom.SerialNumber = serial
	}

	if opts.Indent {
		return json.MarshalIndent(bom, "", "  ")
	}
	return json.Marshal(bom)
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
