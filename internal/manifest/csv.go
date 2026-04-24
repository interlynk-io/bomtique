// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// csvColumns is the frozen column set for v1 (Spec §4.5). The header row
// MUST match this exactly, in this order, with no extras.
var csvColumns = []string{
	"name",
	"version",
	"type",
	"description",
	"supplier_name",
	"supplier_email",
	"license",
	"purl",
	"cpe",
	"hash_algorithm",
	"hash_value",
	"hash_file",
	"scope",
	"depends_on",
	"tags",
}

// csvColumnCount is the number of columns the header MUST carry.
var csvColumnCount = len(csvColumns)

// utf8BOM is U+FEFF as UTF-8, stripped from the head of CSV input per §4.5.1.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ParseCSV parses a CSV components manifest per §4.5 / §4.5.1.
//
// Enforced in M1:
//   - leading UTF-8 BOM stripped.
//   - invalid UTF-8 rejected (§4.2).
//   - first non-blank line is the schema marker (`#component-manifest/v1`);
//     a `#primary-manifest/v1` marker in CSV is rejected (§4.1 — primary
//     manifests are JSON-only); unknown-version markers in a known family
//     are rejected (§4.4).
//   - second non-blank line is the fixed column header; mismatches
//     (extras, missing, reordered) are rejected (§4.5.1).
//   - per-row: `hash_value` XOR `hash_file` (§4.5); `depends_on` / `tags`
//     split on comma with RFC 4180 quoting.
//
// M4 later enforces enumeration membership and "at least one of
// version/purl/hashes" (§6.1).
func ParseCSV(data []byte, path string) (*Manifest, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: invalid UTF-8", pathOrUnknownCSV(path))
	}

	markerLine, afterMarker, ok := firstNonBlankLine(data)
	if !ok {
		return nil, fmt.Errorf("%s: %w", pathOrUnknownCSV(path), ErrNoSchemaMarker)
	}
	marker := strings.TrimSpace(markerLine)
	if !strings.HasPrefix(marker, "#") {
		return nil, fmt.Errorf("%s: %w (first non-blank line is not a `#...` schema marker)", pathOrUnknownCSV(path), ErrNoSchemaMarker)
	}
	markerBody := strings.TrimSpace(marker[1:])
	kind, err := classifySchemaMarker(markerBody)
	if err != nil {
		if errors.Is(err, ErrNoSchemaMarker) {
			return nil, fmt.Errorf("%s: %w", pathOrUnknownCSV(path), err)
		}
		return nil, fmt.Errorf("%s: %w", pathOrUnknownCSV(path), err)
	}
	if kind != KindComponents {
		return nil, fmt.Errorf("%s: primary manifest must be serialized as JSON, not CSV (§4.1)", pathOrUnknownCSV(path))
	}

	cr := csv.NewReader(bytes.NewReader(afterMarker))
	cr.FieldsPerRecord = -1 // we validate column count ourselves
	cr.ReuseRecord = false

	records, err := readAllCSVRecords(cr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pathOrUnknownCSV(path), err)
	}

	nonBlank := dropBlankRecords(records)
	if len(nonBlank) == 0 {
		return nil, fmt.Errorf("%s: missing column header row", pathOrUnknownCSV(path))
	}

	header := nonBlank[0]
	if err := validateCSVHeader(header); err != nil {
		return nil, fmt.Errorf("%s: %w", pathOrUnknownCSV(path), err)
	}

	rows := nonBlank[1:]
	components := make([]Component, 0, len(rows))
	for i, row := range rows {
		comp, err := componentFromCSVRow(row)
		if err != nil {
			// i is the zero-based index among data rows; +1 for human-readable.
			return nil, fmt.Errorf("%s: row %d: %w", pathOrUnknownCSV(path), i+1, err)
		}
		components = append(components, comp)
	}

	return &Manifest{
		Path:   path,
		Kind:   KindComponents,
		Format: FormatCSV,
		Components: &ComponentsManifest{
			Schema:     SchemaComponentsV1,
			Components: components,
		},
	}, nil
}

func pathOrUnknownCSV(path string) string {
	if path == "" {
		return "<csv input>"
	}
	return path
}

