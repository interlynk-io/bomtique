// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// validateComponent checks one Component against the §6.1, §6.2, §6.3,
// §6.4, §7, §8, and §9 rules. `ptr` is the JSON pointer of this
// component (e.g. `/primary`, `/components/3`). `role` tells us whether
// fields the spec says are "ignored on primaries" or "primary-only" are
// in the right place.
func (v *validator) validateComponent(c *manifest.Component, ptr string, role componentRole) {
	if c == nil {
		v.add(Error{Kind: ErrInternal, Pointer: ptr, Message: "nil component"})
		return
	}

	if strings.TrimSpace(c.Name) == "" {
		v.add(Error{
			Kind:    ErrRequiredField,
			Pointer: ptr + "/name",
			Message: "component name is required (§6.1)",
		})
	}

	if !hasIdentityField(c) {
		v.add(Error{
			Kind:    ErrIdentity,
			Pointer: ptr,
			Message: "component must carry at least one of version, purl, or hashes (§6.1)",
		})
	}

	if c.Type != nil && *c.Type != "" {
		if _, ok := componentTypeValues[*c.Type]; !ok {
			v.add(Error{
				Kind:    ErrEnumValue,
				Pointer: ptr + "/type",
				Value:   *c.Type,
				Message: "component type is not in the §7.1 allowed set",
			})
		}
	}
	if c.Scope != nil && *c.Scope != "" {
		if _, ok := scopeValues[*c.Scope]; !ok {
			v.add(Error{
				Kind:    ErrEnumValue,
				Pointer: ptr + "/scope",
				Value:   *c.Scope,
				Message: "scope is not in the §7.2 allowed set",
			})
		}
	}

	if c.Supplier != nil {
		if strings.TrimSpace(c.Supplier.Name) == "" {
			v.add(Error{
				Kind:    ErrRequiredField,
				Pointer: ptr + "/supplier/name",
				Message: "supplier.name must be non-empty when supplier is present (§6.2)",
			})
		}
	}

	if c.Purl != nil && *c.Purl != "" {
		if _, err := purl.Parse(*c.Purl); err != nil {
			v.add(Error{
				Kind:    ErrPurlParse,
				Pointer: ptr + "/purl",
				Value:   *c.Purl,
				Message: fmt.Sprintf("invalid purl: %v", err),
			})
		}
	}

	for i, ref := range c.ExternalReferences {
		base := fmt.Sprintf("%s/external_references/%d", ptr, i)
		if strings.TrimSpace(ref.Type) == "" {
			v.add(Error{
				Kind:    ErrRequiredField,
				Pointer: base + "/type",
				Message: "external_references[].type is required (§6.2)",
			})
		} else if _, ok := externalRefTypeValues[ref.Type]; !ok {
			v.add(Error{
				Kind:    ErrEnumValue,
				Pointer: base + "/type",
				Value:   ref.Type,
				Message: "external_references[].type is not in the §7.3 allowed set",
			})
		}
		if strings.TrimSpace(ref.URL) == "" {
			v.add(Error{
				Kind:    ErrRequiredField,
				Pointer: base + "/url",
				Message: "external_references[].url is required (§6.2)",
			})
		}
	}

	for i, lc := range c.Lifecycles {
		base := fmt.Sprintf("%s/lifecycles/%d", ptr, i)
		if _, ok := lifecyclePhaseValues[lc.Phase]; !ok {
			v.add(Error{
				Kind:    ErrEnumValue,
				Pointer: base + "/phase",
				Value:   lc.Phase,
				Message: "lifecycles[].phase is not in the §7.5 allowed set",
			})
		}
	}

	if c.License != nil {
		v.validateLicense(c.License, ptr+"/license")
	}

	for i, h := range c.Hashes {
		v.validateHashEntry(&h, fmt.Sprintf("%s/hashes/%d", ptr, i))
	}

	if c.Pedigree != nil {
		v.validatePedigree(c, c.Pedigree, ptr+"/pedigree")
	}

	_ = role // reserved for §5.3 ignored-field warnings in a later milestone
}

// hasIdentityField returns true when the Component carries at least one
// of the three §6.1 identity-bearing fields (post-whitespace trim /
// emptiness check).
func hasIdentityField(c *manifest.Component) bool {
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return true
	}
	if c.Purl != nil && strings.TrimSpace(*c.Purl) != "" {
		return true
	}
	if len(c.Hashes) > 0 {
		return true
	}
	return false
}
