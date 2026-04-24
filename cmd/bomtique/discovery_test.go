// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// mkTree materialises a directory tree under root; values are file
// contents, directory-only keys have a trailing "/".
func mkTree(t *testing.T, root string, layout map[string]string) {
	t.Helper()
	for rel, content := range layout {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if content == "DIR" {
			if err := os.MkdirAll(abs, 0o700); err != nil {
				t.Fatalf("mkdir %s: %v", abs, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
}

// -----------------------------------------------------------------------------
// Discovery picks up the expected basenames.
// -----------------------------------------------------------------------------

func TestDiscover_FindsConventionalBasenames(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{
		".primary.json":            "x",
		"service/.primary.json":    "x",
		"service/.components.json": "x",
		"service/.components.csv":  "x",
		"unrelated.json":           "x", // not matching pattern
	})
	got, err := discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want := []string{
		filepath.Join(root, ".primary.json"),
		filepath.Join(root, "service", ".components.csv"),
		filepath.Join(root, "service", ".components.json"),
		filepath.Join(root, "service", ".primary.json"),
	}
	sort.Strings(want)
	sort.Strings(got)
	if !equal(got, want) {
		t.Fatalf("discover mismatch:\n got  %v\n want %v", got, want)
	}
}

// -----------------------------------------------------------------------------
// Discovery skips the excluded directory set.
// -----------------------------------------------------------------------------

func TestDiscover_SkipsExcludedDirs(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{
		".git/.primary.json":         "x", // .git/ excluded
		"node_modules/.primary.json": "x",
		"vendor/.primary.json":       "x",
		".venv/.primary.json":        "x",
		".anything/.primary.json":    "x", // any .-prefixed dir
		"testdata/.primary.json":     "x", // Go's test-fixture convention
		"src/.primary.json":          "x", // KEEP
	})
	got, err := discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want := []string{filepath.Join(root, "src", ".primary.json")}
	if !equal(got, want) {
		t.Fatalf("discover mismatch:\n got  %v\n want %v", got, want)
	}
}

// -----------------------------------------------------------------------------
// Discovery traversal order is deterministic (WalkDir's sorted ReadDir).
// -----------------------------------------------------------------------------

func TestDiscover_DeterministicOrder(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{
		"z/.primary.json":    "x",
		"a/.primary.json":    "x",
		"m/.components.json": "x",
	})
	first, err := discover(root)
	if err != nil {
		t.Fatalf("discover #1: %v", err)
	}
	second, err := discover(root)
	if err != nil {
		t.Fatalf("discover #2: %v", err)
	}
	if !equal(first, second) {
		t.Fatalf("not deterministic:\n first  %v\n second %v", first, second)
	}
	// Expected lexicographic order based on directory names.
	want := []string{
		filepath.Join(root, "a", ".primary.json"),
		filepath.Join(root, "m", ".components.json"),
		filepath.Join(root, "z", ".primary.json"),
	}
	if !equal(first, want) {
		t.Fatalf("order mismatch:\n got  %v\n want %v", first, want)
	}
}

// -----------------------------------------------------------------------------
// Non-matching files are ignored (not errors).
// -----------------------------------------------------------------------------

func TestDiscover_IgnoresOtherFiles(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{
		"src/README.md":             "x",
		"src/somebody.primary.json": "x", // not exactly ".primary.json"
	})
	got, err := discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}

// -----------------------------------------------------------------------------
// readManifests silently skips non-manifest files at discovery level.
// -----------------------------------------------------------------------------

func TestReadManifests_DirectoryDiscoversAndSkipsNoMarker(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, map[string]string{
		".primary.json":    `{"schema":"primary-manifest/v1","primary":{"name":"app","version":"1"}}`,
		".components.json": `{"schema":"component-manifest/v1","components":[{"name":"lib","version":"1"}]}`,
		// A file that happens to match the basename but has no marker
		// should be silently ignored per §12.5.
	})
	// Overwrite .components.json with a no-marker variant to trigger
	// ErrNoSchemaMarker in the parser.
	if err := os.WriteFile(filepath.Join(root, ".components.json"), []byte(`{"foo":1}`), 0o600); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	ms, err := readManifests([]string{root}, io.Discard)
	if err != nil {
		t.Fatalf("readManifests: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("expected 1 manifest (no-marker skipped), got %d", len(ms))
	}
}

// -----------------------------------------------------------------------------
// helpers.
// -----------------------------------------------------------------------------

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
