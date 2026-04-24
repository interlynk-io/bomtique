// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// seedGitHubPrimary writes a primary whose purl is a pkg:github
// form, and drops a vendored subdir with one stub file so the §9.3
// derivation path has a real filesystem to point at.
func seedGitHubPrimary(t *testing.T) (dir string, vendorRel string) {
	t.Helper()
	dir = t.TempDir()
	if _, _, err := withArgs(t,
		"manifest", "init", "-C", dir,
		"--name", "device-firmware",
		"--version", "1.0",
		"--license", "Apache-2.0",
		"--purl", "pkg:github/acme/device-firmware@1.0",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	vendorRel = "src/vendor-libx"
	if err := os.MkdirAll(filepath.Join(dir, vendorRel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, vendorRel, "stub.c"), []byte("int main(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, vendorRel
}

func TestManifestAdd_VendoredAt_EndToEnd(t *testing.T) {
	dir, _ := seedGitHubPrimary(t)
	stdout, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "vendor-libx", "--version", "2.4.0",
		"--vendored-at", "./src/vendor-libx",
		"--ext", "c,h",
		"--upstream-name", "libx",
		"--upstream-version", "2.4.0",
		"--upstream-purl", "pkg:github/upstream-org/libx@2.4.0",
		"--upstream-supplier", "Upstream Inc",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "created") {
		t.Fatalf("stdout missing 'created': %q", stdout.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	c := m.Components.Components[0]

	if got := derefOrEmpty(c.Purl); got != "pkg:github/acme/device-firmware/src/vendor-libx@2.4.0" {
		t.Fatalf("derived purl: got %q", got)
	}
	if len(c.Hashes) != 1 || c.Hashes[0].Path == nil || *c.Hashes[0].Path != "./src/vendor-libx/" {
		t.Fatalf("hash directive: %+v", c.Hashes)
	}
	if c.Pedigree == nil || len(c.Pedigree.Ancestors) != 1 {
		t.Fatalf("pedigree.ancestors: %+v", c.Pedigree)
	}
	anc := c.Pedigree.Ancestors[0]
	if anc.Name != "libx" || anc.Supplier == nil || anc.Supplier.Name != "Upstream Inc" {
		t.Fatalf("ancestor shape: %+v", anc)
	}

	// The resulting manifest must still pass validation end-to-end.
	if _, _, err := withArgs(t, "validate", dir); exitCodeOf(err) != exitOK {
		t.Fatalf("validate after vendored add: %v", err)
	}
}

func TestManifestAdd_VendoredAt_NpmPrimaryFails(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := withArgs(t,
		"manifest", "init", "-C", dir,
		"--name", "app",
		"--version", "1",
		"--purl", "pkg:npm/app@1",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "v", "--version", "1",
		"--vendored-at", "./vendor",
		"--upstream-name", "u", "--upstream-version", "1",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d", got, exitValidationError)
	}
	if !strings.Contains(stderr.String(), "--purl") {
		t.Fatalf("stderr should suggest --purl: %q", stderr.String())
	}
}

func TestManifestAdd_VendoredAt_RejectsPrimary(t *testing.T) {
	dir, _ := seedGitHubPrimary(t)
	_, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--primary",
		"--name", "v", "--version", "1", "--purl", "pkg:generic/v@1",
		"--vendored-at", "./src/vendor-libx",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d", got, exitValidationError)
	}
}
