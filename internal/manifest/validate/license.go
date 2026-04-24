// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// validateLicense enforces §6.3:
//
//   - expression is required and non-empty.
//   - each texts[].id must be a simple SPDX identifier (a single token,
//     no operators / whitespace).
//   - each texts[].id must appear as a bare identifier within expression.
//   - exactly one of texts[].text or texts[].file must be present.
//
// Full SPDX License Expression grammar validation is reserved for
// Options.SPDXExpressionStrict (currently a no-op — the simple
// identifier-in-expression check catches the most common mistakes and
// is cheap enough to run unconditionally).
func (v *validator) validateLicense(l *manifest.License, ptr string) {
	expr := strings.TrimSpace(l.Expression)
	if expr == "" {
		v.add(Error{
			Kind:    ErrLicense,
			Pointer: ptr + "/expression",
			Message: "license expression is required (§6.3)",
		})
		return
	}

	// Optional strict check: does `expression` parse per the SPDX
	// License Expression grammar (Annex D)?  Runs only when the caller
	// opts in via Options.SPDXExpressionStrict.  The cheap token-
	// containment check below still runs regardless.
	if v.opts.SPDXExpressionStrict {
		if err := parseSPDXExpression(expr); err != nil {
			v.add(Error{
				Kind:    ErrLicense,
				Pointer: ptr + "/expression",
				Value:   l.Expression,
				Message: fmt.Sprintf("SPDX expression grammar error: %v", err),
			})
		}
	}

	tokens := expressionTokens(expr)

	for i, t := range l.Texts {
		tptr := fmt.Sprintf("%s/texts/%d", ptr, i)

		id := strings.TrimSpace(t.ID)
		if id == "" {
			v.add(Error{
				Kind:    ErrLicense,
				Pointer: tptr + "/id",
				Message: "texts[].id is required and must be non-empty",
			})
		} else if !isSimpleSPDXID(id) {
			v.add(Error{
				Kind:    ErrLicense,
				Pointer: tptr + "/id",
				Value:   t.ID,
				Message: "texts[].id must be a simple SPDX identifier (no operators or whitespace)",
			})
		} else if !containsToken(tokens, id) {
			v.add(Error{
				Kind:    ErrLicense,
				Pointer: tptr + "/id",
				Value:   t.ID,
				Message: "texts[].id does not appear as a bare identifier in license expression",
			})
		}

		hasText := t.Text != nil && *t.Text != ""
		hasFile := t.File != nil && *t.File != ""
		switch {
		case hasText && hasFile:
			v.add(Error{
				Kind:    ErrLicense,
				Pointer: tptr,
				Message: "texts[] entry must carry exactly one of text or file, not both",
			})
		case !hasText && !hasFile:
			v.add(Error{
				Kind:    ErrLicense,
				Pointer: tptr,
				Message: "texts[] entry must carry exactly one of text or file",
			})
		}
	}
}

// expressionTokens extracts license-identifier tokens from an SPDX
// expression. Operators (AND, OR, WITH), parentheses, and the `+`
// suffix are stripped so the caller can ask whether a given identifier
// is in the remaining set.
func expressionTokens(expr string) []string {
	s := strings.ReplaceAll(expr, "(", " ")
	s = strings.ReplaceAll(s, ")", " ")
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSuffix(f, "+")
		switch f {
		case "AND", "OR", "WITH":
			continue
		}
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func containsToken(tokens []string, target string) bool {
	for _, t := range tokens {
		if t == target {
			return true
		}
	}
	return false
}

// isSimpleSPDXID reports whether id looks like a single SPDX identifier
// — no whitespace, no operators, no parentheses. This is a cheap
// structural check that complements the "appears in expression" rule.
func isSimpleSPDXID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch r {
		case ' ', '\t', '\n', '\r', '(', ')':
			return false
		}
	}
	switch id {
	case "AND", "OR", "WITH":
		return false
	}
	return true
}
