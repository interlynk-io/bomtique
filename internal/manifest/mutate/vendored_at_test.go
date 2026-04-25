// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedPrimaryWithPurl writes a primary manifest whose primary.purl is
// set to the supplied canonical value. Used to exercise the §9.3
// derivation path.
func seedPrimaryWithPurl(t *testing.T, dir, p string) {
	t.Helper()
	body := `{
  "schema": "primary-manifest/v1",
  "primary": { "name": "primary", "version": "1.0", "purl": "` + p + `" }
}`
	if err := os.WriteFile(filepath.Join(dir, ".primary.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkVendorDir(t *testing.T, root, relPath string) string {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop one file so the directory isn't empty — the hash directive
	// works either way, but this models real usage.
	if err := os.WriteFile(filepath.Join(full, "stub.c"), []byte("int main(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestAdd_VendoredAt_DerivesRepoLocalPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/device-firmware@1.0")
	mkVendorDir(t, dir, "src/vendor-libx")

	res, err := Add(AddOptions{
		FromDir:         dir,
		Name:            "vendor-libx",
		Version:         "2.4.0",
		VendoredAt:      "./src/vendor-libx",
		Extensions:      []string{"c", "h"},
		UpstreamName:    "libx",
		UpstreamVersion: "2.4.0",
		UpstreamPurl:    "pkg:github/upstream-org/libx@2.4.0",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]

	if c.Purl == nil || *c.Purl != "pkg:github/acme/device-firmware/src/vendor-libx@2.4.0" {
		t.Fatalf("repo-local purl derivation: got %q", derefStr(c.Purl))
	}
	if len(c.Hashes) != 1 || c.Hashes[0].Path == nil || *c.Hashes[0].Path != "./src/vendor-libx/" {
		t.Fatalf("hash directive: %+v", c.Hashes)
	}
	if got := c.Hashes[0].Algorithm; got != "SHA-256" {
		t.Fatalf("hash algorithm: got %q", got)
	}
	if got := c.Hashes[0].Extensions; !reflectDeepStr(got, []string{"c", "h"}) {
		t.Fatalf("hash extensions: got %v", got)
	}
	if c.Pedigree == nil || len(c.Pedigree.Ancestors) != 1 {
		t.Fatalf("ancestors: %+v", c.Pedigree)
	}
	anc := c.Pedigree.Ancestors[0]
	if anc.Name != "libx" || anc.Version == nil || *anc.Version != "2.4.0" {
		t.Fatalf("ancestor shape: %+v", anc)
	}
	if anc.Purl == nil || *anc.Purl != "pkg:github/upstream-org/libx@2.4.0" {
		t.Fatalf("ancestor purl: %+v", anc.Purl)
	}
}

func TestAdd_VendoredAt_NonRepoPrimaryRequiresExplicitPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:npm/some-lib@1.0")
	mkVendorDir(t, dir, "src/vendor")

	_, err := Add(AddOptions{
		FromDir:         dir,
		Name:            "v",
		Version:         "1",
		VendoredAt:      "./src/vendor",
		UpstreamName:    "up",
		UpstreamVersion: "1",
	})
	if err == nil {
		t.Fatal("expected derivation failure for npm primary")
	}
	if !strings.Contains(err.Error(), "--purl") {
		t.Fatalf("error should suggest --purl, got: %v", err)
	}
}

func TestAdd_VendoredAt_ExplicitPurlSkipsDerivation(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:npm/some-lib@1.0")
	mkVendorDir(t, dir, "src/vendor")

	res, err := Add(AddOptions{
		FromDir:         dir,
		Name:            "v",
		Version:         "1",
		Purl:            "pkg:generic/acme/vendor@1",
		VendoredAt:      "./src/vendor",
		UpstreamName:    "up",
		UpstreamVersion: "1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	if got := derefStr(cm.Components[0].Purl); got != "pkg:generic/acme/vendor@1" {
		t.Fatalf("purl: got %q want pkg:generic/acme/vendor@1", got)
	}
}

func TestAdd_VendoredAt_UpstreamPurlCollisionRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/device-firmware@1.0")
	mkVendorDir(t, dir, "src/vendor")

	// Supply the same purl on both sides. With no --ref, no fetch
	// happens, so we exercise the §9.3 collision check independently
	// of the network path.
	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		Purl:            "pkg:github/upstream-org/libx@2.4.0",
		VendoredAt:      "./src/vendor",
		UpstreamName:    "libx",
		UpstreamVersion: "2.4.0",
		UpstreamPurl:    "pkg:github/upstream-org/libx@2.4.0",
	})
	if err == nil {
		t.Fatal("expected §9.3 rejection on upstream==component purl")
	}
	if !strings.Contains(err.Error(), "§9.3") {
		t.Fatalf("error does not cite §9.3: %v", err)
	}
}

func TestAdd_VendoredAt_AbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		VendoredAt:   "/tmp/evil",
		UpstreamName: "u", UpstreamVersion: "1",
	})
	if err == nil {
		t.Fatal("expected absolute path rejection")
	}
}

func TestAdd_VendoredAt_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		VendoredAt:   "../escape",
		UpstreamName: "u", UpstreamVersion: "1",
	})
	if err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestAdd_VendoredAt_MissingDirRejected(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		VendoredAt:   "./does-not-exist",
		UpstreamName: "u", UpstreamVersion: "1",
	})
	if err == nil {
		t.Fatal("expected missing-dir rejection")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error should mention missing dir: %v", err)
	}
}

func TestAdd_VendoredAt_AncestorRequiresNameAndVersion(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	mkVendorDir(t, dir, "src/vendor")

	// Passing UpstreamVersion but not UpstreamName should fail.
	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		VendoredAt:      "./src/vendor",
		UpstreamVersion: "1",
	})
	if err == nil {
		t.Fatal("expected missing-upstream-name error")
	}
}

func TestAdd_VendoredAt_NoAncestorFlagsOK(t *testing.T) {
	// When zero upstream flags are supplied, no ancestor is emitted.
	// This is unusual (a vendored component without upstream metadata)
	// but spec-conforming: pedigree is optional, and we don't
	// manufacture ghost ancestors.
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/device-firmware@1.0")
	mkVendorDir(t, dir, "src/vendor")

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		VendoredAt: "./src/vendor",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	if cm.Components[0].Pedigree != nil {
		t.Fatalf("no upstream flags → no pedigree (got %+v)", cm.Components[0].Pedigree)
	}
	// Hash directive and derived purl still land.
	if cm.Components[0].Purl == nil {
		t.Fatal("derived purl missing")
	}
	if len(cm.Components[0].Hashes) != 1 {
		t.Fatalf("hash directive missing: %+v", cm.Components[0].Hashes)
	}
}

func TestAdd_VendoredAt_RejectsOnPrimaryFlag(t *testing.T) {
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	mkVendorDir(t, dir, "src/vendor")

	_, err := Add(AddOptions{
		FromDir: dir,
		Primary: true,
		Name:    "v", Version: "1",
		Purl:       "pkg:generic/v@1",
		VendoredAt: "./src/vendor",
	})
	if err == nil {
		t.Fatal("expected rejection when --vendored-at combined with --primary")
	}
}

func reflectDeepStr(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
