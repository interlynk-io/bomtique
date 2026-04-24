// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl_test

import (
	"testing"

	"github.com/interlynk-io/bomtique/internal/purl"
)

func TestCanonEqual(t *testing.T) {
	cases := []struct {
		name   string
		a, b   string
		want   bool
		errors bool
	}{
		{
			name: "identical",
			a:    "pkg:npm/foo@1.0.0",
			b:    "pkg:npm/foo@1.0.0",
			want: true,
		},
		{
			name: "type case-insensitive",
			a:    "pkg:NPM/foo@1.0.0",
			b:    "pkg:npm/foo@1.0.0",
			want: true,
		},
		{
			name: "pypi name underscore vs dash",
			a:    "pkg:pypi/some_lib@1.0.0",
			b:    "pkg:pypi/some-lib@1.0.0",
			want: true,
		},
		{
			name: "qualifier order ignored",
			a:    "pkg:maven/org.acme/lib@1.0.0?classifier=sources&type=jar",
			b:    "pkg:maven/org.acme/lib@1.0.0?type=jar&classifier=sources",
			want: true,
		},
		{
			name: "different versions",
			a:    "pkg:npm/foo@1.0.0",
			b:    "pkg:npm/foo@1.0.1",
			want: false,
		},
		{
			name: "maven namespace case-sensitive",
			a:    "pkg:maven/org.Apache/lib@1.0.0",
			b:    "pkg:maven/org.apache/lib@1.0.0",
			want: false,
		},
		{
			name:   "invalid left",
			a:      "not-a-purl",
			b:      "pkg:npm/foo@1.0.0",
			errors: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := purl.CanonEqual(tc.a, tc.b)
			if tc.errors {
				if err == nil {
					t.Fatalf("expected error, got nil (result %v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("CanonEqual(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestEqualReflexive(t *testing.T) {
	p, err := purl.Parse("pkg:maven/org.acme/lib@1.0.0?classifier=sources#a/b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !purl.Equal(p, p) {
		t.Error("Equal must be reflexive")
	}
}

func TestParseStringRoundtrip(t *testing.T) {
	inputs := []string{
		"pkg:npm/foo@1.0.0",
		"pkg:maven/org.acme/lib@1.0.0?classifier=sources&type=jar",
		"pkg:github/interlynk-io/bomtique@v0.1.0",
		"pkg:generic/acme/libmqtt@4.3.0",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			p, err := purl.Parse(in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := p.String()
			// Re-parse the emitted form and compare canonicalized fields.
			q, err := purl.Parse(got)
			if err != nil {
				t.Fatalf("reparse %q: %v", got, err)
			}
			if !purl.Equal(p, q) {
				t.Errorf("roundtrip mismatch:\n  in   %q\n  out  %q", in, got)
			}
		})
	}
}
