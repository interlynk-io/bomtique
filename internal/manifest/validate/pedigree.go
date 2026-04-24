// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// validatePedigree walks the pedigree substructure and enforces:
//
//   - §9.1: every ancestor carries the same identity fields as a
//     Component (name non-empty + at-least-one-of version/purl/hashes).
//   - §9.2: patch type is in the §7.4 allowlist.
//   - §9.3: the component's own top-level purl, if present, must NOT
//     canonicalise equal to any ancestor's purl.
func (v *validator) validatePedigree(parent *manifest.Component, p *manifest.Pedigree, ptr string) {
	for i := range p.Ancestors {
		a := &p.Ancestors[i]
		v.validateAncestor(a, fmt.Sprintf("%s/ancestors/%d", ptr, i))
	}
	for i := range p.Descendants {
		d := &p.Descendants[i]
		v.validateAncestor(d, fmt.Sprintf("%s/descendants/%d", ptr, i))
	}
	for i := range p.Variants {
		vn := &p.Variants[i]
		v.validateAncestor(vn, fmt.Sprintf("%s/variants/%d", ptr, i))
	}

	for i, patch := range p.Patches {
		base := fmt.Sprintf("%s/patches/%d", ptr, i)
		if strings.TrimSpace(patch.Type) == "" {
			v.add(Error{
				Kind:    ErrRequiredField,
				Pointer: base + "/type",
				Message: "patches[].type is required (§9.2)",
			})
		} else if _, ok := patchTypeValues[patch.Type]; !ok {
			v.add(Error{
				Kind:    ErrEnumValue,
				Pointer: base + "/type",
				Value:   patch.Type,
				Message: "patches[].type is not in the §7.4 allowed set",
			})
		}
	}

	v.checkPatchedPurl(parent, p, ptr)
}

func (v *validator) validateAncestor(a *manifest.Component, ptr string) {
	if strings.TrimSpace(a.Name) == "" {
		v.add(Error{
			Kind:    ErrRequiredField,
			Pointer: ptr + "/name",
			Message: "ancestor name is required (§9.1)",
		})
	}
	if !hasIdentityField(a) {
		v.add(Error{
			Kind:    ErrIdentity,
			Pointer: ptr,
			Message: "ancestor must carry at least one of version, purl, or hashes (§9.1)",
		})
	}
	if a.Purl != nil && *a.Purl != "" {
		if _, err := purl.Parse(*a.Purl); err != nil {
			v.add(Error{
				Kind:    ErrPurlParse,
				Pointer: ptr + "/purl",
				Value:   *a.Purl,
				Message: fmt.Sprintf("invalid ancestor purl: %v", err),
			})
		}
	}
}

// checkPatchedPurl enforces §9.3: when pedigree.ancestors[] is non-empty,
// the component's own purl (if set) MUST NOT canonicalise equal to any
// ancestor's purl. Canonical comparison is via internal/purl.CanonEqual.
// If either side fails to parse, that's an ErrPurlParse already raised
// elsewhere — we just skip the comparison for that pair.
func (v *validator) checkPatchedPurl(parent *manifest.Component, p *manifest.Pedigree, ptr string) {
	if len(p.Ancestors) == 0 {
		return
	}
	if parent.Purl == nil || *parent.Purl == "" {
		return
	}
	parentPurl := *parent.Purl
	for i, a := range p.Ancestors {
		if a.Purl == nil || *a.Purl == "" {
			continue
		}
		equal, err := purl.CanonEqual(parentPurl, *a.Purl)
		if err != nil {
			// ErrPurlParse already raised by validateComponent /
			// validateAncestor for the offending side; skip silently.
			continue
		}
		if equal {
			v.add(Error{
				Kind:    ErrPatchedPurl,
				Pointer: ptr + fmt.Sprintf("/ancestors/%d/purl", i),
				Value:   *a.Purl,
				Message: "component.purl canonicalises equal to pedigree.ancestors[].purl — patched components must not share identity with upstream (§9.3)",
			})
		}
	}
}
