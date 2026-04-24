// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"strings"
	"testing"
)

func TestParseSPDXExpression_Valid(t *testing.T) {
	cases := []string{
		"MIT",
		"Apache-2.0",
		"Apache-2.0+",
		"MIT OR Apache-2.0",
		"MIT AND BSD-3-Clause",
		"(MIT AND BSD-3-Clause) OR Apache-2.0",
		"GPL-2.0-or-later WITH Classpath-exception-2.0",
		"(GPL-2.0-only WITH Classpath-exception-2.0) AND MIT",
		"LicenseRef-Custom",
		"DocumentRef-External:LicenseRef-Custom",
		"MIT AND LicenseRef-Custom",
		"Apache-2.0 OR (GPL-2.0-or-later AND MIT)",
		"(((MIT)))",
		"MIT AND Apache-2.0 AND BSD-3-Clause OR 0BSD",
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			if err := parseSPDXExpression(expr); err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestParseSPDXExpression_Invalid(t *testing.T) {
	cases := []struct {
		name, expr, contains string
	}{
		{"empty", "", "unexpected end"},
		{"unbalanced open paren", "(MIT", "unbalanced"},
		{"unbalanced close paren", "MIT)", "trailing content"},
		{"dangling OR", "MIT OR", "unexpected end"},
		{"dangling AND", "MIT AND", "unexpected end"},
		{"missing exception after WITH", "GPL-2.0 WITH", "exception identifier"},
		{"parens around WITH RHS", "GPL-2.0 WITH (Classpath-exception-2.0)", "exception identifier"},
		// SPDX operators are case-sensitive — a lowercase `and`/`or`
		// lexes as a plain identifier, so the parser sees no operator
		// between the two licenses and surfaces a trailing-content
		// error when a stray ident follows a valid primary.
		{"lowercase and", "MIT and Apache-2.0", "trailing content"},
		{"lowercase or", "MIT or Apache-2.0", "trailing content"},
		{"bare and as primary", "and MIT", "operator keyword"},
		{"double plus", "MIT++", "middle"},
		{"plus then content", "MIT+Apache", "middle"},
		{"bare plus", "+", "bare `+`"},
		{"triple-colon ref", "A:B:C", "too many"},
		{"missing LicenseRef suffix", "DocumentRef-Foo", "LicenseRef-"},
		{"empty LicenseRef", "LicenseRef-", "empty id"},
		{"plus on exception", "GPL-2.0 WITH Classpath-exception-2.0+", "cannot use the `+`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseSPDXExpression(tc.expr)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.expr)
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected error to contain %q, got %v", tc.contains, err)
			}
		})
	}
}
