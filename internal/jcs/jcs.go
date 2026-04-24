// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package jcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Canonicalize takes arbitrary JSON bytes and returns the RFC 8785 JCS
// canonical form: object keys sorted lexicographically by UTF-16 code
// unit, no whitespace, minimal string escaping, and ECMA-262 number
// formatting (shortest round-trip with a normalized exponent).
//
// The input must be a single JSON value. Trailing content after the
// top-level value is an error. NaN and ±Infinity — which JSON disallows
// but json.Decoder with UseNumber can still produce via non-standard
// input — are rejected here too: JCS produces no canonical form for
// them.
//
// The function does not modify the input; the returned slice is freshly
// allocated.
func Canonicalize(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("jcs: parse: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("jcs: trailing content after top-level value")
	}

	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeCanonical emits v into buf in RFC 8785 form. The caller must
// pre-validate that v came from json.Decoder.Decode with UseNumber.
func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		s, err := canonicalNumber(string(t))
		if err != nil {
			return err
		}
		buf.WriteString(s)
	case string:
		writeCanonicalString(buf, t)
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := sortedKeysUTF16(t)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonicalString(buf, k)
			buf.WriteByte(':')
			if err := writeCanonical(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("jcs: unsupported type %T", v)
	}
	return nil
}

// canonicalNumber renders a JSON number per RFC 8785 §3.2.2, which
// defers to ECMA-262 Number.prototype.toString. Go's strconv helpers
// don't produce that exact form (they keep leading-zero exponents and
// prefer scientific notation at different thresholds), so this is a
// hand-rolled implementation of the ECMA-262 algorithm seeded by
// FormatFloat's shortest-round-trip 'e' form.
//
// Given a finite non-zero value m, the algorithm finds (s, k, n) where
// s is the shortest decimal significand of length k and n is chosen so
// m = s * 10^(n-k). From there:
//
//	If k ≤ n ≤ 21: emit s followed by (n-k) zeros (integer form).
//	Else if 0 < n ≤ 21: emit s[:n] "." s[n:] (decimal form).
//	Else if -6 < n ≤ 0: emit "0." (-n zeros) s (leading-zero decimal).
//	Else: emit scientific — s[0] ("." s[1:])? "e" sign (|n-1|) — with
//	      a mandatory sign and no leading-zero exponent.
func canonicalNumber(s string) (string, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("jcs: parse number %q: %w", s, err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("jcs: NaN/Inf not permitted by RFC 8785")
	}
	if f == 0 {
		return "0", nil
	}

	neg := f < 0
	if neg {
		f = -f
	}

	sci := strconv.FormatFloat(f, 'e', -1, 64) // "d.dde+EE" or "de+EE"
	eIdx := strings.IndexByte(sci, 'e')
	mantissa := sci[:eIdx]
	expN, err := strconv.Atoi(sci[eIdx+1:])
	if err != nil {
		return "", fmt.Errorf("jcs: unexpected FormatFloat output %q: %w", sci, err)
	}

	digits := mantissa
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		digits = mantissa[:i] + mantissa[i+1:]
	}
	digits = strings.TrimRight(digits, "0")
	if digits == "" {
		digits = "0"
	}
	k := len(digits)
	n := expN + 1

	var body string
	switch {
	case n >= k && n <= 21:
		body = digits + strings.Repeat("0", n-k)
	case n > 0 && n <= 21:
		body = digits[:n] + "." + digits[n:]
	case n > -6 && n <= 0:
		body = "0." + strings.Repeat("0", -n) + digits
	default:
		exp := n - 1
		sign := "+"
		if exp < 0 {
			sign = "-"
			exp = -exp
		}
		if k == 1 {
			body = digits + "e" + sign + strconv.Itoa(exp)
		} else {
			body = digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(exp)
		}
	}

	if neg {
		return "-" + body, nil
	}
	return body, nil
}

// writeCanonicalString emits s as a JSON string using RFC 8785's
// minimal escape set:
//
//   - `"` → `\"`
//   - `\` → `\\`
//   - `\b`, `\f`, `\n`, `\r`, `\t` → their short escapes
//   - any other C0 control (U+0000 to U+001F) → `\u00XX` (lowercase hex)
//   - everything else passes through as UTF-8 bytes, including the
//     solidus `/` (which JSON permits unescaped) and all non-ASCII.
func writeCanonicalString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// sortedKeysUTF16 orders object keys by UTF-16 code-unit sequence, as
// RFC 8785 §3.2.3 requires. For ASCII keys this matches byte ordering;
// it matters when keys contain characters above U+007F, particularly
// non-BMP characters where the surrogate-pair sequence changes the
// relative order versus a naive UTF-8 byte sort.
func sortedKeysUTF16(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return utf16Less(keys[i], keys[j])
	})
	return keys
}

func utf16Less(a, b string) bool {
	au := utf16.Encode([]rune(a))
	bu := utf16.Encode([]rune(b))
	n := len(au)
	if len(bu) < n {
		n = len(bu)
	}
	for i := 0; i < n; i++ {
		if au[i] != bu[i] {
			return au[i] < bu[i]
		}
	}
	return len(au) < len(bu)
}
