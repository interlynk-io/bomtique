// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// PoolIndex provides O(1) resolution from a parsed depends-on Ref to a
// pool component index. It's built once per run from the deduped
// [pool.Pool] and reused across every primary in the processing set.
//
// Two lookups are kept:
//
//   - byPurl: canonical purl → component index, for RefPurl lookups.
//   - byNameVersion: "name\x00version" → component index, for
//     RefNameVersion lookups.
//
// A component without a purl is absent from byPurl but still keyed in
// byNameVersion when it carries a version. A component with only a
// name (no version, no purl) is absent from both; §11 documents that
// such components aren't first-class reachability targets.
type PoolIndex struct {
	components    []manifest.Component
	byPurl        map[string]int
	byNameVersion map[string]int
}

// NewPoolIndex builds a PoolIndex over the deduped pool. The input
// slice is borrowed — callers MUST NOT mutate it after the index is
// constructed. Identity extraction errors (empty-name components) are
// returned — validation (M4) should have caught them already.
func NewPoolIndex(p *pool.Pool) (*PoolIndex, error) {
	if p == nil {
		return &PoolIndex{}, nil
	}
	idx := &PoolIndex{
		components:    p.Components,
		byPurl:        make(map[string]int),
		byNameVersion: make(map[string]int),
	}
	for i := range p.Components {
		c := &p.Components[i]
		id, err := pool.Identify(c)
		if err != nil {
			return nil, fmt.Errorf("graph: pool[%d]: %w", i, err)
		}
		if id.Kind == pool.KindPurl {
			if _, dup := idx.byPurl[id.Purl]; !dup {
				idx.byPurl[id.Purl] = i
			}
		}
		if key := nameVersionKey(c); key != "" {
			if _, dup := idx.byNameVersion[key]; !dup {
				idx.byNameVersion[key] = i
			}
		}
	}
	return idx, nil
}

// Len is the number of pool components indexed.
func (idx *PoolIndex) Len() int {
	return len(idx.components)
}

// Components returns the underlying pool slice. Borrowed — callers
// MUST NOT mutate.
func (idx *PoolIndex) Components() []manifest.Component {
	return idx.components
}

// Resolve returns the pool index that matches ref, or (-1, false) if
// no component matches. §10.2 forms drive lookup:
//
//   - RefPurl → exact match on canonical purl.
//   - RefNameVersion → byte-exact match on (name, version).
//
// Callers surface (-1, false) via the §10.3 warning channel and drop
// the edge.
func (idx *PoolIndex) Resolve(ref Ref) (int, bool) {
	switch ref.Kind {
	case RefPurl:
		i, ok := idx.byPurl[ref.Purl]
		return i, ok
	case RefNameVersion:
		key := ref.Name + "\x00" + ref.Version
		i, ok := idx.byNameVersion[key]
		return i, ok
	}
	return -1, false
}

// nameVersionKey composes the byNameVersion key. Component.Name is
// trimmed on both sides; components without a version are keyed empty
// and not inserted.
func nameVersionKey(c *manifest.Component) string {
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