// firstNonBlankLine returns the first non-blank line (whitespace-trimmed
// non-empty) and the remaining bytes after the newline terminator. It
// accepts both CRLF and LF. Blank/whitespace-only lines in between are
// skipped.
func firstNonBlankLine(data []byte) (line string, rest []byte, ok bool) {
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		var candidate []byte
		if nl < 0 {
			candidate = data
			data = nil
		} else {
			candidate = data[:nl]
			data = data[nl+1:]
		}
		trimmed := bytes.TrimRight(candidate, "\r")
		if strings.TrimSpace(string(trimmed)) == "" {
			continue
		}
		return string(trimmed), data, true
	}
	return "", nil, false
}

// readAllCSVRecords reads every record from cr until EOF.
func readAllCSVRecords(cr *csv.Reader) ([][]string, error) {
	var out [][]string
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		// Defensive copy: ReuseRecord=false already returns fresh slices,
		// but this guards against a future change.
		cp := make([]string, len(rec))
		copy(cp, rec)
		out = append(out, cp)
	}
}

// dropBlankRecords removes records that contain no content. encoding/csv
// yields one-field empty records for blank/whitespace-only lines when
// FieldsPerRecord=-1.
func dropBlankRecords(records [][]string) [][]string {
	out := make([][]string, 0, len(records))
	for _, r := range records {
		if isBlankRecord(r) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func isBlankRecord(r []string) bool {
	for _, f := range r {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// validateCSVHeader checks that the header row matches csvColumns exactly
// (same length, same values, same order).
func validateCSVHeader(got []string) error {
	if len(got) != csvColumnCount {
		return fmt.Errorf("header has %d columns, want %d (%s)",
			len(got), csvColumnCount, strings.Join(csvColumns, ","))
	}
	for i, want := range csvColumns {
		if got[i] != want {
			return fmt.Errorf("header column %d is %q, want %q", i+1, got[i], want)
		}
	}
	return nil
}

// componentFromCSVRow maps one data row to a Component. The row is already
// known to have the correct column count (validated via the header being
// fixed width and FieldsPerRecord being enforced below).
func componentFromCSVRow(row []string) (Component, error) {
	if len(row) != csvColumnCount {
		return Component{}, fmt.Errorf("row has %d columns, want %d", len(row), csvColumnCount)
	}

	var c Component
	c.Name = row[0]

	if v := row[1]; v != "" {
		c.Version = strPtr(v)
	}
	if v := row[2]; v != "" {
		c.Type = strPtr(v)
	}
	if v := row[3]; v != "" {
		c.Description = strPtr(v)
	}
	if sn, se := row[4], row[5]; sn != "" || se != "" {
		c.Supplier = &Supplier{Name: sn}
		if se != "" {
			c.Supplier.Email = strPtr(se)
		}
	}
	if v := row[6]; v != "" {
		// CSV license column is the shorthand expression string form.
		c.License = &License{Expression: v}
	}
	if v := row[7]; v != "" {
		c.Purl = strPtr(v)
	}
	if v := row[8]; v != "" {
		c.CPE = strPtr(v)
	}

	hashAlg := row[9]
	hashVal := row[10]
	hashFile := row[11]
	if hashVal != "" && hashFile != "" {
		return Component{}, errors.New("hash_value and hash_file are mutually exclusive")
	}
	switch {
	case hashAlg == "" && hashVal == "" && hashFile == "":
		// no hash entry
	case hashAlg == "":
		return Component{}, errors.New("hash_value/hash_file set without hash_algorithm")
	case hashVal == "" && hashFile == "":
		return Component{}, errors.New("hash_algorithm set without hash_value or hash_file")
	default:
		h := Hash{Algorithm: hashAlg}
		if hashVal != "" {
			h.Value = strPtr(hashVal)
		} else {
			h.File = strPtr(hashFile)
		}
		c.Hashes = []Hash{h}
	}

	if v := row[12]; v != "" {
		c.Scope = strPtr(v)
	}
	if v := row[13]; v != "" {
		c.DependsOn = splitCSVList(v)
	}
	if v := row[14]; v != "" {
		c.Tags = splitCSVList(v)
	}
	return c, nil
}

// splitCSVList splits a depends_on / tags cell on bare commas. The outer
// CSV quoting has already been removed by encoding/csv; this function
// operates on the recovered cell content.
//
// Whitespace around entries is preserved: tags and depends-on entries are
// byte-exact (§10.2 says name@version matching is byte-exact, so don't
// strip).
func splitCSVList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func strPtr(s string) *string { return &s }
