// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// TestWriteCSV_AppendixB8RoundTrip parses the only CSV fixture, writes
// it back out, and re-parses to a byte-equal model.
func TestWriteCSV_AppendixB8RoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "appendix", "b8.csv"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	m1, err := manifest.ParseCSV(data, "b8.csv")
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, m1); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	out := buf.Bytes()
	if !bytes.HasSuffix(out, []byte{'\n'}) {
		t.Fatalf("WriteCSV output missing trailing newline")
	}
	if !bytes.HasPrefix(out, []byte("#"+manifest.SchemaComponentsV1+"\n")) {
		t.Fatalf("WriteCSV output missing schema marker line:\n%s", out)
	}

	m2, err := manifest.ParseCSV(out, "b8.csv")
	if err != nil {
		t.Fatalf("second parse: %v\n--- output ---\n%s", err, out)
	}
	if !reflect.DeepEqual(m1, m2) {
		t.Fatalf("round-trip mismatch\nfirst:  %+v\nsecond: %+v\noutput:\n%s",
			m1, m2, out)
	}
}

// TestWriteCSV_QuotesCellsWithCommas verifies RFC 4180 quoting kicks in
// on cells that contain comma-delimited lists.
func TestWriteCSV_QuotesCellsWithCommas(t *testing.T) {
	m := &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatCSV,
		Components: &manifest.ComponentsManifest{
			Schema: manifest.SchemaComponentsV1,
			Components: []manifest.Component{
				{
					Name:      "libx",
					Version:   strPtr("1.0"),
					DependsOn: []string{"a@1", "b@2"},
					Tags:      []string{"core", "networking"},
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, m); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	s := buf.String()
	if !strings.Contains(s, `"a@1,b@2"`) {
		t.Fatalf("depends_on cell not double-quoted: %q", s)
	}
	if !strings.Contains(s, `"core,networking"`) {
		t.Fatalf("tags cell not double-quoted: %q", s)
	}
	m2, err := manifest.ParseCSV(buf.Bytes(), "t.csv")
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, s)
	}
	if !reflect.DeepEqual(
		m.Components.Components[0].DependsOn,
		m2.Components.Components[0].DependsOn,
	) {
		t.Fatalf("depends-on round-trip: got %v want %v",
			m2.Components.Components[0].DependsOn,
			m.Components.Components[0].DependsOn)
	}
}

// TestWriteCSV_RejectsIncompatibleComponent proves WriteCSV refuses to
// emit a lossy representation.
func TestWriteCSV_RejectsIncompatibleComponent(t *testing.T) {
	m := &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatCSV,
		Components: &manifest.ComponentsManifest{
			Schema: manifest.SchemaComponentsV1,
			Components: []manifest.Component{
				{
					Name:    "libx",
					Version: strPtr("1.0"),
					Pedigree: &manifest.Pedigree{
						Ancestors: []manifest.Ancestor{{Name: "up", Version: strPtr("1")}},
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	err := WriteCSV(&buf, m)
	if err == nil {
		t.Fatal("WriteCSV should reject components with pedigree")
	}
	if !strings.Contains(err.Error(), "pedigree") {
		t.Fatalf("error does not mention pedigree: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("WriteCSV wrote partial output before rejecting: %q", buf.String())
	}
}

// TestWriteCSV_RejectsPrimaryKind covers the kind-validation branch.
func TestWriteCSV_RejectsPrimaryKind(t *testing.T) {
	m := &manifest.Manifest{
		Kind: manifest.KindPrimary,
		Primary: &manifest.PrimaryManifest{
			Schema:  manifest.SchemaPrimaryV1,
			Primary: manifest.Component{Name: "p", Version: strPtr("1")},
		},
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, m); err == nil {
		t.Fatal("WriteCSV must reject primary manifest (§4.1)")
	}
}

// TestWriteCSV_RejectsNil covers the nil-guard surface.
func TestWriteCSV_RejectsNil(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, nil); err == nil {
		t.Fatal("WriteCSV(nil) should error")
	}
	if err := WriteCSV(&buf, &manifest.Manifest{Kind: manifest.KindComponents}); err == nil {
		t.Fatal("WriteCSV with nil Components should error")
	}
}

// TestWriteCSV_HeaderFrozen asserts the emitter header matches the
// parser header column-for-column. This catches drift.
func TestWriteCSV_HeaderFrozen(t *testing.T) {
	data := []byte("#" + manifest.SchemaComponentsV1 + "\n" +
		strings.Join(csvHeader, ",") + "\n")
	if _, err := manifest.ParseCSV(data, "t.csv"); err != nil {
		t.Fatalf("frozen header does not parse: %v", err)
	}
}
