// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// seedPoolWith writes a components manifest with the given entries.
func seedPoolWith(t *testing.T, path string, components []manifest.Component) {
	t.Helper()
	m := &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatJSON,
		Path:   path,
		Components: &manifest.ComponentsManifest{
			Schema:     manifest.SchemaComponentsV1,
			Components: components,
		},
	}
	if err := writeManifest(path, m); err != nil {
		t.Fatalf("seed pool at %s: %v", path, err)
	}
}

func TestRemove_HappyPoolPath(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
		{Name: "b", Version: strPtr("1"), Purl: strPtr("pkg:generic/b@1")},
	})

	res, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/a@1"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if res.PoolPath != filepath.Join(dir, ".components.json") {
		t.Fatalf("pool path: got %q", res.PoolPath)
	}
	if res.PoolComponentName != "a" {
		t.Fatalf("removed component: got %q want a", res.PoolComponentName)
	}

	cm, _ := parseComponentsFile(res.PoolPath)
	if len(cm.Components) != 1 || cm.Components[0].Name != "b" {
		t.Fatalf("pool after remove: %+v", cm.Components)
	}
}

func TestRemove_NameVersionRef(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1")},
		{Name: "y", Version: strPtr("1")},
	})

	res, err := Remove(RemoveOptions{FromDir: dir, Ref: "x@1"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if res.PoolComponentName != "x" {
		t.Fatalf("removed: got %q", res.PoolComponentName)
	}
}

func TestRemove_ScrubsSiblingDependsOn(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1"),
			DependsOn: []string{"pkg:generic/target@1"},
		},
		{Name: "target", Version: strPtr("1"), Purl: strPtr("pkg:generic/target@1")},
	})

	res, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/target@1"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res.ScrubbedEdges) != 1 {
		t.Fatalf("ScrubbedEdges: got %d want 1: %+v", len(res.ScrubbedEdges), res.ScrubbedEdges)
	}
	if res.ScrubbedEdges[0].FromName != "a" {
		t.Fatalf("scrubbed from: got %q", res.ScrubbedEdges[0].FromName)
	}
	cm, _ := parseComponentsFile(res.PoolPath)
	var sibling *manifest.Component
	for i := range cm.Components {
		if cm.Components[i].Name == "a" {
			sibling = &cm.Components[i]
		}
	}
	if sibling == nil {
		t.Fatalf("sibling 'a' missing from pool after remove")
	}
	if len(sibling.DependsOn) != 0 {
		t.Fatalf("sibling depends-on not scrubbed: %v", sibling.DependsOn)
	}
}

func TestRemove_ScrubsPrimaryDependsOn(t *testing.T) {
	dir := t.TempDir()
	// Seed a primary that already depends on target.
	primary := filepath.Join(dir, ".primary.json")
	primaryBody := `{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "p", "version": "1",
    "depends-on": ["pkg:generic/target@1"]
  }
}`
	if err := os.WriteFile(primary, []byte(primaryBody), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "target", Version: strPtr("1"), Purl: strPtr("pkg:generic/target@1")},
	})

	res, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/target@1"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !res.PrimaryEdgeScrubbed {
		t.Fatal("PrimaryEdgeScrubbed should be true")
	}
	data, _ := os.ReadFile(primary)
	m, _ := manifest.ParseJSON(data, primary)
	if len(m.Primary.Primary.DependsOn) != 0 {
		t.Fatalf("primary depends-on not scrubbed: %v", m.Primary.Primary.DependsOn)
	}
}

func TestRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
	})

	_, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/missing@1"})
	if !errors.Is(err, ErrRemoveNotFound) {
		t.Fatalf("expected ErrRemoveNotFound, got %v", err)
	}
}

func TestRemove_MultiMatchAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	// Two components manifests in sibling subdirs, both carrying the
	// same purl (they'd collide at scan time, but that's the whole
	// point of the error — user needs to pick one).
	sub1 := filepath.Join(dir, "team-a")
	sub2 := filepath.Join(dir, "team-b")
	if err := os.MkdirAll(sub1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	seedPoolWith(t, filepath.Join(sub1, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:generic/x@1")},
	})
	seedPoolWith(t, filepath.Join(sub2, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:generic/x@1")},
	})

	_, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/x@1"})
	if err == nil {
		t.Fatal("expected multi-match error")
	}
	var mm *ErrRemoveMultiMatch
	if !errors.As(err, &mm) {
		t.Fatalf("expected *ErrRemoveMultiMatch, got %T: %v", err, err)
	}
	if len(mm.Hits) != 2 {
		t.Fatalf("Hits: got %d want 2", len(mm.Hits))
	}
}

