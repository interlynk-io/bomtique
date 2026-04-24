// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package pool

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// Kind classifies which §11 precedence rung produced an Identity.
type Kind int

const (
	KindUnknown     Kind = iota
	KindPurl             // component carries a non-empty purl
	KindNameVersion      // component has no purl but carries a non-empty version
	KindNameOnly         // component has only a name — the fallback rung
)

func (k Kind) String() string {
	switch k {
	case KindPurl:
		return "purl"
	case KindNameVersion:
		return "name+version"
	case KindNameOnly:
		return "name"
	}
	return "unknown"
}

// Identity is a component's resolved identity per §11. The zero value
// represents a component that failed identity extraction (e.g. empty
// name) — call [Identify] instead of constructing directly.
type Identity struct {
	Kind Kind

	// Purl is the canonicalized purl string (via internal/purl.Parse →
	// String) when Kind == KindPurl. Empty otherwise.
	Purl string

	// Name is the component's name. Populated for every Kind; §11
	// falls through to name alone only when no version or purl is
	// present.
	Name string

	// Version is the component's version string. Populated only for
	// Kind == KindNameVersion. Empty otherwise.
	Version string
}

// Key returns a stable map-friendly key for the Identity. Two
// components conflict iff their Key values are equal — except for the
// cross-identity purl-vs-nameVersion check, which [samePurlCanon]
// handles separately.
func (i Identity) Key() string {
	switch i.Kind {
	case KindPurl:
		return "purl|" + i.Purl
	case KindNameVersion:
		return "nv|" + i.Name + "\x00" + i.Version
	case KindNameOnly:
		return "n|" + i.Name
	}
	return ""
}

// String returns a human-friendly rendering for warning messages.
func (i Identity) String() string {
	switch i.Kind {
	case KindPurl:
		return i.Purl
	case KindNameVersion:
		return i.Name + "@" + i.Version
	case KindNameOnly:
		return i.Name
	}
	return "<unknown>"
}

// Identify extracts a component's identity per §11. The purl side is
// parsed through internal/purl so comparisons happen on the canonical
// form. A bad purl is a hard error at validation time (M4); this
// package assumes the input has already passed validation and only
// surfaces an error if canonicalisation fails unexpectedly.
func Identify(c *manifest.Component) (Identity, error) {
	if c == nil {
		return Identity{}, fmt.Errorf("pool: nil component")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return Identity{}, fmt.Errorf("pool: component has empty name")
	}

	if c.Purl != nil && strings.TrimSpace(*c.Purl) != "" {
		canon, err := canonicalPurl(*c.Purl)
		if err != nil {
			return Identity{}, fmt.Errorf("pool: purl canonicalisation: %w", err)
		}
		return Identity{Kind: KindPurl, Purl: canon, Name: name}, nil
	}
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return Identity{
			Kind:    KindNameVersion,
			Name:    name,
			Version: strings.TrimSpace(*c.Version),
		}, nil
	}
	return Identity{Kind: KindNameOnly, Name: name}, nil
}

// canonicalPurl returns the canonical string form of raw, suitable for
// byte-exact key comparison. §9.3 mandates canonical-form comparison for
// every purl check in the spec, and internal/purl already does that at
// Parse time.
func canonicalPurl(raw string) (string, error) {
	p, err := purl.Parse(raw)
	if err != nil {
		return "", err
	}
	return p.String(), nil
}
