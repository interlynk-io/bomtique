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

	"github.com/interlynk-io/bomtique/internal/manifest"
)

func TestUpdate_ReplaceLicense(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "libx", Version: strPtr("1.0"),
			License: &manifest.License{Expression: "MIT"},
			Purl:    strPtr("pkg:generic/libx@1.0"),
		},
	})

	res, err := Update(UpdateOptions{
		FromDir: dir,
		Ref:     "pkg:generic/libx@1.0",
		License: "Apache-2.0",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !containsStr(res.FieldsChanged, "license") {
		t.Fatalf("license not in FieldsChanged: %v", res.FieldsChanged)
	}

	cm, _ := parseComponentsFile(res.Path)
	if cm.Components[0].License.Expression != "Apache-2.0" {
		t.Fatalf("license not updated: %+v", cm.Components[0].License)
	}
}

func TestUpdate_ToBumpsVersionAndPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1.0"), Purl: strPtr("pkg:generic/libx@1.0")},
	})

	res, err := Update(UpdateOptions{
		FromDir:   dir,
		Ref:       "pkg:generic/libx@1.0",
		ToVersion: "2.0",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !res.PurlVersionBumped {
		t.Fatal("PurlVersionBumped should be true")
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	if *c.Version != "2.0" {
		t.Fatalf("version: got %q", derefStr(c.Version))
	}
	if *c.Purl != "pkg:generic/libx@2.0" {
		t.Fatalf("purl: got %q", derefStr(c.Purl))
	}
}

func TestUpdate_ToWithMismatchedPurlLeavesPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	// Purl version segment is "0.1", component version is "1.0".
	// --to 2.0 should bump component version, warn about purl, leave
	// purl alone. Ref uses the purl (§11 precedence makes the
	// component's identity the purl).
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1.0"), Purl: strPtr("pkg:generic/libx@0.1")},
	})

	warnBuf := withDiagSink(t)
	res, err := Update(UpdateOptions{
		FromDir:   dir,
		Ref:       "pkg:generic/libx@0.1",
		ToVersion: "2.0",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if res.PurlVersionBumped {
		t.Fatal("PurlVersionBumped should be false when segment mismatches")
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	if *c.Version != "2.0" {
		t.Fatalf("version: got %q", derefStr(c.Version))
	}
	if *c.Purl != "pkg:generic/libx@0.1" {
		t.Fatalf("purl should be unchanged, got %q", derefStr(c.Purl))
	}
	if !strings.Contains(warnBuf.String(), "purl") {
		t.Fatalf("expected purl warning, stderr: %q", warnBuf.String())
	}
}

func TestUpdate_PedigreePatchesPreserved(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	patches := []manifest.Patch{
		{
			Type: "backport",
			Diff: &manifest.Diff{URL: strPtr("./fix.patch")},
		},
	}
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "libx", Version: strPtr("1.0"),
			Purl: strPtr("pkg:github/acme/libx/src/vendor-libx@1.0"),
			Pedigree: &manifest.Pedigree{
				Ancestors: []manifest.Ancestor{
					{Name: "up", Version: strPtr("1.0")},
				},
				Patches: patches,
			},
		},
	})

	res, err := Update(UpdateOptions{
		FromDir:   dir,
		Ref:       "pkg:github/acme/libx/src/vendor-libx@1.0",
		ToVersion: "1.1",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	if c.Pedigree == nil || len(c.Pedigree.Patches) != 1 {
		t.Fatalf("patches lost: %+v", c.Pedigree)
	}
	if c.Pedigree.Patches[0].Type != "backport" {
		t.Fatalf("patch type changed: %+v", c.Pedigree.Patches[0])
	}
}

func TestUpdate_ClearLicense(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "libx", Version: strPtr("1.0"),
			License: &manifest.License{Expression: "MIT"},
		},
	})
	_, err := Update(UpdateOptions{
		FromDir:      dir,
		Ref:          "libx@1.0",
		ClearLicense: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	if cm.Components[0].License != nil {
		t.Fatalf("license not cleared: %+v", cm.Components[0].License)
	}
}

func TestUpdate_ClearPedigreePatches(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{
			Name: "libx", Version: strPtr("1.0"),
			Purl: strPtr("pkg:generic/libx@1.0"),
			Pedigree: &manifest.Pedigree{
				Patches: []manifest.Patch{
					{Type: "backport", Diff: &manifest.Diff{URL: strPtr("./p")}},
				},
			},
		},
	})
	_, err := Update(UpdateOptions{
		FromDir:              dir,
		Ref:                  "pkg:generic/libx@1.0",
		ClearPedigreePatches: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	if cm.Components[0].Pedigree != nil && len(cm.Components[0].Pedigree.Patches) != 0 {
		t.Fatalf("patches not cleared: %+v", cm.Components[0].Pedigree)
	}
}

func TestUpdate_IdentityCollisionAfterBump(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	// Two siblings; bumping libx to 2.0 would collide with liby's purl.
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1.0"), Purl: strPtr("pkg:generic/libx@1.0")},
		{Name: "libx", Version: strPtr("2.0"), Purl: strPtr("pkg:generic/libx@2.0")},
	})

	_, err := Update(UpdateOptions{
		FromDir:   dir,
		Ref:       "pkg:generic/libx@1.0",
		ToVersion: "2.0",
	})
	if err == nil {
		t.Fatal("expected identity collision after version bump")
	}
	var coll *ErrIdentityCollision
	if !errors.As(err, &coll) {
		t.Fatalf("expected *ErrIdentityCollision, got %T: %v", err, err)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1.0"), Purl: strPtr("pkg:generic/libx@1.0")},
	})
	_, err := Update(UpdateOptions{FromDir: dir, Ref: "pkg:generic/missing@1", License: "MIT"})
	if !errors.Is(err, ErrUpdateNotFound) {
		t.Fatalf("expected ErrUpdateNotFound, got %v", err)
	}
}

