// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// canonicalPurl returns the canonical form of a purl string so the
// emitter's depends-on index compares byte-exact against §10.2 refs
// that have already been canonicalised by graph.ParseRef.
func canonicalPurl(raw string) (string, error) {
	p, err := purl.Parse(raw)
	if err != nil {
		return "", err
	}
	return p.String(), nil
}

// buildDependencies produces the §14.1 `dependencies[]` array for one
// emission. The primary is always the first entry; every reachable
// pool component follows in input order. `dependsOn` entries resolve
// the manifest's depends-on references to pool bom-refs, silently
// dropping anything that doesn't resolve (M6's graph layer has
// already warned about unresolved edges).
func buildDependencies(in EmitInput, refs *bomRefs) []cdxDependency {
	if refs == nil {
		return nil
	}
	poolByNV := buildReachableIndex(in, refs)
	out := make([]cdxDependency, 0, 1+len(in.Reachable))

	primaryDeps := resolveDepsOn(in.Primary.DependsOn, poolByNV)
	out = append(out, cdxDependency{Ref: refs.primary, DependsOn: primaryDeps})

	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		deps := resolveDepsOn(rc.Component.DependsOn, poolByNV)
		out = append(out, cdxDependency{Ref: refs.pool[i], DependsOn: deps})
	}
	return out
}

// reachableIndex maps both canonical purls and "name\x00version" keys
// to the corresponding reachable-component bom-ref. Both lookups are
// required because §10.2 refs can take either form.
type reachableIndex struct {
	byPurl        map[string]string
	byNameVersion map[string]string
}

func buildReachableIndex(in EmitInput, refs *bomRefs) reachableIndex {
	idx := reachableIndex{
		byPurl:        make(map[string]string),
		byNameVersion: make(map[string]string),
	}
	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		bomRef := refs.pool[i]
		if rc.Component.Purl != nil && *rc.Component.Purl != "" {
			if canon, err := canonicalPurl(*rc.Component.Purl); err == nil {
				idx.byPurl[canon] = bomRef
			}
		}
		if key := componentNVKey(rc.Component.Name, rc.Component.Version); key != "" {
			idx.byNameVersion[key] = bomRef
		}
	}
	return idx
}

// resolveDepsOn parses every raw depends-on entry, resolves it against
// the reachable-component index, and returns the corresponding
// bom-refs. Entries that don't resolve are dropped silently — M6
// already emitted §10.3 warnings for them.
func resolveDepsOn(raw []string, idx reachableIndex) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		ref, err := graph.ParseRef(r)
		if err != nil {
			continue
		}
		switch ref.Kind {
		case graph.RefPurl:
			if bomRef, ok := idx.byPurl[ref.Purl]; ok {
				out = append(out, bomRef)
			}
		case graph.RefNameVersion:
			key := ref.Name + "\x00" + ref.Version
			if bomRef, ok := idx.byNameVersion[key]; ok {
				out = append(out, bomRef)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func componentNVKey(name string, version *string) string {
	if name == "" || version == nil || *version == "" {
		return ""
	}
	return name + "\x00" + *version
}
