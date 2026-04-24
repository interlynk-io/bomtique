// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// TestWriteJSON_AppendixRoundTrip proves that every Appendix B JSON
// fixture model-round-trips through WriteJSON. Byte equality against
// the fixture is not a goal (§6.3 license and §9.2 diff.text both
// collapse to object form on parse).
func TestWriteJSON_AppendixRoundTrip(t *testing.T) {
	fixtures := []string{
		"b1.json",
		"b2.json",
		"b3_server_primary.json",
		"b3_worker_primary.json",
		"b3_shared_components.json",
		"b4.json",
		"b5.json",
		"b6.json",
		"b7.json",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "testdata", "appendix", name))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			m1, err := manifest.ParseJSON(data, name)
			if err != nil {
				t.Fatalf("first parse: %v", err)
			}

			var buf bytes.Buffer
			if err := WriteJSON(&buf, m1); err != nil {
				t.Fatalf("WriteJSON: %v", err)
			}

			out := buf.Bytes()
			if !bytes.HasSuffix(out, []byte{'\n'}) {
				t.Fatalf("WriteJSON output missing trailing newline")
			}

			m2, err := manifest.ParseJSON(out, name)
			if err != nil {
				t.Fatalf("second parse: %v\n--- output ---\n%s", err, out)
			}
			if !reflect.DeepEqual(m1, m2) {
				t.Fatalf("round-trip mismatch\nfirst:  %+v\nsecond: %+v\noutput:\n%s",
					m1, m2, out)
			}
		})
	}
}

// TestWriteJSON_Indent asserts the canonical 2-space indent format.
func TestWriteJSON_Indent(t *testing.T) {
	src := `{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "x",
    "version": "1"
  }
}`
	m, err := manifest.ParseJSON([]byte(src), "t.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, m); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	got := buf.String()
	want := "{\n  \"schema\": \"primary-manifest/v1\",\n  \"primary\": {\n    \"name\": \"x\",\n    \"version\": \"1\"\n  }\n}\n"
	if got != want {
		t.Fatalf("indent mismatch\n got: %q\nwant: %q", got, want)
	}
}

// TestWriteJSON_UnknownPreserved builds a primary manifest with
// top-level and per-component unknown keys, parses it, re-writes it,
// re-parses the output, and asserts the Unknown sidecar maps survive
// round-trip. This is the load-bearing preservation guarantee for the
// add / update / patch flows.
func TestWriteJSON_UnknownPreserved(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "primary top-level unknown",
			src: `{
  "schema": "primary-manifest/v1",
  "primary": { "name": "p", "version": "1" },
  "x-corp": { "ticket": "JIRA-123", "owner": "infra" },
  "x-notes": "internal build"
}`,
		},
		{
			name: "component unknown",
			src: `{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libx",
      "version": "1",
      "x-cve-exempt": ["CVE-2024-1"],
      "x-owner": "team-a"
    }
  ]
}`,
		},
		{
			name: "components manifest top-level unknown",
			src: `{
  "schema": "component-manifest/v1",
  "x-source": "manual",
  "components": [ { "name": "l", "version": "1" } ]
}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m1, err := manifest.ParseJSON([]byte(tc.src), tc.name)
			if err != nil {
				t.Fatalf("first parse: %v", err)
			}
			var buf bytes.Buffer
			if err := WriteJSON(&buf, m1); err != nil {
				t.Fatalf("WriteJSON: %v", err)
			}
			m2, err := manifest.ParseJSON(buf.Bytes(), tc.name)
			if err != nil {
				t.Fatalf("second parse: %v\n--- output ---\n%s", err, buf.String())
			}
			if !reflect.DeepEqual(m1, m2) {
				t.Fatalf("unknown fields not preserved\nfirst:  %+v\nsecond: %+v\noutput:\n%s",
					m1, m2, buf.String())
			}
		})
	}
}

// TestWriteJSON_UnknownSortedOrder verifies that unknown keys are
// emitted lexicographically after known fields.
func TestWriteJSON_UnknownSortedOrder(t *testing.T) {
	src := `{
  "schema": "primary-manifest/v1",
  "primary": { "name": "p", "version": "1" },
  "z-last": 3,
  "a-first": 1,
  "m-mid": 2
}`
	m, err := manifest.ParseJSON([]byte(src), "t.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, m); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := buf.String()
	iA := strings.Index(out, `"a-first"`)
	iM := strings.Index(out, `"m-mid"`)
	iZ := strings.Index(out, `"z-last"`)
	if iA < 0 || iM < 0 || iZ < 0 {
		t.Fatalf("unknown keys missing from output:\n%s", out)
	}
	if iA >= iM || iM >= iZ {
		t.Fatalf("unknown keys not lexicographically sorted: a=%d m=%d z=%d\n%s", iA, iM, iZ, out)
	}
}

// TestWriteJSON_RejectsNil covers the trivial error surface.
func TestWriteJSON_RejectsNil(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, nil); err == nil {
		t.Fatal("WriteJSON(nil) should error")
	}
	if err := WriteJSON(&buf, &manifest.Manifest{Kind: manifest.KindPrimary}); err == nil {
		t.Fatal("WriteJSON with nil Primary should error")
	}
	if err := WriteJSON(&buf, &manifest.Manifest{Kind: manifest.KindComponents}); err == nil {
		t.Fatal("WriteJSON with nil Components should error")
	}
	if err := WriteJSON(&buf, &manifest.Manifest{Kind: manifest.KindUnknown}); err == nil {
		t.Fatal("WriteJSON with KindUnknown should error")
	}
}

// TestMarshalJSON_AppendUnknownsRawMessage guards against a common bug
// class: Unknown values are json.RawMessage and must be spliced in
// verbatim without re-marshalling.
func TestMarshalJSON_AppendUnknownsRawMessage(t *testing.T) {
	c := manifest.Component{
		Name:    "x",
		Version: strPtr("1"),
		Unknown: map[string]json.RawMessage{
			"x-raw": json.RawMessage(`{"nested":{"a":1,"b":[2,3]}}`),
		},
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"x-raw":{"nested":{"a":1,"b":[2,3]}}`)) {
		t.Fatalf("raw unknown value not spliced verbatim:\n%s", raw)
	}
}

func strPtr(s string) *string { return &s }
