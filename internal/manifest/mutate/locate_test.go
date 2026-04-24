// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const samplePrimary = `{
  "schema": "primary-manifest/v1",
  "primary": { "name": "p", "version": "1" }
}
`

const sampleComponents = `{
  "schema": "component-manifest/v1",
  "components": [ { "name": "c", "version": "1" } ]
}
`

const sampleComponentsCSV = `#component-manifest/v1
name,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags
libx,1.0,,,,,,,,,,,,,
`

func TestLocatePrimary_FindsInCWD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".primary.json"), samplePrimary)
	path, err := LocatePrimary(dir)
	if err != nil {
		t.Fatalf("LocatePrimary: %v", err)
	}
	if path != filepath.Join(dir, ".primary.json") {
		t.Fatalf("path: got %q want %q", path, filepath.Join(dir, ".primary.json"))
	}
}

func TestLocatePrimary_WalksUp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".primary.json"), samplePrimary)
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := LocatePrimary(sub)
	if err != nil {
		t.Fatalf("LocatePrimary: %v", err)
	}
	if path != filepath.Join(root, ".primary.json") {
		t.Fatalf("path: got %q want %q", path, filepath.Join(root, ".primary.json"))
	}
}

func TestLocatePrimary_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LocatePrimary(dir)
	if !errors.Is(err, ErrPrimaryNotFound) {
		t.Fatalf("expected ErrPrimaryNotFound, got %v", err)
	}
}

func TestLocatePrimary_StopsAtExcludedBoundary(t *testing.T) {
	// A .git directory below the primary must NOT cause the walk to
	// surface the primary from a path nested inside .git.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".primary.json"), samplePrimary)
	// Create .git/<sub> and start the walk there.
	gitSub := filepath.Join(root, ".git", "objects")
	if err := os.MkdirAll(gitSub, 0o755); err != nil {
		t.Fatal(err)
	}
	// shouldStopAt fires when the upward walk crosses into `.git`
	// (the parent of `objects` is `.git`). LocatePrimary starts IN
	// gitSub which means its absolute path starts there; walking up
	// hits `.git` as the parent. Since shouldStopAt(".git") is true,
	// the walk terminates before reaching the primary.
	_, err := LocatePrimary(gitSub)
	if !errors.Is(err, ErrPrimaryNotFound) {
		t.Fatalf("expected ErrPrimaryNotFound when walk crosses .git, got %v", err)
	}
}

func TestLocatePrimary_IgnoresComponentsManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".primary.json"), sampleComponents) // wrong marker
	_, err := LocatePrimary(dir)
	if !errors.Is(err, ErrPrimaryNotFound) {
		t.Fatalf("expected ErrPrimaryNotFound when .primary.json is actually a components manifest, got %v", err)
	}
}

func TestLocateOrCreateComponents_FlagIntoExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "other.components.json")
	writeFile(t, target, sampleComponents)
	path, created, err := LocateOrCreateComponents(dir, target)
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	if path != target {
		t.Fatalf("path: got %q want %q", path, target)
	}
	if created {
		t.Fatal("expected created=false for existing file")
	}
}

func TestLocateOrCreateComponents_FlagIntoMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "fresh.components.json")
	path, created, err := LocateOrCreateComponents(dir, target)
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	if path != target {
		t.Fatalf("path: got %q want %q", path, target)
	}
	if !created {
		t.Fatal("expected created=true for missing file")
	}
}

func TestLocateOrCreateComponents_FlagIntoRelative(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Relative to fromDir (sub), ../shared.components.json should
	// resolve to dir/shared.components.json.
	path, created, err := LocateOrCreateComponents(sub, filepath.Join("..", "shared.components.json"))
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	want := filepath.Join(dir, "shared.components.json")
	if path != want {
		t.Fatalf("path: got %q want %q", path, want)
	}
	if !created {
		t.Fatal("expected created=true")
	}
}

func TestLocateOrCreateComponents_FindsExistingJSONInCWD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".primary.json"), samplePrimary)
	writeFile(t, filepath.Join(dir, ".components.json"), sampleComponents)
	path, created, err := LocateOrCreateComponents(dir, "")
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	if path != filepath.Join(dir, ".components.json") {
		t.Fatalf("path: got %q want %q", path, filepath.Join(dir, ".components.json"))
	}
	if created {
		t.Fatal("expected created=false")
	}
}

func TestLocateOrCreateComponents_FindsExistingCSVInCWD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".primary.json"), samplePrimary)
	writeFile(t, filepath.Join(dir, ".components.csv"), sampleComponentsCSV)
	path, created, err := LocateOrCreateComponents(dir, "")
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	if path != filepath.Join(dir, ".components.csv") {
		t.Fatalf("path: got %q want %q", path, filepath.Join(dir, ".components.csv"))
	}
	if created {
		t.Fatal("expected created=false")
	}
}

func TestLocateOrCreateComponents_JSONPreferredOverCSV(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".primary.json"), samplePrimary)
	writeFile(t, filepath.Join(dir, ".components.json"), sampleComponents)
	writeFile(t, filepath.Join(dir, ".components.csv"), sampleComponentsCSV)
	path, _, err := LocateOrCreateComponents(dir, "")
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	if path != filepath.Join(dir, ".components.json") {
		t.Fatalf("path: got %q want .components.json", path)
	}
}

func TestLocateOrCreateComponents_WalksUpToPrimary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".primary.json"), samplePrimary)
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path, created, err := LocateOrCreateComponents(sub, "")
	if err != nil {
		t.Fatalf("LocateOrCreateComponents: %v", err)
	}
	want := filepath.Join(root, ".components.json")
	if path != want {
		t.Fatalf("path: got %q want %q", path, want)
	}
	if !created {
		t.Fatal("expected created=true when falling back to alongside-primary")
	}
}

func TestLocateOrCreateComponents_NoPrimaryNoFlag(t *testing.T) {
	dir := t.TempDir()
	_, _, err := LocateOrCreateComponents(dir, "")
	if !errors.Is(err, ErrPrimaryNotFound) {
		t.Fatalf("expected ErrPrimaryNotFound, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
