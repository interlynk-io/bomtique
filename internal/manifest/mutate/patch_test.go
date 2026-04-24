// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

func TestPatch_FirstPatchCreatesPedigree(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})

	res, err := Patch(PatchOptions{
		FromDir:  dir,
		Ref:      "pkg:generic/libx@1",
		Type:     "backport",
		DiffPath: "./patches/fix.patch",
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if res.PatchType != "backport" {
		t.Fatalf("PatchType: got %q", res.PatchType)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	if c.Pedigree == nil || len(c.Pedigree.Patches) != 1 {
		t.Fatalf("pedigree.patches: %+v", c.Pedigree)
	}
	p := c.Pedigree.Patches[0]
	if p.Type != "backport" {
		t.Fatalf("patch.type: got %q", p.Type)
	}
	if p.Diff == nil || p.Diff.URL == nil || *p.Diff.URL != "./patches/fix.patch" {
		t.Fatalf("patch.diff.url: %+v", p.Diff)
	}
}

func TestPatch_SecondPatchAppends(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})

	if _, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p/1.patch",
	}); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	if _, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "cherry-pick", DiffPath: "./p/2.patch",
	}); err != nil {
		t.Fatalf("second patch: %v", err)
	}

	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	patches := cm.Components[0].Pedigree.Patches
	if len(patches) != 2 {
		t.Fatalf("patches count: got %d want 2", len(patches))
	}
	if patches[0].Type != "backport" || patches[1].Type != "cherry-pick" {
		t.Fatalf("patches out of order: %+v", patches)
	}
}

func TestPatch_InvalidTypeRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})

	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "bogus", DiffPath: "./p.patch",
	})
	if !errors.Is(err, ErrPatchInvalidType) {
		t.Fatalf("expected ErrPatchInvalidType, got %v", err)
	}
}

func TestPatch_AbsoluteDiffPathRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "/tmp/evil.patch",
	})
	if err == nil {
		t.Fatal("expected absolute path rejection")
	}
	if !strings.Contains(err.Error(), "diff path") {
		t.Fatalf("error does not mention diff path: %v", err)
	}
}

func TestPatch_TraversalDiffPathRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "../../outside.patch",
	})
	if err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestPatch_ResolvesEntries(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p.patch",
		Resolves: []ResolvesSpec{
			{Type: "security", Name: "CVE-2024-1", URL: "https://example.com/1"},
			{Type: "defect", Name: "BUG-1"},
		},
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	rs := cm.Components[0].Pedigree.Patches[0].Resolves
	if len(rs) != 2 {
		t.Fatalf("resolves count: got %d want 2", len(rs))
	}
	if rs[0].Type == nil || *rs[0].Type != "security" {
		t.Fatalf("resolves[0].type: %+v", rs[0].Type)
	}
	if rs[0].Name == nil || *rs[0].Name != "CVE-2024-1" {
		t.Fatalf("resolves[0].name: %+v", rs[0].Name)
	}
	if rs[1].URL != nil {
		t.Fatalf("resolves[1].url should be nil (not provided): %+v", rs[1].URL)
	}
}

func TestPatch_InvalidResolvesType(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p.patch",
		Resolves: []ResolvesSpec{{Type: "bogus", Name: "X"}},
	})
	if !errors.Is(err, ErrPatchInvalidResolvesType) {
		t.Fatalf("expected ErrPatchInvalidResolvesType, got %v", err)
	}
}

func TestPatch_ResolvesRequiresNameOrID(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p.patch",
		Resolves: []ResolvesSpec{{Type: "security"}}, // missing name/id
	})
	if err == nil {
		t.Fatal("expected error for resolves without name/id")
	}
}

func TestPatch_NotesAppend(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1"),
			Pedigree: &manifest.Pedigree{Notes: strPtr("old note")},
		},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p.patch",
		Notes: "new note",
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	got := *cm.Components[0].Pedigree.Notes
	want := "old note\n\nnew note"
	if got != want {
		t.Fatalf("notes append: got %q want %q", got, want)
	}
}

func TestPatch_NotesReplace(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1"),
			Pedigree: &manifest.Pedigree{Notes: strPtr("old note")},
		},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p.patch",
		Notes:        "replacement",
		ReplaceNotes: true,
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	if got := *cm.Components[0].Pedigree.Notes; got != "replacement" {
		t.Fatalf("notes replace: got %q", got)
	}
}

func TestPatch_DryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	poolPath := filepath.Join(dir, ".components.json")
	seedPoolWith(t, poolPath, []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport", DiffPath: "./p.patch", DryRun: true,
	})
	if err != nil {
		t.Fatalf("Patch --dry-run: %v", err)
	}
	cm, _ := parseComponentsFile(poolPath)
	if cm.Components[0].Pedigree != nil {
		t.Fatalf("dry-run wrote pedigree: %+v", cm.Components[0].Pedigree)
	}
}

func TestPatch_NotFound(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/missing@1",
		Type: "backport", DiffPath: "./p.patch",
	})
	if !errors.Is(err, ErrUpdateNotFound) {
		t.Fatalf("expected ErrUpdateNotFound, got %v", err)
	}
}

func TestPatch_EmptyDiffPath(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Patch(PatchOptions{
		FromDir: dir, Ref: "pkg:generic/libx@1",
		Type: "backport",
	})
	if err == nil {
		t.Fatal("expected error for empty diff path")
	}
}
