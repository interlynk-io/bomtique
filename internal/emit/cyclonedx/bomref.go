// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// bomRefs holds the per-emission bom-ref assignments.
type bomRefs struct {
	primary string
	pool    []string // indexed by EmitInput.Reachable index
}

// assignBOMRefs derives a bom-ref for the primary and every reachable
// pool component following §15.1's precedence:
//
//  1. Explicit `bom-ref` on the component, if non-empty.
//  2. Canonical purl form (via internal/purl.Parse+String).
//  3. `pkg:generic/<pct-name>@<version>` with RFC 3986 §2.3 unreserved
//     percent-encoding on name. `@version` is dropped when version is
//     absent.
//
// Duplicates across the emission are a hard error — CycloneDX requires
// bom-refs to be unique within the document.
func assignBOMRefs(in EmitInput) (*bomRefs, error) {
	seen := make(map[string]string, 1+len(in.Reachable))
	refs := &bomRefs{pool: make([]string, len(in.Reachable))}

	// Primary first so a collision gets attributed correctly.
	if in.Primary == nil {
		return nil, fmt.Errorf("cyclonedx: nil primary")
	}
	pref, err := deriveBOMRef(in.Primary)
	if err != nil {
		return nil, fmt.Errorf("cyclonedx: primary %q: %w", in.Primary.Name, err)
	}
	seen[pref] = "primary"
	refs.primary = pref

	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		ref, err := deriveBOMRef(rc.Component)
		if err != nil {
			return nil, fmt.Errorf("cyclonedx: pool[%d] %q: %w", i, rc.Component.Name, err)
		}
		if prev, dup := seen[ref]; dup {
			return nil, fmt.Errorf("cyclonedx: bom-ref collision %q between %s and pool[%d] %q (§15.1)",
				ref, prev, i, rc.Component.Name)
		}
		seen[ref] = fmt.Sprintf("pool[%d] %q", i, rc.Component.Name)
		refs.pool[i] = ref
	}

	return refs, nil
}

// deriveBOMRef implements §15.1's three-rung precedence for one
// component. Returns a non-empty ref or an error (the error surfaces
// when a declared purl fails to canonicalise, which should have been
// caught by M4).
func deriveBOMRef(c *manifest.Component) (string, error) {
	if c.BOMRef != nil && strings.TrimSpace(*c.BOMRef) != "" {
		return *c.BOMRef, nil
	}
	if c.Purl != nil && strings.TrimSpace(*c.Purl) != "" {
		p, err := purl.Parse(*c.Purl)
		if err != nil {
			return "", fmt.Errorf("purl canonicalisation: %w", err)
		}
		return p.String(), nil
	}

	name := strings.TrimSpace(c.Name)
	if name == "" {
		return "", fmt.Errorf("component has no name, no purl, no bom-ref")
	}
	encName := pctEncodeUnreserved(name)
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return "pkg:generic/" + encName + "@" + pctEncodeUnreserved(strings.TrimSpace(*c.Version)), nil
	}
	return "pkg:generic/" + encName, nil
}

// pctEncodeUnreserved percent-encodes every byte that is NOT in the
// RFC 3986 §2.3 "unreserved" set — ALPHA / DIGIT / `-` / `.` / `_` /
// `~`. The output is safe to drop into a purl name segment without
// further escaping. Percent-encoding applies byte-wise on UTF-8 bytes
// so multi-byte characters are expressed in their wire form.
func pctEncodeUnreserved(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if unreservedByte(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func unreservedByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	}
	return false
}
