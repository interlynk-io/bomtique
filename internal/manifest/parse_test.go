// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestAppendixJSONRoundTrip parses each Appendix B JSON fixture, asserts a
// few identity-level facts, then re-marshals and re-parses to confirm the
// parser's outputs survive a round-trip through json.Marshal.
func TestAppendixJSONRoundTrip(t *testing.T) {
	type want struct {
		kind       Kind
		schema     string
		primary    string   // primary name, empty if components
		components []string // component names, in order
	}
	cases := []struct {
		name string
		file string
		want want
	}{
		{
			name: "B.1 minimal primary",
			file: "b1.json",
			want: want{kind: KindPrimary, schema: SchemaPrimaryV1, primary: "acme-server"},
		},
		{
			name: "B.2 minimal components",
			file: "b2.json",
			want: want{kind: KindComponents, schema: SchemaComponentsV1, components: []string{"libmqtt"}},
		},
		{
			name: "B.3 server primary",
			file: "b3_server_primary.json",
			want: want{kind: KindPrimary, schema: SchemaPrimaryV1, primary: "acme-server"},
		},
		{
			name: "B.3 worker primary",
			file: "b3_worker_primary.json",
			want: want{kind: KindPrimary, schema: SchemaPrimaryV1, primary: "acme-worker"},
		},
		{
			name: "B.3 shared components",
			file: "b3_shared_components.json",
			want: want{kind: KindComponents, schema: SchemaComponentsV1, components: []string{"libmqtt", "libtls"}},
		},
		{
			name: "B.4 dual-licensed native",
			file: "b4.json",
			want: want{kind: KindComponents, schema: SchemaComponentsV1, components: []string{"libjpeg-turbo"}},
		},
		{
			name: "B.5 vendored with pedigree",
			file: "b5.json",
			want: want{kind: KindComponents, schema: SchemaComponentsV1, components: []string{"vendor-parser"}},
		},
		{
			name: "B.6 sub-component bom-ref",
			file: "b6.json",
			want: want{kind: KindComponents, schema: SchemaComponentsV1, components: []string{"acme_imaging._core"}},
		},
		{
			name: "B.7 vendored header no version",
			file: "b7.json",
			want: want{kind: KindComponents, schema: SchemaComponentsV1, components: []string{"pythoncapi_compat"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "appendix", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			m1, err := ParseJSON(data, tc.file)
			if err != nil {
				t.Fatalf("first parse: %v", err)
			}
			assertManifestShape(t, m1, tc.want)

			// Re-marshal and re-parse. Because MarshalJSON on the top-level
			// manifest types uses the default field encoder, the output is
			// canonical-ish JSON. Re-parsing it must yield a value equal to
			// the first parse (modulo the Path/Format fields we reset).
			remarshalled, err := remarshalManifest(m1)
			if err != nil {
				t.Fatalf("remarshal: %v", err)
			}
			m2, err := ParseJSON(remarshalled, tc.file)
			if err != nil {
				t.Fatalf("second parse: %v\n--- remarshalled:\n%s", err, remarshalled)
			}
			if !reflect.DeepEqual(m1, m2) {
				t.Fatalf("round-trip mismatch\nfirst:  %+v\nsecond: %+v\nremarshalled: %s",
					m1, m2, string(remarshalled))
			}
		})
	}
}

func assertManifestShape(t *testing.T, m *Manifest, w struct {
	kind       Kind
	schema     string
	primary    string
	components []string
}) {
	t.Helper()
	if m.Kind != w.kind {
		t.Fatalf("kind: got %v, want %v", m.Kind, w.kind)
	}
	if m.Format != FormatJSON {
		t.Fatalf("format: got %v, want json", m.Format)
	}
	switch w.kind {
	case KindPrimary:
		if m.Primary == nil {
			t.Fatalf("Primary nil for primary manifest")
		}
		if m.Primary.Schema != w.schema {
			t.Fatalf("schema: got %q, want %q", m.Primary.Schema, w.schema)
		}
		if m.Primary.Primary.Name != w.primary {
			t.Fatalf("primary name: got %q, want %q", m.Primary.Primary.Name, w.primary)
		}
	case KindComponents:
		if m.Components == nil {
			t.Fatalf("Components nil for components manifest")
		}
		if m.Components.Schema != w.schema {
			t.Fatalf("schema: got %q, want %q", m.Components.Schema, w.schema)
		}
		if got := len(m.Components.Components); got != len(w.components) {
			t.Fatalf("component count: got %d, want %d", got, len(w.components))
		}
		for i, want := range w.components {
			if got := m.Components.Components[i].Name; got != want {
				t.Fatalf("component %d name: got %q, want %q", i, got, want)
			}
		}
	}
}

func remarshalManifest(m *Manifest) ([]byte, error) {
	switch m.Kind {
	case KindPrimary:
		return json.Marshal(m.Primary)
	case KindComponents:
		return json.Marshal(m.Components)
	}
	return nil, errors.New("unknown kind")
}

// TestAppendixB8CSV parses the CSV components manifest from Appendix B.8
// and asserts per-row content, including the comma-quoted tags cell.
func TestAppendixB8CSV(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "appendix", "b8.csv"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	m, err := ParseCSV(data, "b8.csv")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Kind != KindComponents || m.Format != FormatCSV {
		t.Fatalf("kind/format: got %v/%v, want components/csv", m.Kind, m.Format)
	}
	if m.Components == nil {
		t.Fatalf("Components nil")
	}
	if got, want := m.Components.Schema, SchemaComponentsV1; got != want {
		t.Fatalf("schema: got %q, want %q", got, want)
	}
	if got := len(m.Components.Components); got != 2 {
		t.Fatalf("component count: got %d, want 2", got)
	}

	mqtt := m.Components.Components[0]
	if mqtt.Name != "libmqtt" || derefString(mqtt.Version) != "4.3.0" {
		t.Fatalf("row 1 name/version: %q/%v", mqtt.Name, derefString(mqtt.Version))
	}
	if mqtt.Supplier == nil || mqtt.Supplier.Name != "Acme Corp" {
		t.Fatalf("row 1 supplier: %+v", mqtt.Supplier)
	}
	if mqtt.License == nil || mqtt.License.Expression != "EPL-2.0" {
		t.Fatalf("row 1 license: %+v", mqtt.License)
	}
	if derefString(mqtt.Purl) != "pkg:generic/acme/libmqtt@4.3.0" {
		t.Fatalf("row 1 purl: %v", derefString(mqtt.Purl))
	}
	if len(mqtt.Hashes) != 1 || mqtt.Hashes[0].Algorithm != "SHA-256" || derefString(mqtt.Hashes[0].Value) != "9f86d08..." {
		t.Fatalf("row 1 hashes: %+v", mqtt.Hashes)
	}
	if !reflect.DeepEqual(mqtt.DependsOn, []string{"libtls@3.9.0"}) {
		t.Fatalf("row 1 depends_on: %v", mqtt.DependsOn)
	}
	if !reflect.DeepEqual(mqtt.Tags, []string{"core", "networking"}) {
		t.Fatalf("row 1 tags: %v", mqtt.Tags)
	}

	tls := m.Components.Components[1]
	if tls.Name != "libtls" || derefString(tls.Version) != "3.9.0" {
		t.Fatalf("row 2 name/version: %q/%v", tls.Name, derefString(tls.Version))
	}
	if len(tls.DependsOn) != 0 {
		t.Fatalf("row 2 depends_on: %v", tls.DependsOn)
	}
	if !reflect.DeepEqual(tls.Tags, []string{"core", "networking"}) {
		t.Fatalf("row 2 tags: %v", tls.Tags)
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// --- Negative / edge-case tests for parser primitives ---

func TestJSONInvalidUTF8(t *testing.T) {
	// Byte 0xff is not a valid UTF-8 start byte, full stop.
	data := []byte(`{"schema":"primary-manifest/v1","primary":{"name":"x","version":"1.0"}}`)
	data[len(data)-2] = 0xff // corrupt the last few bytes
	if _, err := ParseJSON(data, "bad.json"); err == nil {
		t.Fatal("expected UTF-8 error, got nil")
	} else if !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected UTF-8 error, got %v", err)
	}
}

func TestJSONDuplicateKey(t *testing.T) {
	data := []byte(`{"schema":"primary-manifest/v1","primary":{"name":"x","name":"y","version":"1.0"}}`)
	_, err := ParseJSON(data, "dup.json")
	if err == nil {
		t.Fatal("expected duplicate-key error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate-key error, got %v", err)
	}
}

func TestJSONDuplicateKeyTopLevel(t *testing.T) {
	data := []byte(`{"schema":"primary-manifest/v1","schema":"component-manifest/v1","primary":{"name":"x","version":"1"}}`)
	_, err := ParseJSON(data, "dup.json")
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate-key error, got %v", err)
	}
}

func TestJSONUnknownSchemaFamilyVersion(t *testing.T) {
	data := []byte(`{"schema":"primary-manifest/v2","primary":{"name":"x","version":"1"}}`)
	_, err := ParseJSON(data, "v2.json")
	if err == nil || !strings.Contains(err.Error(), "unsupported primary manifest version") {
		t.Fatalf("expected unsupported-version error, got %v", err)
	}
}

func TestJSONNoSchemaMarker(t *testing.T) {
	data := []byte(`{"primary":{"name":"x","version":"1"}}`)
	_, err := ParseJSON(data, "bare.json")
	if err == nil || !errors.Is(err, ErrNoSchemaMarker) {
		t.Fatalf("expected ErrNoSchemaMarker, got %v", err)
	}
}

func TestJSONPreservesUnknowns(t *testing.T) {
	data := []byte(`{
		"schema": "component-manifest/v1",
		"x-extra": 42,
		"components": [
			{"name":"a","version":"1","x-comp":true}
		]
	}`)
	m, err := ParseJSON(data, "unknowns.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Components.Unknown == nil || string(m.Components.Unknown["x-extra"]) != "42" {
		t.Fatalf("top-level unknown missing: %v", m.Components.Unknown)
	}
	c := m.Components.Components[0]
	if c.Unknown == nil || string(c.Unknown["x-comp"]) != "true" {
		t.Fatalf("component unknown missing: %v", c.Unknown)
	}
}

func TestJSONLicenseStringShorthand(t *testing.T) {
	data := []byte(`{"schema":"component-manifest/v1","components":[{"name":"a","version":"1","license":"MIT"}]}`)
	m, err := ParseJSON(data, "lic.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := m.Components.Components[0]
	if c.License == nil || c.License.Expression != "MIT" {
		t.Fatalf("license: %+v", c.License)
	}
}

func TestJSONDiffTextStringShorthand(t *testing.T) {
	data := []byte(`{
		"schema":"component-manifest/v1",
		"components":[{
			"name":"a","version":"1",
			"pedigree":{"patches":[{"type":"backport","diff":{"text":"--- a\n+++ b\n"}}]}
		}]
	}`)
	m, err := ParseJSON(data, "diff.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := m.Components.Components[0]
	if c.Pedigree == nil || len(c.Pedigree.Patches) != 1 {
		t.Fatalf("patches: %+v", c.Pedigree)
	}
	d := c.Pedigree.Patches[0].Diff
	if d == nil || d.Text == nil || d.Text.Content != "--- a\n+++ b\n" {
		t.Fatalf("diff.text shorthand: %+v", d)
	}
	if d.Text.Encoding != nil || d.Text.ContentType != nil {
		t.Fatalf("string-form diff should not carry encoding/contentType: %+v", d.Text)
	}
}

func TestJSONDiffTextAttachment(t *testing.T) {
	data := []byte(`{
		"schema":"component-manifest/v1",
		"components":[{
			"name":"a","version":"1",
			"pedigree":{"patches":[{"type":"backport","diff":{"text":{"content":"aGVsbG8=","encoding":"base64","contentType":"text/plain"}}}]}
		}]
	}`)
	m, err := ParseJSON(data, "diff.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := m.Components.Components[0].Pedigree.Patches[0].Diff
	if d.Text == nil || d.Text.Content != "aGVsbG8=" {
		t.Fatalf("diff.text content: %+v", d.Text)
	}
	if d.Text.Encoding == nil || *d.Text.Encoding != "base64" {
		t.Fatalf("diff.text encoding: %+v", d.Text.Encoding)
	}
	if d.Text.ContentType == nil || *d.Text.ContentType != "text/plain" {
		t.Fatalf("diff.text contentType: %+v", d.Text.ContentType)
	}
}

func TestCSVBOMStripped(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF},
		[]byte("#component-manifest/v1\nname,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags\nfoo,1.0,,,,,,,,,,,,,\n")...)
	m, err := ParseCSV(data, "bom.csv")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Components.Components) != 1 || m.Components.Components[0].Name != "foo" {
		t.Fatalf("BOM-stripped CSV parse shape: %+v", m.Components.Components)
	}
}

func TestCSVCRLFAndBlankLines(t *testing.T) {
	data := []byte("\r\n#component-manifest/v1\r\n\r\nname,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags\r\n   \r\nfoo,1.0,,,,,,,,,,,,,\r\n")
	m, err := ParseCSV(data, "crlf.csv")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Components.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(m.Components.Components))
	}
}

