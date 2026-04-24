// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// ProcessingSet validates rules that span the run (§10.4, §12.1, §5.2).
// It also invokes [Manifest] on each input so a single call gives the
// caller the full picture. An empty return slice means every rule
// passes.
//
// Rules enforced here:
//
//   - §12.1: a processing set MUST contain at least one primary manifest.
//   - §10.4: when two or more primary manifests are present, each MUST
//     carry a non-empty depends-on array. A single primary is allowed
//     to omit depends-on (the "drop one primary + one components file in
//     a tree" convenience rule).
//   - §5.2: each components manifest MUST carry a non-empty components[]
//     array (already enforced per-file by Manifest; repeated here for
//     processing-set-level callers that skip the per-file pass).
func ProcessingSet(manifests []*manifest.Manifest, opts Options) []Error {
	var errs []Error

	var primaries []*manifest.Manifest
	for _, m := range manifests {
		if m == nil {
			continue
		}
		if m.Kind == manifest.KindPrimary {
			primaries = append(primaries, m)
		}
	}

	// Per-file pass first — surfaces structural / semantic errors before
	// cross-file issues, which makes diagnosis easier.
	for _, m := range manifests {
		if m == nil {
			continue
		}
		errs = append(errs, Manifest(m, opts)...)
	}

	if len(primaries) == 0 {
		errs = append(errs, Error{
			Kind:    ErrProcessingSet,
			Message: "processing set must contain at least one primary manifest (§12.1)",
		})
	}

	if len(primaries) >= 2 {
		for _, pm := range primaries {
			if pm.Primary == nil {
				continue // internal invariant already flagged
			}
			if len(pm.Primary.Primary.DependsOn) == 0 {
				errs = append(errs, Error{
					Path:    pm.Path,
					Pointer: "/primary/depends-on",
					Kind:    ErrDependsOn,
					Message: "multi-primary processing set: every primary must carry a non-empty depends-on array (§10.4)",
				})
			}
		}
	}

	// §5.2 (empty components[]) is enforced by the per-file pass above;
	// no separate cross-file handling is needed.
	return errs
}
