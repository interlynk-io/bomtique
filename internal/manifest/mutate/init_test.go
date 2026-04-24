// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

func TestInit_HappyPath(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitOptions{
		Dir:         dir,
		Name:        "bomtique",
		Version:     "0.1.0",
		License:     "Apache-2.0",
		Description: "Reference consumer",
		Purl:        "pkg:github/interlynk-io/bomtique@0.1.0",
		Supplier:    "Interlynk.io",
		SupplierURL: "https://interlynk.io",
		Website:     "https://github.com/interlynk-io/bomtique",
		VCS:         "https://github.com/interlynk-io/bomtique",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if res.Overwrote {
		t.Fatal("Overwrote should be false on a clean Init")
	}
	if res.Path != filepath.Join(dir, ".primary.json") {
		t.Fatalf("path: got %q want %q", res.Path, filepath.Join(dir, ".primary.json"))
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !bytes.HasSuffix(data, []byte{'\n'}) {
		t.Fatal("written file missing trailing newline")
	}

	parsed, err := manifest.ParseJSON(data, res.Path)
	if err != nil {
		t.Fatalf("parse written file: %v", err)
	}
	p := parsed.Primary
	if p.Schema != manifest.SchemaPrimaryV1 {
		t.Fatalf("schema: got %q", p.Schema)
	}
	if p.Primary.Name != "bomtique" {
		t.Fatalf("name: got %q", p.Primary.Name)
	}
	if p.Primary.Type == nil || *p.Primary.Type != "application" {
		t.Fatalf("default type: got %+v want application", p.Primary.Type)
	}
	if p.Primary.License == nil || p.Primary.License.Expression != "Apache-2.0" {
		t.Fatalf("license: got %+v", p.Primary.License)
	}
	if p.Primary.Supplier == nil || p.Primary.Supplier.Name != "Interlynk.io" {
		t.Fatalf("supplier: got %+v", p.Primary.Supplier)
	}
	if got := len(p.Primary.ExternalReferences); got != 2 {
		t.Fatalf("external_references count: got %d want 2", got)
	}
}

func TestInit_DefaultTypeApplication(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitOptions{Dir: dir, Name: "p", Version: "1"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	c := res.Manifest.Primary.Primary
	if c.Type == nil || *c.Type != "application" {
		t.Fatalf("default type not application: got %+v", c.Type)
	}
}

func TestInit_CustomType(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitOptions{Dir: dir, Name: "p", Version: "1", Type: "firmware"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	c := res.Manifest.Primary.Primary
	if c.Type == nil || *c.Type != "firmware" {
		t.Fatalf("type not firmware: got %+v", c.Type)
	}
}

func TestInit_RefuseExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitOptions{Dir: dir, Name: "p", Version: "1"}); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	_, err := Init(InitOptions{Dir: dir, Name: "p2", Version: "2"})
	if !errors.Is(err, ErrPrimaryExists) {
		t.Fatalf("expected ErrPrimaryExists, got %v", err)
	}
}

func TestInit_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitOptions{Dir: dir, Name: "p", Version: "1"}); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	res, err := Init(InitOptions{Dir: dir, Name: "p2", Version: "2", Force: true})
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if !res.Overwrote {
		t.Fatal("Overwrote should be true on --force re-init")
	}
	if res.Manifest.Primary.Primary.Name != "p2" {
		t.Fatalf("name not replaced: got %q", res.Manifest.Primary.Primary.Name)
	}
}

