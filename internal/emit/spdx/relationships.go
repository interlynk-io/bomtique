// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"sort"

	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// buildRelationships produces the SPDX relationships array:
//
//   - one DESCRIBES from SPDXRef-DOCUMENT to the primary's package;
//   - one DEPENDS_ON per resolved `depends-on` edge, sourced on both
//     the primary and every pool component's depends-on.
//
// Unresolved edges are skipped silently — M6's graph pass has already
// emitted §10.3 warnings for them.
func buildRelationships(in EmitInput, primaryID string, poolIDs []string) []spdxRelation {
	relations := []spdxRelation{{
		SPDXElementID:      documentSPDXID,
		RelationshipType:   relationshipDescribes,
		RelatedSPDXElement: primaryID,
	}}

	idx := buildReachableIndex(in, poolIDs)

	relations = appendDependsOn(relations, primaryID, in.Primary.DependsOn, idx)
	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		relations = appendDependsOn(relations, poolIDs[i], rc.Component.DependsOn, idx)
	}

	// Deterministic output: DESCRIBES stays first; DEPENDS_ON entries
	// sort by (source, target) so two runs produce byte-identical
	// relationship arrays.
	sort.SliceStable(relations[1:], func(i, j int) bool {
		a, b := relations[i+1], relations[j+1]
		if a.SPDXElementID != b.SPDXElementID {
			return a.SPDXElementID < b.SPDXElementID
		}
		return a.RelatedSPDXElement < b.RelatedSPDXElement
	})

	return relations
}

type reachableIndex struct {
	byPurl        map[string]string
	byNameVersion map[string]string
}

func buildReachableIndex(in EmitInput, poolIDs []string) reachableIndex {
	idx := reachableIndex{
		byPurl:        make(map[string]string),
		byNameVersion: make(map[string]string),
	}
	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		spdxID := poolIDs[i]
		if rc.Component.Purl != nil && *rc.Component.Purl != "" {
			if p, err := purl.Parse(*rc.Component.Purl); err == nil {
				idx.byPurl[p.String()] = spdxID
			}
		}
		if rc.Component.Version != nil && *rc.Component.Version != "" {
			key := rc.Component.Name + "\x00" + *rc.Component.Version
			idx.byNameVersion[key] = spdxID
		}
	}
	return idx
}

func appendDependsOn(relations []spdxRelation, fromID string, depsOn []string, idx reachableIndex) []spdxRelation {
	for _, raw := range depsOn {
		ref, err := graph.ParseRef(raw)
		if err != nil {
			continue
		}
		var target string
		switch ref.Kind {
		case graph.RefPurl:
			target = idx.byPurl[ref.Purl]
		case graph.RefNameVersion:
			target = idx.byNameVersion[ref.Name+"\x00"+ref.Version]
		}
		if target == "" {
			continue
		}
		relations = append(relations, spdxRelation{
			SPDXElementID:      fromID,
			RelationshipType:   relationshipDependsOn,
			RelatedSPDXElement: target,
		})
	}
	return relations
}
