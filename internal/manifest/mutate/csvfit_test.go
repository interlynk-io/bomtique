// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

func TestCheckFitsCSV_AcceptsSimpleComponent(t *testing.T) {
	c := &manifest.Component{
		Name:    "libx",
		Version: strPtr("1.0"),
		License: &manifest.License{Expression: "MIT"},
		Purl:    strPtr("pkg:generic/libx@1.0"),
	}
	if err := CheckFitsCSV(c); err != nil {
		t.Fatalf("CheckFitsCSV: %v", err)
	}
}

func TestCheckFitsCSV_RejectsIncompatibleFields(t *testing.T) {
	cases := []struct {
		name string
		c    *manifest.Component
		want string
	}{
		{
			name: "bom-ref",
			c:    &manifest.Component{Name: "x", Version: strPtr("1"), BOMRef: strPtr("b")},
			want: "bom-ref",
		},
		{
			name: "external_references",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				ExternalReferences: []manifest.ExternalRef{{Type: "vcs", URL: "u"}},
			},
			want: "external_references",
		},
		{
			name: "license texts",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				License: &manifest.License{
					Expression: "MIT",
					Texts:      []manifest.LicenseText{{ID: "MIT", File: strPtr("./L")}},
				},
			},
			want: "structured license texts",
		},
		{
			name: "directory hash",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				Hashes: []manifest.Hash{{Algorithm: "SHA-256", Path: strPtr("./d/")}},
			},
			want: "directory-form hashes",
		},
		{
			name: "multiple hashes",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				Hashes: []manifest.Hash{
					{Algorithm: "SHA-256", Value: strPtr("aa")},
					{Algorithm: "SHA-512", Value: strPtr("bb")},
				},
			},
			want: "at most one hash",
		},
		{
			name: "pedigree",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				Pedigree: &manifest.Pedigree{Notes: strPtr("n")},
			},
			want: "pedigree",
		},
		{
			name: "lifecycles",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				Lifecycles: []manifest.Lifecycle{{Phase: "build"}},
			},
			want: "lifecycles",
		},
		{
			name: "unknown fields",
			c: &manifest.Component{
				Name: "x", Version: strPtr("1"),
				Unknown: map[string]json.RawMessage{"x-foo": json.RawMessage(`"y"`)},
			},
			want: "unknown fields",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckFitsCSV(tc.c)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not name the offending field: got %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCheckFitsCSV_NilComponent(t *testing.T) {
	if err := CheckFitsCSV(nil); err == nil {
		t.Fatal("CheckFitsCSV(nil) should error")
	}
}