func TestCSVPrimaryMarkerRejected(t *testing.T) {
	data := []byte("#primary-manifest/v1\nname,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags\n")
	_, err := ParseCSV(data, "primary.csv")
	if err == nil || !strings.Contains(err.Error(), "primary manifest must be serialized as JSON") {
		t.Fatalf("expected primary-in-csv error, got %v", err)
	}
}

func TestCSVHeaderMismatchReordered(t *testing.T) {
	data := []byte("#component-manifest/v1\nversion,name,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags\n")
	_, err := ParseCSV(data, "bad.csv")
	if err == nil || !strings.Contains(err.Error(), "header column") {
		t.Fatalf("expected header mismatch error, got %v", err)
	}
}

func TestCSVHeaderMissingColumn(t *testing.T) {
	data := []byte("#component-manifest/v1\nname,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on\n")
	_, err := ParseCSV(data, "short.csv")
	if err == nil || !strings.Contains(err.Error(), "header has 14 columns") {
		t.Fatalf("expected column-count error, got %v", err)
	}
}

func TestCSVHashValueAndFileExclusive(t *testing.T) {
	data := []byte("#component-manifest/v1\nname,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags\nfoo,1.0,,,,,,,,SHA-256,abc,./x,,,\n")
	_, err := ParseCSV(data, "both.csv")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected exclusive-hash error, got %v", err)
	}
}

func TestParseFileDispatch(t *testing.T) {
	m, err := ParseFile(filepath.Join("testdata", "appendix", "b1.json"))
	if err != nil {
		t.Fatalf("parse b1.json: %v", err)
	}
	if m.Format != FormatJSON || m.Kind != KindPrimary {
		t.Fatalf("dispatch wrong: format=%v kind=%v", m.Format, m.Kind)
	}

	m, err = ParseFile(filepath.Join("testdata", "appendix", "b8.csv"))
	if err != nil {
		t.Fatalf("parse b8.csv: %v", err)
	}
	if m.Format != FormatCSV || m.Kind != KindComponents {
		t.Fatalf("dispatch wrong: format=%v kind=%v", m.Format, m.Kind)
	}
}