func TestInit_ForcePreservesUnknown(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing .primary.json with unknown top-level AND
	// unknown component-level keys.
	seed := `{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "old",
    "version": "0.1",
    "x-team": "infra"
  },
  "x-ticket": "JIRA-1"
}
`
	target := filepath.Join(dir, ".primary.json")
	if err := os.WriteFile(target, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Init(InitOptions{Dir: dir, Name: "new", Version: "1.0", Force: true})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !res.Overwrote {
		t.Fatal("Overwrote should be true")
	}

	// Re-read the file and confirm unknowns came along.
	out, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.ParseJSON(out, target)
	if err != nil {
		t.Fatalf("parse written file: %v", err)
	}

	got, ok := m.Primary.Unknown["x-ticket"]
	if !ok {
		t.Fatalf("top-level unknown not preserved:\n%s", out)
	}
	if string(got) != `"JIRA-1"` {
		t.Fatalf("top-level unknown value mismatch: got %s want %q", got, `"JIRA-1"`)
	}

	got2, ok := m.Primary.Primary.Unknown["x-team"]
	if !ok {
		t.Fatalf("component unknown not preserved:\n%s", out)
	}
	if string(got2) != `"infra"` {
		t.Fatalf("component unknown value mismatch: got %s want %q", got2, `"infra"`)
	}
}

func TestInit_ValidationFailure_MissingName(t *testing.T) {
	dir := t.TempDir()
	_, err := Init(InitOptions{Dir: dir, Version: "1"})
	if err == nil {
		t.Fatal("Init should fail when Name is empty")
	}
	var ve *ErrInitValidation
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ErrInitValidation, got %T: %v", err, err)
	}
	if len(ve.Errors) == 0 {
		t.Fatal("ErrInitValidation has empty Errors slice")
	}
}

func TestInit_ValidationFailure_NoIdentityField(t *testing.T) {
	dir := t.TempDir()
	// Name alone isn't enough per §6.1 — need version, purl, or hashes.
	_, err := Init(InitOptions{Dir: dir, Name: "p"})
	if err == nil {
		t.Fatal("Init should fail when no identity field is present")
	}
	var ve *ErrInitValidation
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ErrInitValidation, got %T: %v", err, err)
	}
}

func TestInit_NonExistentDir(t *testing.T) {
	target := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := Init(InitOptions{Dir: target, Name: "p", Version: "1"})
	if err == nil {
		t.Fatal("Init should fail when target dir does not exist")
	}
}

func TestInit_DirIsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Init(InitOptions{Dir: filePath, Name: "p", Version: "1"})
	if err == nil {
		t.Fatal("Init should fail when Dir points at a regular file")
	}
}

func TestInit_OutputIsCanonical(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitOptions{
		Dir:     dir,
		Name:    "p",
		Version: "1",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	// Canonical layout: struct-declaration field order, 2-space indent.
	wantPrefix := "{\n  \"schema\": \"primary-manifest/v1\",\n  \"primary\": {\n    \"name\": \"p\","
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("output prefix mismatch\n got: %q\nwant prefix: %q", got, wantPrefix)
	}
}

// Guard against a common footgun: InitOptions.Name gets the value
// "\t p " (leading/trailing whitespace). Spec §6.1 strips whitespace
// for "non-empty" semantics; Init mirrors that.
func TestInit_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitOptions{Dir: dir, Name: " p ", Version: " 1 "})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	c := res.Manifest.Primary.Primary
	if c.Name != "p" {
		t.Fatalf("name not trimmed: got %q", c.Name)
	}
	if c.Version == nil || *c.Version != "1" {
		t.Fatalf("version not trimmed: got %+v", c.Version)
	}
}

// Belt-and-braces check that the on-disk file is exactly the WriteJSON
// output, including no Unknown entries marshalled on a clean init.
func TestInit_CleanInitHasNoUnknowns(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitOptions{Dir: dir, Name: "p", Version: "1"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	m := res.Manifest
	if m.Primary.Unknown != nil {
		t.Fatalf("clean init should have no top-level unknowns: %v", m.Primary.Unknown)
	}
	if m.Primary.Primary.Unknown != nil {
		t.Fatalf("clean init should have no component unknowns: %v", m.Primary.Primary.Unknown)
	}
}

// TestInit_RawMessageInvariant guards the json.RawMessage-based
// Unknown map contract.
func TestInit_RawMessageInvariant(t *testing.T) {
	dir := t.TempDir()
	seed := `{
  "schema": "primary-manifest/v1",
  "primary": { "name": "p", "version": "1" },
  "x-meta": {"a":1,"b":[2,3]}
}`
	target := filepath.Join(dir, ".primary.json")
	if err := os.WriteFile(target, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Init(InitOptions{Dir: dir, Name: "p", Version: "1", Force: true})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	unk := res.Manifest.Primary.Unknown
	if unk == nil {
		t.Fatal("Unknown map lost on Force re-init")
	}
	raw, ok := unk["x-meta"]
	if !ok {
		t.Fatalf("x-meta missing: %v", unk)
	}
	// Confirm it parses back correctly.
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("x-meta is not valid JSON: %v (%s)", err, raw)
	}
}
