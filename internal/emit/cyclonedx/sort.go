// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import "sort"

// sortBOM applies spec §15.2's ordering rules across the assembled BOM:
//
//   - `components[]` by bom-ref ascending.
//   - `dependencies[]` by ref ascending; each `dependsOn` array by ref
//     ascending (the spec requires the inner array; sorting the outer
//     array is an additional byte-identity guarantee).
//   - Per-component `hashes` by (algorithm, value), `externalReferences`
//     by (type, url). Done for both metadata.component (primary) and
//     every entry of components[].
//
// Sorting is applied to the Go struct before json.Marshal runs so the
// serialised output is byte-stable without needing a post-pass
// canonicalisation.
func sortBOM(bom *cdxBOM) {
	if bom == nil {
		return
	}

	if bom.Metadata != nil && bom.Metadata.Component != nil {
		sortComponent(bom.Metadata.Component)
	}
	for i := range bom.Components {
		sortComponent(&bom.Components[i])
	}

	sort.SliceStable(bom.Components, func(i, j int) bool {
		return bom.Components[i].BOMRef < bom.Components[j].BOMRef
	})

	for i := range bom.Dependencies {
		sort.Strings(bom.Dependencies[i].DependsOn)
	}
	sort.SliceStable(bom.Dependencies, func(i, j int) bool {
		return bom.Dependencies[i].Ref < bom.Dependencies[j].Ref
	})
}

// sortComponent sorts the inner arrays of a single component — hashes
// and externalReferences — per §15.2. Nested pedigree components reuse
// the same rule so ancestor / descendant / variant sub-components are
// internally ordered too.
func sortComponent(c *cdxComponent) {
	if c == nil {
		return
	}

	sort.SliceStable(c.Hashes, func(i, j int) bool {
		if c.Hashes[i].Alg != c.Hashes[j].Alg {
			return c.Hashes[i].Alg < c.Hashes[j].Alg
		}
		return c.Hashes[i].Content < c.Hashes[j].Content
	})

	sort.SliceStable(c.ExternalReferences, func(i, j int) bool {
		if c.ExternalReferences[i].Type != c.ExternalReferences[j].Type {
			return c.ExternalReferences[i].Type < c.ExternalReferences[j].Type
		}
		return c.ExternalReferences[i].URL < c.ExternalReferences[j].URL
	})

	if c.Pedigree != nil {
		for i := range c.Pedigree.Ancestors {
			sortComponent(&c.Pedigree.Ancestors[i])
		}
		for i := range c.Pedigree.Descendants {
			sortComponent(&c.Pedigree.Descendants[i])
		}
		for i := range c.Pedigree.Variants {
			sortComponent(&c.Pedigree.Variants[i])
		}
	}
}
