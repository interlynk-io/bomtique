// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/interlynk-io/bomtique/internal/purl"
)

// RefKind classifies a parsed depends-on entry per §10.2.
type RefKind int

const (
	RefUnknown     RefKind = iota
	RefPurl                // starts with `pkg:` — canonical purl match
	RefNameVersion         // last-@ split — byte-exact (name, version) match
)

// Ref is a parsed `depends-on` entry. One of Purl (RefPurl) or
// Name + Version (RefNameVersion) is set, matching Kind.
type Ref struct {
	Kind    RefKind
	Raw     string // original entry as written in the manifest
	Purl    string // canonical form when Kind == RefPurl
	Name    string
	Version string
}

// ErrInvalidReference signals a depends-on entry that fits neither §10.2
// form — whitespace, bare name without `@`, empty string. Callers surface
// this as a hard error (M4 validation territory); M6 uses it as a
// programmatic marker.
var ErrInvalidReference = errors.New("invalid depends-on reference")

// ErrInvalidPurlReference signals a `pkg:`-prefixed entry that failed to
// parse as a purl per [purl-spec]. §10.2 last sentence: hard error.
var ErrInvalidPurlReference = errors.New("invalid purl in depends-on reference")

// ParseRef parses one `depends-on` entry per §10.2. It accepts two forms:
//
//   - `pkg:<purl>` — the canonical form. Parse via internal/purl;
//     invalid purls are a hard error.
//   - `name@version` — the byte-exact fallback, split on the *last* `@`
//     character so scoped ecosystem identifiers like
//     `@angular/core@1.0.0` parse with name = `@angular/core`,
//     version = `1.0.0`.
//
// Any string that contains whitespace, or any non-`pkg:` string without
// at least one `@`, is invalid. A bare `@something` with no version side
// is invalid too.
func ParseRef(raw string) (Ref, error) {
	if raw == "" {
		return Ref{}, fmt.Errorf("%w: empty string", ErrInvalidReference)
	}
	if containsWhitespace(raw) {
		return Ref{}, fmt.Errorf("%w: contains whitespace: %q", ErrInvalidReference, raw)
	}

	if strings.HasPrefix(raw, "pkg:") {
		p, err := purl.Parse(raw)
		if err != nil {
			return Ref{}, fmt.Errorf("%w: %s: %w", ErrInvalidPurlReference, raw, err)
		}
		return Ref{Kind: RefPurl, Raw: raw, Purl: p.String()}, nil
	}

	// name@version form: split on LAST `@` per §10.2.
	atIdx := strings.LastIndexByte(raw, '@')
	if atIdx <= 0 || atIdx == len(raw)-1 {
		// Either no `@` (bare name — not allowed), or `@` is first (no
		// name before it), or `@` is last (no version after it).
		return Ref{}, fmt.Errorf("%w: not a pkg: purl or name@version: %q", ErrInvalidReference, raw)
	}
	name := raw[:atIdx]
	version := raw[atIdx+1:]
	if name == "" || version == "" {
		return Ref{}, fmt.Errorf("%w: empty name or version in %q", ErrInvalidReference, raw)
	}
	return Ref{Kind: RefNameVersion, Raw: raw, Name: name, Version: version}, nil
}

func containsWhitespace(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}
