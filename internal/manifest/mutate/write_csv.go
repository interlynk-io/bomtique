// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// csvHeader mirrors the frozen §4.5 column order. It is kept in sync
// with internal/manifest/csv.go's csvColumns by the round-trip tests.
var csvHeader = []string{
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

// WriteCSV serialises a components manifest as canonical CSV:
// the `#component-manifest/v1` schema-marker comment line, the frozen
// §4.5 column header, one row per component, LF line endings, RFC 4180
// quoting on cells that need it, and a trailing newline.
//
// Every component MUST satisfy CheckFitsCSV; a mismatch aborts the
// write before any bytes are emitted.
func WriteCSV(w io.Writer, m *manifest.Manifest) error {
	if m == nil {
		return errors.New("WriteCSV: manifest is nil")
	}
	if m.Kind != manifest.KindComponents {
		return fmt.Errorf("WriteCSV: only components manifests are valid CSV inputs (got kind %v)", m.Kind)
	}
	if m.Components == nil {
		return errors.New("WriteCSV: components manifest has nil Components")
	}
	for i := range m.Components.Components {
		c := &m.Components.Components[i]
		if err := CheckFitsCSV(c); err != nil {
			return fmt.Errorf("WriteCSV: components[%d] (%q): %w", i, c.Name, err)
		}
	}

	if _, err := fmt.Fprintln(w, "#"+manifest.SchemaComponentsV1); err != nil {
		return fmt.Errorf("WriteCSV: write marker: %w", err)
	}

	cw := csv.NewWriter(w)
	cw.UseCRLF = false
	if err := cw.Write(csvHeader); err != nil {
		return fmt.Errorf("WriteCSV: write header: %w", err)
	}
	for i := range m.Components.Components {
		row := csvRowFor(&m.Components.Components[i])
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("WriteCSV: write row %d: %w", i, err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("WriteCSV: flush: %w", err)
	}
	return nil
}

// csvRowFor builds one CSV row matching csvHeader for a single
// component. The caller has already proven CheckFitsCSV passes, so
// only the columns CSV supports are consulted here.
func csvRowFor(c *manifest.Component) []string {
	row := make([]string, len(csvHeader))
	row[0] = c.Name
	row[1] = strOr(c.Version)
	row[2] = strOr(c.Type)
	row[3] = strOr(c.Description)
	if c.Supplier != nil {
		row[4] = c.Supplier.Name
		row[5] = strOr(c.Supplier.Email)
	}
	if c.License != nil {
		// CSV column carries the SPDX expression only; license texts
		// are rejected by CheckFitsCSV.
		row[6] = c.License.Expression
	}
	row[7] = strOr(c.Purl)
	row[8] = strOr(c.CPE)
	if len(c.Hashes) == 1 {
		h := c.Hashes[0]
		row[9] = h.Algorithm
		row[10] = strOr(h.Value)
		row[11] = strOr(h.File)
	}
	row[12] = strOr(c.Scope)
	row[13] = joinCSVList(c.DependsOn)
	row[14] = joinCSVList(c.Tags)
	return row
}

// joinCSVList renders a string slice as the comma-separated form the
// parser expects in the `depends_on` / `tags` columns. RFC 4180
// quoting happens at the csv.Writer level when the resulting cell
// contains commas, quotes, or newlines.
func joinCSVList(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.Join(s, ",")
}

func strOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
