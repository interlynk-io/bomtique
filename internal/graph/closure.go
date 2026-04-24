// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"sort"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// Closure is the result of a transitive-reachability walk from a set of
// root references through a [PoolIndex]. Reached holds the pool-indexed
// components in byte-wise sorted order (deterministic across runs);
// Resolved is the subset of root references that hit a pool component;
// Unresolved captures root references that failed to resolve so the
// caller can surface §10.3 warnings with the right context (e.g.
// primary name).
type Closure struct {
	Reached    []int // pool indices, sorted ascending
	Resolved   []Ref // roots that matched a pool component
	Unresolved []Ref // roots that failed to resolve
}

// Contains reports whether the closure includes pool index i.
func (c *Closure) Contains(i int) bool {
	// Small N; linear scan is fine. Callers that need set semantics
	// should materialise a map themselves.
	for _, x := range c.Reached {
		if x == i {
			return true
		}
	}
	return false
}

// TransitiveClosure computes every pool index reachable from the root
// references, walking each reached component's own `depends-on` array.
// Unresolved edges below the roots go through diag.Warn (§10.3 says
// "MUST produce a warning; drop the edge; preserve the referring
// component"); unresolved *root* edges are returned in Closure.Unresolved
// so the caller can attribute them to the primary being emitted.
//
// Cycles in the graph are tolerated — the visited-set guarantees
// termination.
//
// `originLabel` is inserted into intra-closure warnings to identify the
// referring component (e.g. `"libfoo@1.0.0"`). Leaf-level warnings
// include it so operators know which component held the bad edge.
func TransitiveClosure(idx *PoolIndex, roots []Ref) *Closure {
	c := &Closure{}
	if idx == nil || idx.Len() == 0 {
		c.Unresolved = append(c.Unresolved, roots...)
		return c
	}

	visited := make(map[int]struct{})
	queue := make([]int, 0, len(roots))

	// Seed from roots. Root-level unresolved entries are recorded on
	// the Closure; caller warns with its own context (primary name).
	for _, r := range roots {
		i, ok := idx.Resolve(r)
		if !ok {
			c.Unresolved = append(c.Unresolved, r)
			continue
		}
		c.Resolved = append(c.Resolved, r)
		if _, seen := visited[i]; !seen {
			visited[i] = struct{}{}
			queue = append(queue, i)
		}
	}

	// BFS through depends-on of each reached component.
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		comp := &idx.Components()[current]
		for _, entry := range comp.DependsOn {
			ref, err := ParseRef(entry)
			if err != nil {
				// §10.2 makes malformed depends-on entries a hard
				// error at parse time. By the time we reach M6 the
				// validator (M4) should have rejected it; warn
				// defensively if we see one slip through.
				diag.Warn("graph: malformed depends-on entry %q on component %q — skipping (§10.2)",
					entry, nameVersionLabel(comp))
				continue
			}
			next, ok := idx.Resolve(ref)
			if !ok {
				diag.Warn("graph: unresolved depends-on %q on component %q — dropping edge, keeping component (§10.3)",
					entry, nameVersionLabel(comp))
				continue
			}
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	c.Reached = make([]int, 0, len(visited))
	for i := range visited {
		c.Reached = append(c.Reached, i)
	}
	sort.Ints(c.Reached)
	return c
}

// nameVersionLabel renders a short "name@version" label for warnings.
// Falls back to name only when version is absent, and to "<unnamed>"
// for a nameless component (which §6.1 validation should already have
// rejected).
func nameVersionLabel(c *manifest.Component) string {
	if c == nil {
		return "<nil>"
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = "<unnamed>"
	}
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return name + "@" + strings.TrimSpace(*c.Version)
	}
	return name
}
