// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// seedPrimary drops a minimal .primary.json into dir so LocatePrimary
// can anchor LocateOrCreateComponents' fallback path.
func seedPrimary(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".primary.json"), []byte(samplePrimary), 0o644); err != nil {
		t.Fatal(err)
	}
}

// withDiagSink redirects the diag warning channel into a buffer for
// the duration of the test. Returns the buffer.
func withDiagSink(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	diag.SetSink(buf)
	diag.Reset()
	t.Cleanup(func() {
		diag.SetSink(nil)
		diag.Reset()
	})
	return buf
}

// --- Pool add ---

func TestAdd_Pool_CreatesComponentsJSON(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "libx", Version: "1.0",
		License: "MIT",
		Purl:    "pkg:generic/libx@1.0",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !res.Created {
		t.Fatal("Created should be true on first add")
	}
	if res.Path != filepath.Join(dir, ".components.json") {
		t.Fatalf("path: got %q", res.Path)
	}

	cm, err := parseComponentsFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Components) != 1 || cm.Components[0].Name != "libx" {
		t.Fatalf("components after add: %+v", cm.Components)
	}
}

func TestAdd_Pool_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	if _, err := Add(AddOptions{FromDir: dir, Name: "a", Version: "1", Purl: "pkg:generic/a@1"}); err != nil {
		t.Fatal(err)
	}
	res, err := Add(AddOptions{FromDir: dir, Name: "b", Version: "1", Purl: "pkg:generic/b@1"})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if res.Created {
		t.Fatal("Created should be false on second add")
	}

	cm, err := parseComponentsFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Components) != 2 {
		t.Fatalf("components count: got %d want 2", len(cm.Components))
	}
}

func TestAdd_Pool_IdentityCollision(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	if _, err := Add(AddOptions{FromDir: dir, Name: "x", Version: "1", Purl: "pkg:generic/x@1"}); err != nil {
		t.Fatal(err)
	}
	_, err := Add(AddOptions{FromDir: dir, Name: "x", Version: "1", Purl: "pkg:generic/x@1"})
	if err == nil {
		t.Fatal("expected identity collision")
	}
	var coll *ErrIdentityCollision
	if !errors.As(err, &coll) {
		t.Fatalf("expected *ErrIdentityCollision, got %T: %v", err, err)
	}
	if !strings.Contains(coll.Existing, "pkg:generic/x@1") {
		t.Fatalf("collision message missing existing purl: %+v", coll)
	}
}

