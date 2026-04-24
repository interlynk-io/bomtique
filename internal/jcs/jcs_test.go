// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package jcs_test

import (
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/jcs"
)

// -----------------------------------------------------------------------------
// Object key sorting (UTF-16 code-unit order).
// -----------------------------------------------------------------------------

func TestCanonicalize_SortsKeys(t *testing.T) {
	in := []byte(`{"b": 1, "a": 2, "c": 3}`)
	got, err := jcs.Canonicalize(in)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if string(got) != `{"a":2,"b":1,"c":3}` {
		t.Fatalf("got %s", got)
	}
}

func TestCanonicalize_NestedKeysSorted(t *testing.T) {
	in := []byte(`{"z": {"d": 4, "a": 1}, "a": {"y": 2, "b": 3}}`)
	got, _ := jcs.Canonicalize(in)
	if string(got) != `{"a":{"b":3,"y":2},"z":{"a":1,"d":4}}` {
		t.Fatalf("nested sort wrong: %s", got)
	}
}

// -----------------------------------------------------------------------------
// No whitespace.
// -----------------------------------------------------------------------------

func TestCanonicalize_StripsWhitespace(t *testing.T) {
	in := []byte("   \n\t { \"x\" : [ 1 , 2 , 3 ] }  \n")
	got, _ := jcs.Canonicalize(in)
	if string(got) != `{"x":[1,2,3]}` {
		t.Fatalf("got %s", got)
	}
}

// -----------------------------------------------------------------------------
// Numbers — RFC 8785 §3.2.2 cases.
// -----------------------------------------------------------------------------

func TestCanonicalize_NumberFormatting(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"0", "0"},
		{"-0", "0"}, // negative zero collapses
		{"1", "1"},
		{"-1", "-1"},
		{"10e3", "10000"}, // expands to integer
		{"1e21", "1e+21"}, // large: scientific with + sign
		{"1e-7", "1e-7"},  // small: scientific with - sign, no leading zero
		{"0.1", "0.1"},
		{"0.0001", "0.0001"},
		{"1e-6", "0.000001"},
		{"1.5", "1.5"},
		{"1500", "1500"},
		{"-2.5e10", "-25000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := jcs.Canonicalize([]byte(tc.in))
			if err != nil {
				t.Fatalf("Canonicalize(%q): %v", tc.in, err)
			}
			if string(got) != tc.want {
				t.Fatalf("Canonicalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Strings — escape set.
// -----------------------------------------------------------------------------

func TestCanonicalize_StringEscapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"hello", `"hello"`, `"hello"`},
		{"solidus", `"a/b"`, `"a/b"`}, // solidus passes through per §3.2.2
		{"quote", `"\""`, `"\""`},
		{"backslash", `"\\"`, `"\\"`},
		{"short_escapes", `"\n\r\t\b\f"`, `"\n\r\t\b\f"`},
		{"empty", `""`, `""`},
		{"ascii_space", `" "`, `" "`},
		{"utf8_passthrough", `"café"`, `"café"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := jcs.Canonicalize([]byte(tc.in))
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalize_ControlCharUnicodeEscape(t *testing.T) {
	// Input is a JSON string containing a U+001F control character via
	// \u001f escape; JCS re-emits it in the canonical \u00NN lowercase-hex
	// form.
	in := []byte(`"\u001f"`)
	got, err := jcs.Canonicalize(in)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if string(got) != `"\u001f"` {
		t.Fatalf("got %q, want %q", got, `"\u001f"`)
	}
}

// -----------------------------------------------------------------------------
// Arrays preserve input order.
// -----------------------------------------------------------------------------

func TestCanonicalize_ArrayOrderPreserved(t *testing.T) {
	in := []byte(`["z", "a", "m"]`)
	got, _ := jcs.Canonicalize(in)
	if string(got) != `["z","a","m"]` {
		t.Fatalf("array order changed: %s", got)
	}
}

// -----------------------------------------------------------------------------
// Booleans + null.
// -----------------------------------------------------------------------------

func TestCanonicalize_Literals(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"true", "true"},
		{"false", "false"},
		{"null", "null"},
	}
	for _, tc := range cases {
		got, _ := jcs.Canonicalize([]byte(tc.in))
		if string(got) != tc.want {
			t.Fatalf("Canonicalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Compound scenario combining everything.
// -----------------------------------------------------------------------------

func TestCanonicalize_Compound(t *testing.T) {
	in := []byte(`{
		"arr": [3, 1, 2],
		"n": 1.5e2,
		"s": "hello\nworld",
		"obj": {"z": true, "a": null}
	}`)
	got, _ := jcs.Canonicalize(in)
	want := `{"arr":[3,1,2],"n":150,"obj":{"a":null,"z":true},"s":"hello\nworld"}`
	if string(got) != want {
		t.Fatalf("compound mismatch\n  got  %s\n  want %s", got, want)
	}
}

// -----------------------------------------------------------------------------
// Deterministic output — run twice, compare.
// -----------------------------------------------------------------------------

func TestCanonicalize_Idempotent(t *testing.T) {
	in := []byte(`{"c":3,"a":1,"b":{"y":2,"x":1}}`)
	a, _ := jcs.Canonicalize(in)
	b, _ := jcs.Canonicalize(a)
	if string(a) != string(b) {
		t.Fatalf("not idempotent:\n  first:  %s\n  second: %s", a, b)
	}
}

// -----------------------------------------------------------------------------
// Errors — trailing content, parse.
// -----------------------------------------------------------------------------

func TestCanonicalize_TrailingContent(t *testing.T) {
	_, err := jcs.Canonicalize([]byte(`{"a":1} garbage`))
	if err == nil || !strings.Contains(err.Error(), "trailing content") {
		t.Fatalf("expected trailing-content error, got %v", err)
	}
}

func TestCanonicalize_ParseError(t *testing.T) {
	_, err := jcs.Canonicalize([]byte(`{not valid`))
	if err == nil {
		t.Fatal("expected parse error")
	}
}