func TestRemove_IntoDisambiguates(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	sub1 := filepath.Join(dir, "team-a")
	sub2 := filepath.Join(dir, "team-b")
	if err := os.MkdirAll(sub1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	seedPoolWith(t, filepath.Join(sub1, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:generic/x@1")},
	})
	seedPoolWith(t, filepath.Join(sub2, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:generic/x@1")},
	})

	res, err := Remove(RemoveOptions{
		FromDir: dir,
		Into:    filepath.Join(sub1, ".components.json"),
		Ref:     "pkg:generic/x@1",
	})
	if err != nil {
		t.Fatalf("Remove --into: %v", err)
	}
	if res.PoolPath != filepath.Join(sub1, ".components.json") {
		t.Fatalf("pool path: got %q", res.PoolPath)
	}

	// team-b's copy should still be there.
	cm, _ := parseComponentsFile(filepath.Join(sub2, ".components.json"))
	if len(cm.Components) != 1 {
		t.Fatalf("team-b untouched count: got %d want 1", len(cm.Components))
	}
}

func TestRemove_PrimaryFormScrubs(t *testing.T) {
	dir := t.TempDir()
	// Primary with one depends-on entry; NO pool on disk.
	primary := filepath.Join(dir, ".primary.json")
	body := `{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "p", "version": "1",
    "depends-on": ["pkg:generic/x@1", "pkg:generic/y@1"]
  }
}`
	if err := os.WriteFile(primary, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Remove(RemoveOptions{FromDir: dir, Primary: true, Ref: "pkg:generic/x@1"})
	if err != nil {
		t.Fatalf("Remove --primary: %v", err)
	}
	if !res.PrimaryEdgeScrubbed {
		t.Fatal("PrimaryEdgeScrubbed should be true")
	}
	data, _ := os.ReadFile(primary)
	m, _ := manifest.ParseJSON(data, primary)
	if len(m.Primary.Primary.DependsOn) != 1 || m.Primary.Primary.DependsOn[0] != "pkg:generic/y@1" {
		t.Fatalf("depends-on after --primary remove: %v", m.Primary.Primary.DependsOn)
	}
}

func TestRemove_PrimaryFormMissingEdgeError(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir) // sample primary has no depends-on
	_, err := Remove(RemoveOptions{FromDir: dir, Primary: true, Ref: "pkg:generic/x@1"})
	if err == nil {
		t.Fatal("expected error when ref is not in depends-on")
	}
	if !strings.Contains(err.Error(), "no entry matching") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRemove_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	poolPath := filepath.Join(dir, ".components.json")
	seedPoolWith(t, poolPath, []manifest.Component{
		{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
	})
	before, err := os.ReadFile(poolPath)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/a@1", DryRun: true})
	if err != nil {
		t.Fatalf("Remove --dry-run: %v", err)
	}
	if !res.DryRun {
		t.Fatal("RemoveResult.DryRun should mirror the flag")
	}

	after, err := os.ReadFile(poolPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("dry-run wrote to disk\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRemove_InvalidRefSurfaces(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	_, err := Remove(RemoveOptions{FromDir: dir, Ref: "not a ref"})
	if err == nil {
		t.Fatal("expected parse error for malformed ref")
	}
}

func TestRemove_NoPrimary(t *testing.T) {
	dir := t.TempDir()
	_, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/x@1"})
	if !errors.Is(err, ErrPrimaryNotFound) {
		t.Fatalf("expected ErrPrimaryNotFound, got %v", err)
	}
}

func TestRemove_EmptyDependsOnAfterScrub(t *testing.T) {
	// When the scrub empties a depends-on slice, the kept slice must
	// be nil so the manifest re-serialises without an empty array
	// (keeps the model stable for round-trip).
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1"),
			DependsOn: []string{"pkg:generic/target@1"},
		},
		{Name: "target", Version: strPtr("1"), Purl: strPtr("pkg:generic/target@1")},
	})

	if _, err := Remove(RemoveOptions{FromDir: dir, Ref: "pkg:generic/target@1"}); err != nil {
		t.Fatal(err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	for i := range cm.Components {
		if cm.Components[i].Name == "a" && cm.Components[i].DependsOn != nil {
			t.Fatalf("depends-on should be nil after full scrub, got %v", cm.Components[i].DependsOn)
		}
	}
}
