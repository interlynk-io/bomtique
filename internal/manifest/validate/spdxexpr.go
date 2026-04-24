// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"
	"strings"
)

// parseSPDXExpression checks that expr conforms to the SPDX License
// Expression grammar defined in SPDX Annex D. It validates structure
// only — a returned `nil` means the expression parses, not that every
// identifier is on the official SPDX License List. That would require
// vendoring a ~600-entry list (plus ~70 exception IDs); bomtique
// leaves that layer to downstream tooling.
//
// Grammar summary (case-sensitive keywords):
//
//	expression      = or-expr
//	or-expr         = and-expr ("OR" and-expr)*
//	and-expr        = with-expr ("AND" with-expr)*
//	with-expr       = primary ("WITH" exception-id)?
//	primary         = "(" or-expr ")" | license
//	license         = license-id ("+")?
//	                | "LicenseRef-"<idstring>
//	                | "DocumentRef-"<idstring> ":" "LicenseRef-"<idstring>
//	license-id      = [A-Za-z0-9.\-_]+        (on the SPDX License List)
//	exception-id    = [A-Za-z0-9.\-_]+        (on the SPDX Exceptions List)
//
// Precedence (loosest to tightest): OR, AND, WITH. Parentheses force
// grouping.
func parseSPDXExpression(expr string) error {
	tokens, err := tokenizeSPDXExpression(expr)
	if err != nil {
		return err
	}
	p := &spdxParser{tokens: tokens}
	if err := p.parseOr(); err != nil {
		return err
	}
	if p.peek().kind != spdxEOF {
		return fmt.Errorf("trailing content %q at position %d", p.peek().raw, p.peek().pos)
	}
	return nil
}

// --- tokenizer ----------------------------------------------------------

type spdxTokenKind int

const (
	spdxEOF spdxTokenKind = iota
	spdxIdent
	spdxAND
	spdxOR
	spdxWITH
	spdxLParen
	spdxRParen
)

type spdxToken struct {
	kind spdxTokenKind
	raw  string
	pos  int // byte offset in the original string, for error messages
}

func (k spdxTokenKind) String() string {
	switch k {
	case spdxIdent:
		return "identifier"
	case spdxAND:
		return "AND"
	case spdxOR:
		return "OR"
	case spdxWITH:
		return "WITH"
	case spdxLParen:
		return "'('"
	case spdxRParen:
		return "')'"
	}
	return "<eof>"
}

// tokenizeSPDXExpression lexes an SPDX License Expression into the
// token sequence the parser consumes.  Whitespace between tokens is
// ignored; `(` / `)` are single-character tokens; everything else is
// an identifier run over `[A-Za-z0-9._+\-:]`.  The operator keywords
// AND / OR / WITH are recognised by their exact spellings (SPDX
// keywords are case-sensitive).
func tokenizeSPDXExpression(expr string) ([]spdxToken, error) {
	var out []spdxToken
	i := 0
	for i < len(expr) {
		c := expr[i]
		switch c {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		case '(':
			out = append(out, spdxToken{kind: spdxLParen, raw: "(", pos: i})
			i++
			continue
		case ')':
			out = append(out, spdxToken{kind: spdxRParen, raw: ")", pos: i})
			i++
			continue
		}
		if !isSPDXIdentByte(c) {
			return nil, fmt.Errorf("unexpected character %q at position %d", c, i)
		}
		start := i
		for i < len(expr) && isSPDXIdentByte(expr[i]) {
			i++
		}
		raw := expr[start:i]
		kind := spdxIdent
		switch raw {
		case "AND":
			kind = spdxAND
		case "OR":
			kind = spdxOR
		case "WITH":
			kind = spdxWITH
		}
		out = append(out, spdxToken{kind: kind, raw: raw, pos: start})
	}
	out = append(out, spdxToken{kind: spdxEOF, pos: len(expr)})
	return out, nil
}

// isSPDXIdentByte recognises the character set that license-ids,
// license-refs, and exception-ids are built from. The `:` is needed
// for `DocumentRef-A:LicenseRef-B` two-part refs; `+` for the
// "or-later" suffix; the rest is the SPDX identifier charset.
func isSPDXIdentByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '.' || c == '-' || c == '_' || c == '+' || c == ':':
		return true
	}
	return false
}

// --- parser --------------------------------------------------------------

type spdxParser struct {
	tokens []spdxToken
	idx    int
}

func (p *spdxParser) peek() spdxToken {
	return p.tokens[p.idx]
}

func (p *spdxParser) consume() spdxToken {
	t := p.tokens[p.idx]
	p.idx++
	return t
}

func (p *spdxParser) expect(k spdxTokenKind) (spdxToken, error) {
	t := p.consume()
	if t.kind != k {
		return t, fmt.Errorf("expected %s, got %s (%q) at position %d", k, t.kind, t.raw, t.pos)
	}
	return t, nil
}

