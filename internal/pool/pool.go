// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package pool

import (
	"fmt"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// Pool is the merged shared component set a processing set emits once
// per run, after §12.2 construction and §11 dedup. Components are
// stored in the order they passed dedup — which preserves the input
// concatenation order of the source components manifests — so callers
// (M7 emitter, M6 reachability) see a deterministic sequence.
type Pool struct {
	Components []manifest.Component
}

// Build merges every Components slice of every components manifest into
// a deduped pool per §11 and §12.2:
//
//  1. Concatenate `components[]` from each manifest in input order.
//  2. Run the direct-identity pass (§11, four warning cases).
//  3. Run the secondary mixed purl / no-purl cross-check (§11).
//
// All warnings are emitted via internal/diag (the §13.3 stderr channel).
// Any primary manifests in the input are ignored — callers pass the
// primary separately to [CheckPrimaryDistinct].
//
// An error is returned only for internal invariants (e.g. a component
// with an empty name that bypassed M4 validation); normal spec
// rejections are captured by the validator and should be caught before
// this function runs.
func Build(manifests []*manifest.Manifest) (*Pool, error) {
	entries, err := collectPoolEntries(manifests)
	if err != nil {
		return nil, err
	}

	// Pass 1: direct-identity dedup.
	surviving, err := directIdentityPass(entries)
	if err != nil {
		return nil, err
	}

	// Pass 2: secondary mixed purl / no-purl merge.
	merged := secondaryPass(surviving)

	out := &Pool{Components: make([]manifest.Component, 0, len(merged))}
	for _, e := range merged {
		out.Components = append(out.Components, *e.comp)
	}
	return out, nil
}

// CheckPrimaryDistinct enforces §11's "within a single SBOM emission,
// primary MUST NOT share identity with a pool component" rule. Pool is
// the post-dedup merged pool; primary is the component under the
// primary-manifest's `primary` field. A collision returns a non-nil
// error — the spec requires a hard rejection.
func CheckPrimaryDistinct(primary *manifest.Component, p *Pool) error {
	if primary == nil || p == nil {
		return nil
	}
	pi, err := Identify(primary)
	if err != nil {
		return fmt.Errorf("pool: primary identity: %w", err)
	}
	for i := range p.Components {
		ci, err := Identify(&p.Components[i])
		if err != nil {
			return fmt.Errorf("pool: pool[%d] identity: %w", i, err)
		}
		if identitiesCollide(pi, ci) {
			return fmt.Errorf("pool: primary identity %q collides with pool[%d] %q (§11)",
				pi, i, ci)
		}
	}
	return nil
}

// entry is a component together with its provenance — which manifest
// carried it and at what index. We keep the provenance so warning
// messages can point the user at the right file.
type entry struct {
	comp     *manifest.Component
	manifest string // source manifest path, may be empty for in-memory inputs
	index    int    // 0-based component index within the source manifest
}

// location renders a locator string like "path.json#/components/3" for
// diagnostics. Empty paths degrade to "<input>#/components/3" so
// warnings are still informative in tests.
func (e entry) location() string {
	path := e.manifest
	if path == "" {
		path = "<input>"
	}
	return fmt.Sprintf("%s#/components/%d", path, e.index)
}

func collectPoolEntries(manifests []*manifest.Manifest) ([]entry, error) {
	var out []entry
	for _, m := range manifests {
		if m == nil || m.Kind != manifest.KindComponents || m.Components == nil {
			continue
		}
		for i := range m.Components.Components {
			out = append(out, entry{
				comp:     copyComponent(&m.Components.Components[i]),
				manifest: m.Path,
				index:    i,
			})
		}
	}
	return out, nil
}

// copyComponent returns a shallow-copy Component so the dedup / merge
// passes can mutate the result (e.g. secondary-pass field merges)
// without poisoning the caller's input. Pointer-valued fields are
// aliased, not deep-copied — the passes treat them read-only except
// when replacing wholesale.
func copyComponent(c *manifest.Component) *manifest.Component {
	if c == nil {
		return nil
	}
	cp := *c
	return &cp
}

// identitiesCollide reports whether two identities represent the same
// §11 identity. Same-Kind entries compare on Key(); cross-Kind always
// returns false except for purl-vs-nameVersion, which §11's secondary
// pass handles separately and is not considered a "collision" here.
func identitiesCollide(a, b Identity) bool {
	if a.Kind == KindUnknown || b.Kind == KindUnknown {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	return a.Key() == b.Key()
}
