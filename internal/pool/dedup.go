// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package pool

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// directIdentityPass implements §11's first dedup pass. It runs in
// input order and produces the list of entries that survive dedup,
// plus warnings for each of the four §11 cases.
//
// Cases:
//
//  1. duplicate purl → warn, drop the later entry.
//  2. same (name, version) with differing purls → both kept + warn
//     (§11 treats them as distinct; the warning flags a likely upstream
//     collision).
//  3. no-purl with matching (name, version) → warn, drop the later.
//  4. name-only match (no purl, no version on either side) → warn,
//     drop the later.
func directIdentityPass(entries []entry) ([]entry, error) {
	type nvRecord struct {
		firstIdx int    // index into `entries` of the first occurrence
		purl     string // canonical purl if any, else ""
	}

	byPurl := make(map[string]int)             // canonical purl → first index in entries
	byNameVersion := make(map[string]nvRecord) // "name\x00version" → record
	byName := make(map[string]int)             // name-only → first index

	kept := make([]entry, 0, len(entries))

	for i := range entries {
		e := entries[i]
		id, err := Identify(e.comp)
		if err != nil {
			return nil, fmt.Errorf("pool: entry %s: %w", e.location(), err)
		}

		switch id.Kind {
		case KindPurl:
			if firstIdx, ok := byPurl[id.Purl]; ok {
				// Case 1: duplicate purl.
				diag.Warn("pool: duplicate purl %q — keeping %s, dropping %s (§11)",
					id.Purl,
					entries[firstIdx].location(),
					e.location())
				continue
			}
			byPurl[id.Purl] = i

			// Warn on case 2 if another component carries the same
			// (name, version) with a different purl.
			key := nvKey(e.comp)
			if key != "" {
				if rec, ok := byNameVersion[key]; ok && rec.purl != "" && rec.purl != id.Purl {
					diag.Warn("pool: same (name, version) %q declared with differing purls %q and %q — treating as distinct (§11)",
						humanNV(key), rec.purl, id.Purl)
				} else if !ok {
					byNameVersion[key] = nvRecord{firstIdx: i, purl: id.Purl}
				}
			}

		case KindNameVersion:
			key := nvKey(e.comp)
			if rec, ok := byNameVersion[key]; ok {
				if rec.purl == "" {
					// Case 3: no-purl + no-purl duplicate.
					diag.Warn("pool: duplicate (name, version) %q with no purl on either side — keeping %s, dropping %s (§11)",
						humanNV(key),
						entries[rec.firstIdx].location(),
						e.location())
					continue
				}
				// Cross case: purl-bearing already in the index.
				// The direct pass keeps both; the secondary pass may
				// later merge this no-purl entry into the purl-bearing
				// one. Don't drop, don't warn yet (secondary warns).
			} else {
				byNameVersion[key] = nvRecord{firstIdx: i, purl: ""}
			}

		case KindNameOnly:
			if firstIdx, ok := byName[id.Name]; ok {
				// Case 4: name-only duplicate.
				diag.Warn("pool: duplicate name-only component %q — keeping %s, dropping %s (§11)",
					id.Name,
					entries[firstIdx].location(),
					e.location())
				continue
			}
			byName[id.Name] = i
		}

		kept = append(kept, e)
		_ = id // id was computed for side-effect warnings above
	}

	return kept, nil
}

