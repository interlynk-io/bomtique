// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl_test

import (
	"testing"

	"github.com/interlynk-io/bomtique/internal/purl"
)

// FuzzParse feeds arbitrary strings to purl.Parse and asserts the
// canonicaliser never panics. A passing fuzz is "errors for invalid
// input, success for valid"; neither branch should crash.
//
// Run locally with `go test -fuzz=FuzzParse ./internal/purl`.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"pkg:generic/x@1.0.0",
		"pkg:github/acme/lib@2.4.0",
		"pkg:github/upstream-org/upstream-lib@2.4.0?vendored=true",
		"pkg:pypi/acme-imaging@1.2.0#c-ext/_core",
		"pkg:generic/x", // no version
		"pkg:GENERIC/x@1.0.0",
		"pkg:",
		"",
		"not-a-purl",
		"pkg:/bad",
		"pkg:generic//@1.0.0", // empty name
		"pkg:bitbucket/@0/@0@0",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = purl.Parse(raw)
	})
}

// FuzzCanonEqual exercises the canonical-equality pairwise comparison.
// For two arbitrary strings we only assert no panic — neither side
// needs to parse.
func FuzzCanonEqual(f *testing.F) {
	seeds := [][2]string{
		{"pkg:generic/x@1.0.0", "pkg:generic/x@1.0.0"},
		{"pkg:GENERIC/x@1.0.0", "pkg:generic/x@1.0.0"},
		{"pkg:generic/x@1.0.0", "pkg:generic/y@1.0.0"},
		{"not-a-purl", "pkg:generic/x@1.0.0"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		_, _ = purl.CanonEqual(a, b)
	})
}
