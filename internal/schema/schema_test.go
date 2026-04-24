// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/schema"
	vendored "github.com/interlynk-io/bomtique/schemas"
)

func TestValidator_CycloneDXDogfood(t *testing.T) {
	fsys, entry, err := vendored.CycloneDX17()
	if err != nil {
		t.Fatalf("vendored CycloneDX17: %v", err)
	}
	v, err := schema.New(fsys, entry)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "sbom", "bomtique-0.1.0.cdx.json"))
	if err != nil {
		t.Fatalf("read dogfood SBOM: %v", err)
	}
	if err := v.Validate(data); err != nil {
		t.Fatalf("dogfood CycloneDX SBOM fails schema validation: %v", err)
	}
}

func TestValidator_SPDXMinimal(t *testing.T) {
	fsys, entry, err := vendored.SPDX23()
	if err != nil {
		t.Fatalf("vendored SPDX23: %v", err)
	}
	v, err := schema.New(fsys, entry)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// A hand-authored minimal SPDX 2.3 doc that should satisfy the
	// schema (matches the shape bomtique's SPDX emitter produces).
	doc := map[string]any{
		"spdxVersion":       "SPDX-2.3",
		"dataLicense":       "CC0-1.0",
		"SPDXID":            "SPDXRef-DOCUMENT",
		"name":              "minimal",
		"documentNamespace": "https://example.com/minimal-ns",
		"creationInfo": map[string]any{
			"created":  "2024-01-01T00:00:00Z",
			"creators": []string{"Tool: bomtique-test"},
		},
		"packages": []any{
			map[string]any{
				"SPDXID":           "SPDXRef-Package-minimal",
				"name":             "minimal",
				"downloadLocation": "NOASSERTION",
				"filesAnalyzed":    false,
			},
		},
		"relationships": []any{
			map[string]any{
				"spdxElementId":      "SPDXRef-DOCUMENT",
				"relationshipType":   "DESCRIBES",
				"relatedSpdxElement": "SPDXRef-Package-minimal",
			},
		},
	}
	bytes, _ := json.Marshal(doc)
	if err := v.Validate(bytes); err != nil {
		t.Fatalf("minimal SPDX fails schema validation: %v", err)
	}
}

func TestValidator_RejectsBrokenCycloneDX(t *testing.T) {
	fsys, entry, err := vendored.CycloneDX17()
	if err != nil {
		t.Fatal(err)
	}
	v, err := schema.New(fsys, entry)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// `bomFormat` is required; flipping it to an int makes the doc
	// structurally invalid per the schema.
	bad := []byte(`{"bomFormat":42,"specVersion":"1.7","version":1}`)
	if err := v.Validate(bad); err == nil {
		t.Fatal("expected validation error on broken bomFormat, got nil")
	}
}

func TestValidator_RejectsBrokenSPDX(t *testing.T) {
	fsys, entry, err := vendored.SPDX23()
	if err != nil {
		t.Fatal(err)
	}
	v, err := schema.New(fsys, entry)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Missing required `SPDXID` on the document.
	bad := []byte(`{"spdxVersion":"SPDX-2.3","dataLicense":"CC0-1.0","name":"broken"}`)
	if err := v.Validate(bad); err == nil {
		t.Fatal("expected validation error on missing SPDXID, got nil")
	}
}

func TestValidator_MalformedJSON(t *testing.T) {
	fsys, entry, _ := vendored.CycloneDX17()
	v, _ := schema.New(fsys, entry)
	err := v.Validate([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error wording, got %v", err)
	}
}