func TestUpdate_NoChangesError(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	_, err := Update(UpdateOptions{FromDir: dir, Ref: "pkg:generic/libx@1"})
	if err == nil {
		t.Fatal("Update with no changes should error")
	}
	if !strings.Contains(err.Error(), "no changes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdate_DryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	poolPath := filepath.Join(dir, ".components.json")
	seedPoolWith(t, poolPath, []manifest.Component{
		{
			Name: "libx", Version: strPtr("1"),
			License: &manifest.License{Expression: "MIT"},
			Purl:    strPtr("pkg:generic/libx@1"),
		},
	})
	beforeRes, _ := parseComponentsFile(poolPath)
	var before bytes.Buffer
	if err := WriteJSON(&before, &manifest.Manifest{
		Kind:       manifest.KindComponents,
		Format:     manifest.FormatJSON,
		Components: beforeRes,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := Update(UpdateOptions{
		FromDir: dir,
		Ref:     "pkg:generic/libx@1",
		License: "Apache-2.0",
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("Update --dry-run: %v", err)
	}

	afterRes, _ := parseComponentsFile(poolPath)
	if afterRes.Components[0].License.Expression != "MIT" {
		t.Fatalf("dry-run wrote change to disk: %+v", afterRes.Components[0].License)
	}
}

func TestUpdate_NameVersionRef(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1")},
	})
	res, err := Update(UpdateOptions{
		FromDir:     dir,
		Ref:         "libx@1",
		Description: "updated",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	if cm.Components[0].Description == nil || *cm.Components[0].Description != "updated" {
		t.Fatalf("description not updated: %+v", cm.Components[0].Description)
	}
}

func TestUpdate_IntoNarrowsSearch(t *testing.T) {
	// --into forces the update to a specific file. §11 is a global
	// invariant, so genuine multi-match across files means the pool is
	// already broken — update refuses those. This test just proves
	// --into steers the match to a file where the ref is unique.
	dir := t.TempDir()
	seedPrimary(t, dir)
	sub1 := filepath.Join(dir, "a")
	sub2 := filepath.Join(dir, "b")
	mkdirAll(t, sub1)
	mkdirAll(t, sub2)
	seedPoolWith(t, filepath.Join(sub1, ".components.json"), []manifest.Component{
		{Name: "libx", Version: strPtr("1"), Purl: strPtr("pkg:generic/libx@1")},
	})
	seedPoolWith(t, filepath.Join(sub2, ".components.json"), []manifest.Component{
		{Name: "liby", Version: strPtr("1"), Purl: strPtr("pkg:generic/liby@1")},
	})

	res, err := Update(UpdateOptions{
		FromDir: dir,
		Into:    filepath.Join(sub1, ".components.json"),
		Ref:     "pkg:generic/libx@1",
		License: "MIT",
	})
	if err != nil {
		t.Fatalf("Update --into: %v", err)
	}
	if res.Path != filepath.Join(sub1, ".components.json") {
		t.Fatalf("path: got %q", res.Path)
	}
}

// helpers

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestUpdate_Primary_VersionBumpLockstepPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/app@1.0")

	res, err := Update(UpdateOptions{FromDir: dir, Primary: true, ToVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !res.PurlVersionBumped {
		t.Fatalf("expected PurlVersionBumped=true: %+v", res)
	}
	if !containsStr(res.FieldsChanged, "version") || !containsStr(res.FieldsChanged, "purl") {
		t.Fatalf("expected version+purl in FieldsChanged: %v", res.FieldsChanged)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.ParseJSON(data, "")
	if err != nil {
		t.Fatal(err)
	}
	p := m.Primary.Primary
	if p.Version == nil || *p.Version != "1.0.0" {
		t.Fatalf("version not bumped: %+v", p.Version)
	}
	if p.Purl == nil || *p.Purl != "pkg:github/acme/app@1.0.0" {
		t.Fatalf("purl not bumped: %+v", p.Purl)
	}
}

func TestUpdate_Primary_FieldReplace(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir) // no purl, license MIT

	_, err := Update(UpdateOptions{FromDir: dir, Primary: true, License: "Apache-2.0"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".primary.json"))
	m, _ := manifest.ParseJSON(data, "")
	if m.Primary.Primary.License == nil || m.Primary.Primary.License.Expression != "Apache-2.0" {
		t.Fatalf("license not updated: %+v", m.Primary.Primary.License)
	}
}

func TestUpdate_Primary_RejectsRef(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)

	_, err := Update(UpdateOptions{
		FromDir: dir,
		Primary: true,
		Ref:     "anything@1",
		License: "MIT",
	})
	if err == nil {
		t.Fatal("expected error for --primary with ref, got nil")
	}
	if !strings.Contains(err.Error(), "--primary takes no <ref>") {
		t.Fatalf("error should mention --primary: %v", err)
	}
}

func TestUpdate_Primary_DryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/app@1.0")
	before, _ := os.ReadFile(filepath.Join(dir, ".primary.json"))

	res, err := Update(UpdateOptions{
		FromDir: dir, Primary: true, ToVersion: "1.0.0", DryRun: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !res.DryRun {
		t.Fatalf("DryRun flag not propagated")
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".primary.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run wrote to disk")
	}
}