func (p *spdxParser) parseOr() error {
	if err := p.parseAnd(); err != nil {
		return err
	}
	for p.peek().kind == spdxOR {
		p.consume()
		if err := p.parseAnd(); err != nil {
			return err
		}
	}
	return nil
}

func (p *spdxParser) parseAnd() error {
	if err := p.parseWith(); err != nil {
		return err
	}
	for p.peek().kind == spdxAND {
		p.consume()
		if err := p.parseWith(); err != nil {
			return err
		}
	}
	return nil
}

// parseWith handles the WITH operator, which applies an exception to a
// simple license expression.  §D's grammar restricts the right-hand
// side to a single exception-id identifier — not a parenthesised
// expression, not a compound.
func (p *spdxParser) parseWith() error {
	if err := p.parsePrimary(); err != nil {
		return err
	}
	if p.peek().kind != spdxWITH {
		return nil
	}
	p.consume()
	t, err := p.expect(spdxIdent)
	if err != nil {
		return fmt.Errorf("WITH must be followed by an exception identifier: %w", err)
	}
	if strings.HasSuffix(t.raw, "+") {
		return fmt.Errorf("exception identifier %q cannot use the `+` suffix at position %d", t.raw, t.pos)
	}
	return nil
}

func (p *spdxParser) parsePrimary() error {
	t := p.peek()
	switch t.kind {
	case spdxLParen:
		p.consume()
		if err := p.parseOr(); err != nil {
			return err
		}
		if _, err := p.expect(spdxRParen); err != nil {
			return fmt.Errorf("unbalanced parentheses: %w", err)
		}
		return nil
	case spdxIdent:
		p.consume()
		return validateSPDXLicenseForm(t)
	case spdxEOF:
		return fmt.Errorf("unexpected end of expression")
	}
	return fmt.Errorf("unexpected token %q at position %d", t.raw, t.pos)
}

// validateSPDXLicenseForm applies the shape rules that apply to a
// single license identifier, as opposed to an operator or keyword:
//
//   - A `+` suffix is only valid on a license-id, and only once at
//     the end of the ident.
//   - A `:` only appears inside the `DocumentRef-X:LicenseRef-Y` form;
//     both halves have their expected prefixes.
//   - LicenseRef-/DocumentRef- prefixes, when present, must be
//     followed by a non-empty idstring.
//   - A bare operator keyword spelled as a plain ident (e.g. "And")
//     is rejected — SPDX operators are case-sensitive.
func validateSPDXLicenseForm(t spdxToken) error {
	raw := t.raw
	if raw == "" {
		return fmt.Errorf("empty license identifier at position %d", t.pos)
	}

	// `+` must be the last character if present — never at the start,
	// never adjacent to `:`.
	if strings.Contains(raw[:len(raw)-1], "+") {
		return fmt.Errorf("license identifier %q has a `+` in the middle; it must be a single trailing suffix", raw)
	}
	body := raw
	if strings.HasSuffix(body, "+") {
		body = body[:len(body)-1]
		if body == "" {
			return fmt.Errorf("bare `+` is not a license identifier at position %d", t.pos)
		}
	}

	// Split on `:` — allowed only for DocumentRef-/LicenseRef- pairs.
	if strings.Contains(body, ":") {
		parts := strings.Split(body, ":")
		if len(parts) != 2 {
			return fmt.Errorf("license reference %q has too many `:` separators", raw)
		}
		if !strings.HasPrefix(parts[0], "DocumentRef-") || len("DocumentRef-") == len(parts[0]) {
			return fmt.Errorf("license reference %q: left side must be `DocumentRef-<id>`", raw)
		}
		if !strings.HasPrefix(parts[1], "LicenseRef-") || len("LicenseRef-") == len(parts[1]) {
			return fmt.Errorf("license reference %q: right side must be `LicenseRef-<id>`", raw)
		}
		return nil
	}

	if strings.HasPrefix(body, "LicenseRef-") {
		if len(body) == len("LicenseRef-") {
			return fmt.Errorf("license reference %q has empty id", raw)
		}
		return nil
	}
	if strings.HasPrefix(body, "DocumentRef-") {
		return fmt.Errorf("license reference %q is missing the `:LicenseRef-<id>` suffix", raw)
	}

	// Reject case-insensitive matches of the operator keywords —
	// catches `and`, `Or`, `With` typos before they land in output.
	switch strings.ToUpper(body) {
	case "AND", "OR", "WITH":
		if body != strings.ToUpper(body) {
			return fmt.Errorf("license identifier %q shadows operator keyword (SPDX operators are case-sensitive)", raw)
		}
	}
	return nil
}