// secondaryPass implements §11's cross-check for mixed purl / no-purl
// duplicates. It builds an index of purl-bearing entries keyed by
// `(name, version)`, then merges any no-purl entry whose (name,
// version) matches into the purl-bearing entry. The purl-bearing
// component's purl wins; no-purl fields that the purl-bearing record
// lacks are filled in. Field conflicts warn per field, purl-bearing
// wins.
//
// Components whose identity is name-only are not matched; §11 calls
// this out explicitly.
func secondaryPass(entries []entry) []entry {
	// Index purl-bearing entries by (name, version).
	type purlEntry struct {
		target int // index into entries
	}
	byNV := make(map[string]purlEntry)
	for i := range entries {
		e := entries[i]
		id, _ := Identify(e.comp)
		if id.Kind != KindPurl {
			continue
		}
		key := nvKey(e.comp)
		if key == "" {
			continue // no version on the purl-bearing component
		}
		if _, ok := byNV[key]; ok {
			// Multiple purl-bearing components with the same (name,
			// version): direct-pass already warned about case 2.
			// Secondary pass can't pick a unique merge target, so we
			// leave them alone.
			continue
		}
		byNV[key] = purlEntry{target: i}
	}

	// Walk entries; merge no-purl matches into their purl-bearing
	// counterparts; drop merged entries from the output.
	dropped := make(map[int]bool)
	for i := range entries {
		e := entries[i]
		id, _ := Identify(e.comp)
		if id.Kind != KindNameVersion {
			continue
		}
		key := nvKey(e.comp)
		pe, ok := byNV[key]
		if !ok {
			continue
		}
		if pe.target == i {
			continue // shouldn't happen — purl-bearing vs no-purl
		}

		target := entries[pe.target].comp
		source := e.comp
		mergeNoPurlInto(target, source, pe.target, i, entries)
		dropped[i] = true
	}

	if len(dropped) == 0 {
		return entries
	}
	out := make([]entry, 0, len(entries)-len(dropped))
	for i, e := range entries {
		if dropped[i] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// mergeNoPurlInto merges source (a no-purl component) into target (a
// purl-bearing component) per §11's secondary-pass rule. It emits one
// "merging" warning, plus one warning per field where both sides carry
// a value that disagrees. Target's values always win on conflict.
func mergeNoPurlInto(target, source *manifest.Component, targetIdx, sourceIdx int, entries []entry) {
	diag.Warn("pool: merging no-purl component %s into purl-bearing %s — keeping %q purl (§11)",
		entries[sourceIdx].location(), entries[targetIdx].location(), derefOrEmpty(target.Purl))

	loc := fmt.Sprintf("%s (merged from %s)", entries[targetIdx].location(), entries[sourceIdx].location())

	// Scalar-valued pointer fields: keep target's if non-empty; copy
	// source's if target is empty. If both are set and they differ,
	// keep target's and warn.
	target.BOMRef = mergePtrString(target.BOMRef, source.BOMRef, loc, "bom-ref")
	target.Version = mergePtrString(target.Version, source.Version, loc, "version")
	target.Type = mergePtrString(target.Type, source.Type, loc, "type")
	target.Description = mergePtrString(target.Description, source.Description, loc, "description")
	target.CPE = mergePtrString(target.CPE, source.CPE, loc, "cpe")
	target.Scope = mergePtrString(target.Scope, source.Scope, loc, "scope")

	// Object-valued fields: keep target's when set; fill from source
	// when target is unset. Conflict warns.
	if target.Supplier == nil {
		target.Supplier = source.Supplier
	} else if source.Supplier != nil && !suppliersEqual(target.Supplier, source.Supplier) {
		diag.Warn("pool: field conflict on %s: supplier — keeping purl-bearing's value (§11)", loc)
	}
	if target.License == nil {
		target.License = source.License
	} else if source.License != nil && !licensesEqual(target.License, source.License) {
		diag.Warn("pool: field conflict on %s: license — keeping purl-bearing's value (§11)", loc)
	}
	if target.Pedigree == nil {
		target.Pedigree = source.Pedigree
	} else if source.Pedigree != nil {
		diag.Warn("pool: field conflict on %s: pedigree — keeping purl-bearing's value (§11)", loc)
	}

	// Slice-valued fields: if target's is empty/nil, take source's; if
	// both are non-empty and they differ, keep target's and warn.
	target.Hashes = mergeSlice(target.Hashes, source.Hashes, loc, "hashes", hashesEqual)
	target.ExternalReferences = mergeSlice(target.ExternalReferences, source.ExternalReferences, loc, "external_references", externalRefsEqual)
	target.DependsOn = mergeStringSlice(target.DependsOn, source.DependsOn, loc, "depends-on")
	target.Tags = mergeStringSlice(target.Tags, source.Tags, loc, "tags")
	target.Lifecycles = mergeSlice(target.Lifecycles, source.Lifecycles, loc, "lifecycles", lifecyclesEqual)
}

func mergePtrString(target, source *string, loc, field string) *string {
	if target != nil && *target != "" {
		if source != nil && *source != "" && *source != *target {
			diag.Warn("pool: field conflict on %s: %s — keeping purl-bearing's value %q (§11)", loc, field, *target)
		}
		return target
	}
	return source
}

func mergeSlice[T any](target, source []T, loc, field string, eq func(a, b []T) bool) []T {
	if len(target) == 0 {
		return source
	}
	if len(source) > 0 && !eq(target, source) {
		diag.Warn("pool: field conflict on %s: %s — keeping purl-bearing's value (§11)", loc, field)
	}
	return target
}

func mergeStringSlice(target, source []string, loc, field string) []string {
	if len(target) == 0 {
		return source
	}
	if len(source) > 0 && !stringSlicesEqual(target, source) {
		diag.Warn("pool: field conflict on %s: %s — keeping purl-bearing's value (§11)", loc, field)
	}
	return target
}

// nvKey builds the "name\x00version" key used for (name, version)
// indexing. Returns "" if either side is missing — callers use that to
// skip name-only components from the secondary pass.
func nvKey(c *manifest.Component) string {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return ""
	}
	if c.Version == nil {
		return ""
	}
	version := strings.TrimSpace(*c.Version)
	if version == "" {
		return ""
	}
	return name + "\x00" + version
}

// humanNV turns a nvKey back into "name@version" for error messages.
func humanNV(key string) string {
	nul := strings.IndexByte(key, '\x00')
	if nul < 0 {
		return key
	}
	return key[:nul] + "@" + key[nul+1:]
}

func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// --- helpers for equality checks used by mergeSlice ------------------------

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hashesEqual(a, b []manifest.Hash) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Algorithm != b[i].Algorithm ||
			!ptrStringEq(a[i].Value, b[i].Value) ||
			!ptrStringEq(a[i].File, b[i].File) ||
			!ptrStringEq(a[i].Path, b[i].Path) ||
			!stringSlicesEqual(a[i].Extensions, b[i].Extensions) {
			return false
		}
	}
	return true
}

func externalRefsEqual(a, b []manifest.ExternalRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].URL != b[i].URL || !ptrStringEq(a[i].Comment, b[i].Comment) {
			return false
		}
	}
	return true
}

func lifecyclesEqual(a, b []manifest.Lifecycle) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Phase != b[i].Phase {
			return false
		}
	}
	return true
}

func suppliersEqual(a, b *manifest.Supplier) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name && ptrStringEq(a.Email, b.Email) && ptrStringEq(a.URL, b.URL)
}

func licensesEqual(a, b *manifest.License) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Expression != b.Expression || len(a.Texts) != len(b.Texts) {
		return false
	}
	for i := range a.Texts {
		if a.Texts[i].ID != b.Texts[i].ID ||
			!ptrStringEq(a.Texts[i].Text, b.Texts[i].Text) ||
			!ptrStringEq(a.Texts[i].File, b.Texts[i].File) {
			return false
		}
	}
	return true
}

func ptrStringEq(a, b *string) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
