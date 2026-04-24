// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"errors"
	"fmt"
	"sort"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// ErrMultiPrimaryMissingDepsOn signals a §10.4 hard error — a multi-
// primary processing set where at least one primary omits depends-on
// or carries an empty array. (M4 validation catches this earlier; the
// error exists for callers that bypass validation.)
var ErrMultiPrimaryMissingDepsOn = errors.New("multi-primary processing set requires non-empty depends-on on every primary (§10.4)")

// Reachability holds the per-primary result of a §10.4 reachability
// pass. The Components slice is a deterministic subset of the pool —
// sorted ascending by pool index — that the emitter writes into the
// SBOM. Unreachable lists pool indices that were NOT reached from this
// primary (per-primary warnings have already fired against diag by the
// time Reachability is returned).
type Reachability struct {
	Primary         *manifest.Component
	Components      []int // pool indices, sorted ascending
	Unreachable     []int // pool indices not reached from this primary, sorted
	UnresolvedRoots []Ref // primary.depends-on entries that failed to resolve
}

// PerPrimary runs the §10.4 reachability pass for one primary against
// a pre-built [PoolIndex]. `multiPrimary` toggles the single-vs-multi
// semantics:
//
//   - Single-primary + empty/absent depends-on: every pool component
//     becomes a direct dep (convenience rule).
//   - Single-primary + non-empty: closure only; per-primary warnings
//     for unreachable pool components.
//   - Multi-primary: depends-on MUST be non-empty — else hard error.
//     Closure + per-primary warnings.
//
// Orphan-across-all warnings are NOT emitted here; they belong to the
// full-processing-set pass ([ForProcessingSet]) because they depend on
// the union of every primary's reach.
func PerPrimary(idx *PoolIndex, primary *manifest.Component, multiPrimary bool) (*Reachability, error) {
	if primary == nil {
		return nil, errors.New("graph: nil primary")
	}
	if idx == nil {
		idx = &PoolIndex{}
	}

	depsOn := primary.DependsOn
	r := &Reachability{Primary: primary}

	if len(depsOn) == 0 {
		if multiPrimary {
			return nil, fmt.Errorf("%w: primary %q", ErrMultiPrimaryMissingDepsOn, primary.Name)
		}
		// Single-primary convenience: every pool component is a direct dep.
		r.Components = make([]int, 0, idx.Len())
		for i := 0; i < idx.Len(); i++ {
			r.Components = append(r.Components, i)
		}
		// Nothing unreachable by definition. No warnings fire.
		return r, nil
	}

	roots, err := parseDependsOn(depsOn, primary)
	if err != nil {
		return nil, err
	}

	closure := TransitiveClosure(idx, roots)
	r.Components = closure.Reached
	r.UnresolvedRoots = closure.Unresolved

	// Primary-side root-warnings: closure.Unresolved carries refs the
	// primary listed but the pool doesn't contain. Warn per §10.3 here
	// so the message can name the primary.
	primaryLabel := nameVersionLabel(primary)
	for _, ur := range closure.Unresolved {
		diag.Warn("graph: unresolved depends-on %q on primary %q — dropping edge, keeping primary (§10.3)",
			ur.Raw, primaryLabel)
	}

	r.Unreachable = computeUnreachable(idx.Len(), r.Components)
	for _, i := range r.Unreachable {
		comp := &idx.Components()[i]
		diag.Warn("graph: pool component %q not reachable from primary %q — omitting from its SBOM (§10.4)",
			nameVersionLabel(comp), primaryLabel)
	}

	return r, nil
}

// ForProcessingSet runs [PerPrimary] against every primary in the
// processing set, then emits the §10.4 final warning: one line per
// pool component that is not reached from ANY primary.
//
// `primaries` must be non-empty; the caller (the CLI / validator) has
// already enforced §12.1. The multi-vs-single toggle is derived from
// `len(primaries)`.
func ForProcessingSet(p *pool.Pool, primaries []*manifest.Component) ([]*Reachability, error) {
	if len(primaries) == 0 {
		return nil, errors.New("graph: no primaries — §12.1 requires at least one")
	}
	idx, err := NewPoolIndex(p)
	if err != nil {
		return nil, err
	}
	multi := len(primaries) > 1

	results := make([]*Reachability, 0, len(primaries))
	reachedAnywhere := make(map[int]struct{})
	for _, pr := range primaries {
		r, err := PerPrimary(idx, pr, multi)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
		for _, i := range r.Components {
			reachedAnywhere[i] = struct{}{}
		}
	}

	// Orphan-across-all warning: emitted once per run, not per SBOM.
	orphans := make([]int, 0)
	for i := 0; i < idx.Len(); i++ {
		if _, ok := reachedAnywhere[i]; !ok {
			orphans = append(orphans, i)
		}
	}
	sort.Ints(orphans)
	for _, i := range orphans {
		comp := &idx.Components()[i]
		diag.Warn("graph: pool component %q not reachable from any primary in this run (§10.4)",
			nameVersionLabel(comp))
	}

	return results, nil
}

// parseDependsOn parses every string in a primary's depends-on into a
// Ref. The first parse error short-circuits with context about which
// primary and entry failed.
func parseDependsOn(depsOn []string, primary *manifest.Component) ([]Ref, error) {
	out := make([]Ref, 0, len(depsOn))
	for _, raw := range depsOn {
		ref, err := ParseRef(raw)
		if err != nil {
			return nil, fmt.Errorf("graph: primary %q: %w", nameVersionLabel(primary), err)
		}
		out = append(out, ref)
	}
	return out, nil
}

// computeUnreachable produces the sorted complement of `reached` over
// [0, total). Both sides must already be sorted; `reached` is the
// Components slice from a Reachability run.
func computeUnreachable(total int, reached []int) []int {
	if total == 0 {
		return nil
	}
	inReached := make(map[int]struct{}, len(reached))
	for _, i := range reached {
		inReached[i] = struct{}{}
	}
	out := make([]int, 0, total-len(reached))
	for i := 0; i < total; i++ {
		if _, ok := inReached[i]; !ok {
			out = append(out, i)
		}
	}
	return out
}
