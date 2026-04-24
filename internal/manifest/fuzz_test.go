// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest_test

import (
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// FuzzParseJSON feeds arbitrary bytes to ParseJSON and asserts the
// parser never panics. The seed corpus covers a valid primary, a
// valid components manifest, the malformed no-marker case, and a few
// pathological shapes (empty object, non-object top level, duplicate
// keys). Run locally with `go test -fuzz=FuzzParseJSON ./internal/manifest`.
func FuzzParseJSON(f *testing.F) {
	seeds := []string{
		`{"schema":"primary-manifest/v1","primary":{"name":"x","version":"1"}}`,
		`{"schema":"component-manifest/v1","components":[{"name":"a","version":"1"}]}`,
		`{"schema":"primary-manifest/v2","primary":{"name":"x"}}`,
		`{}`,
		`[]`,
		`null`,
		`"just-a-string"`,
		`{"schema":"primary-manifest/v1","primary":{"name":"x","name":"y"}}`, // duplicate keys
		`{"schema":"component-manifest/v1","components":[]}`,                 // empty pool
		``,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// The parser is allowed to return an error for any input; the
		// invariant is "no panic".  We intentionally don't inspect the
		// result here.
		_, _ = manifest.ParseJSON(data, "fuzz.json")
	})
}

// FuzzParseCSV feeds arbitrary bytes to ParseCSV with the same
// no-panic invariant. Seeds include a valid components CSV, a header-
// only document, a BOM-prefixed payload, CRLF variants, and a few
// quoting / comma-in-field edge cases.
func FuzzParseCSV(f *testing.F) {
	header := "name,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags"
	seeds := []string{
		"#component-manifest/v1\n" + header + "\nlibfoo,1.0,,,,,,pkg:generic/libfoo@1,,,,,,,",
		"#component-manifest/v1\r\n" + header + "\r\n",
		"\ufeff#component-manifest/v1\n" + header + "\n",
		"#primary-manifest/v1\n" + header + "\n",
		"#component-manifest/v2\n" + header + "\n",
		header + "\n", // no marker
		"",
		`#component-manifest/v1` + "\n" + header + "\n" + `libfoo,1.0,,,,,,,"a,b,c",,,,,,"core,networking"`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = manifest.ParseCSV(data, "fuzz.csv")
	})
}