func TestAdd_Pool_IntoExplicit(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	target := filepath.Join(dir, "team-a.components.json")

	res, err := Add(AddOptions{
		FromDir: dir, Into: target,
		Name: "l", Version: "1", Purl: "pkg:generic/l@1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if res.Path != target {
		t.Fatalf("path: got %q want %q", res.Path, target)
	}
	if !res.Created {
		t.Fatal("expected created=true for new --into path")
	}
}

func TestAdd_Pool_CSVTargetRejectsPedigree(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	// Use a CSV target; the component will fail CheckFitsCSV because
	// we populate a pedigree via --from.
	csvTarget := filepath.Join(dir, ".components.csv")
	if err := os.WriteFile(csvTarget, []byte(sampleComponentsCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	fromPath := filepath.Join(dir, "comp.json")
	if err := os.WriteFile(fromPath, []byte(`{
  "name": "libx",
  "version": "1",
  "pedigree": { "notes": "vendored" }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Add(AddOptions{
		FromDir:  dir,
		Into:     csvTarget,
		FromPath: fromPath,
	})
	if err == nil {
		t.Fatal("expected CSV rejection for pedigree-bearing component")
	}
	if !strings.Contains(err.Error(), "pedigree") {
		t.Fatalf("error does not mention pedigree: %v", err)
	}
	if !strings.Contains(err.Error(), "--into") {
		t.Fatalf("error does not suggest --into <json-path>: %v", err)
	}
}

func TestAdd_Pool_FromJSONFile(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	fromPath := filepath.Join(dir, "src.json")
	if err := os.WriteFile(fromPath, []byte(`{
  "name": "libx",
  "version": "1.0",
  "license": "MIT",
  "purl": "pkg:generic/libx@1.0",
  "external_references": [
    { "type": "vcs", "url": "https://git.example/libx" }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Add(AddOptions{FromDir: dir, FromPath: fromPath})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, err := parseComponentsFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	c := cm.Components[0]
	if c.Name != "libx" {
		t.Fatalf("name not lifted from file: %q", c.Name)
	}
	if len(c.ExternalReferences) != 1 {
		t.Fatalf("external_references not lifted: %+v", c.ExternalReferences)
	}
}

func TestAdd_Pool_FromStdinReader(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	body := `{"name":"stdin-lib","version":"1","license":"MIT","purl":"pkg:generic/stdin-lib@1"}`
	res, err := Add(AddOptions{
		FromDir:    dir,
		FromPath:   "-",
		FromReader: strings.NewReader(body),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, err := parseComponentsFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if cm.Components[0].Name != "stdin-lib" {
		t.Fatalf("stdin name not lifted: %+v", cm.Components[0])
	}
}

func TestAdd_Pool_FlagOverridesFromFile(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	fromPath := filepath.Join(dir, "src.json")
	if err := os.WriteFile(fromPath, []byte(`{
  "name": "libx",
  "version": "1.0",
  "license": "MIT",
  "purl": "pkg:generic/libx@1.0"
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	warnBuf := withDiagSink(t)

	res, err := Add(AddOptions{
		FromDir:  dir,
		FromPath: fromPath,
		Version:  "2.0",        // override
		License:  "Apache-2.0", // override
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, err := parseComponentsFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	c := cm.Components[0]
	if *c.Version != "2.0" {
		t.Fatalf("version override not applied: %v", derefStr(c.Version))
	}
	if c.License.Expression != "Apache-2.0" {
		t.Fatalf("license override not applied: %+v", c.License)
	}

	s := warnBuf.String()
	if !strings.Contains(s, "version") || !strings.Contains(s, "license") {
		t.Fatalf("override warnings missing from diag output: %q", s)
	}
}

func TestAdd_Pool_NoDataError(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	_, err := Add(AddOptions{FromDir: dir})
	if err == nil {
		t.Fatal("Add with no flags and no --from should error")
	}
}

func TestAdd_Pool_ValidationErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	// Name without version/purl/hashes → §6.1 identity failure.
	_, err := Add(AddOptions{FromDir: dir, Name: "lonely"})
	if err == nil {
		t.Fatal("Add with insufficient identity should fail")
	}
	var ve *ErrInitValidation
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ErrInitValidation (reused type), got %T: %v", err, err)
	}
}

// --- Primary depends-on ---

func TestAdd_Primary_AppendsRefFromPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	res, err := Add(AddOptions{
		FromDir: dir,
		Primary: true,
		Name:    "x", Version: "1",
		Purl: "pkg:generic/x@1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !res.ToPrimary {
		t.Fatal("ToPrimary should be true")
	}
	if res.Ref != "pkg:generic/x@1" {
		t.Fatalf("ref: got %q", res.Ref)
	}

	data, _ := os.ReadFile(res.Path)
	m, _ := manifest.ParseJSON(data, res.Path)
	if len(m.Primary.Primary.DependsOn) != 1 || m.Primary.Primary.DependsOn[0] != "pkg:generic/x@1" {
		t.Fatalf("depends-on after add: %v", m.Primary.Primary.DependsOn)
	}
}

func TestAdd_Primary_AppendsRefFromNameVersion(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	res, err := Add(AddOptions{
		FromDir: dir,
		Primary: true,
		Name:    "x", Version: "1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if res.Ref != "x@1" {
		t.Fatalf("ref: got %q want x@1", res.Ref)
	}
}

func TestAdd_Primary_Dedups(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	if _, err := Add(AddOptions{
		FromDir: dir, Primary: true, Name: "x", Version: "1", Purl: "pkg:generic/x@1",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := Add(AddOptions{
		FromDir: dir, Primary: true, Name: "x", Version: "1", Purl: "pkg:generic/x@1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyPresent {
		t.Fatal("expected AlreadyPresent=true on duplicate primary add")
	}

	data, _ := os.ReadFile(res.Path)
	m, _ := manifest.ParseJSON(data, res.Path)
	if got := len(m.Primary.Primary.DependsOn); got != 1 {
		t.Fatalf("depends-on after dup add: len=%d (expected 1, no change)", got)
	}
}

func TestAdd_Primary_NoIdentityErrors(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	_, err := Add(AddOptions{FromDir: dir, Primary: true, Name: "x"}) // no version/purl
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("expected ErrInvalidRef, got %v", err)
	}
}

func TestAdd_Primary_ScopeIgnoredWithWarning(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	warnBuf := withDiagSink(t)
	if _, err := Add(AddOptions{
		FromDir: dir, Primary: true,
		Name: "x", Version: "1", Purl: "pkg:generic/x@1",
		Scope: "optional",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warnBuf.String(), "scope") {
		t.Fatalf("expected scope warning in diag output: %q", warnBuf.String())
	}
}

func TestAdd_Primary_MissingPrimaryManifest(t *testing.T) {
	dir := t.TempDir() // no primary
	_, err := Add(AddOptions{FromDir: dir, Primary: true, Name: "x", Version: "1"})
	if !errors.Is(err, ErrPrimaryNotFound) {
		t.Fatalf("expected ErrPrimaryNotFound, got %v", err)
	}
}

// --- External refs flag translation ---

func TestAdd_Pool_ExternalRefsFromShorthands(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	res, err := Add(AddOptions{
		FromDir:      dir,
		Name:         "x",
		Version:      "1",
		Purl:         "pkg:generic/x@1",
		Website:      "https://example.com",
		VCS:          "https://git.example/x",
		IssueTracker: "https://bugs.example/x",
		ExternalRefs: []ExternalRefSpec{{Type: "support", URL: "https://support.example"}},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	refs := cm.Components[0].ExternalReferences
	if len(refs) != 4 {
		t.Fatalf("external_references count: got %d want 4: %+v", len(refs), refs)
	}
	types := map[string]string{}
	for _, r := range refs {
		types[r.Type] = r.URL
	}
	if types["website"] != "https://example.com" ||
		types["vcs"] != "https://git.example/x" ||
		types["issue-tracker"] != "https://bugs.example/x" ||
		types["support"] != "https://support.example" {
		t.Fatalf("external_refs mapping wrong: %+v", types)
	}
}
