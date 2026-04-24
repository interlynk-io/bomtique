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

// seedInitDir runs `manifest init` in a fresh tempdir and returns it.
func seedInitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "acme-app",
		"--version", "1.0",
		"--license", "Apache-2.0",
		"--purl", "pkg:generic/acme-app@1.0",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("seed init: %v", err)
	}
	return dir
}

func TestManifestAdd_PoolCreatesComponentsJSON(t *testing.T) {
	dir := seedInitDir(t)
	stdout, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir,
		"--name", "libx",
		"--version", "1.0",
		"--license", "MIT",
		"--purl", "pkg:generic/libx@1.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "created") {
		t.Fatalf("stdout missing 'created': %q", stdout.String())
	}

	target := filepath.Join(dir, ".components.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.ParseJSON(data, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Components.Components) != 1 || m.Components.Components[0].Name != "libx" {
		t.Fatalf("components after add: %+v", m.Components.Components)
	}
}

func TestManifestAdd_PoolAppends(t *testing.T) {
	dir := seedInitDir(t)
	if _, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "a", "--version", "1", "--purl", "pkg:generic/a@1",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	stdout, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "b", "--version", "1", "--purl", "pkg:generic/b@1",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("second add: exit %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("stdout missing 'updated': %q", stdout.String())
	}
}

func TestManifestAdd_IdentityCollisionExits1(t *testing.T) {
	dir := seedInitDir(t)
	if _, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "x", "--version", "1", "--purl", "pkg:generic/x@1",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	_, stderr, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "x", "--version", "1", "--purl", "pkg:generic/x@1",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitValidationError, err)
	}
	if !strings.Contains(stderr.String(), "identity collision") {
		t.Fatalf("stderr missing 'identity collision': %q", stderr.String())
	}
}

func TestManifestAdd_PrimaryAppendsRef(t *testing.T) {
	dir := seedInitDir(t)
	stdout, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir, "--primary",
		"--name", "libx", "--version", "1",
		"--purl", "pkg:generic/libx@1",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "added") || !strings.Contains(stdout.String(), "depends-on") {
		t.Fatalf("stdout missing confirmation: %q", stdout.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".primary.json"))
	m, _ := manifest.ParseJSON(data, "primary")
	deps := m.Primary.Primary.DependsOn
	if len(deps) != 1 || deps[0] != "pkg:generic/libx@1" {
		t.Fatalf("depends-on after add: %v", deps)
	}
}

func TestManifestAdd_PrimaryDedupsShowsUnchanged(t *testing.T) {
	dir := seedInitDir(t)
	if _, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir, "--primary",
		"--name", "x", "--version", "1", "--purl", "pkg:generic/x@1",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	stdout, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir, "--primary",
		"--name", "x", "--version", "1", "--purl", "pkg:generic/x@1",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("second primary add exit: %d", got)
	}
	if !strings.Contains(stdout.String(), "unchanged") {
		t.Fatalf("stdout missing 'unchanged' on dedup: %q", stdout.String())
	}
}

func TestManifestAdd_PrimaryScopeWarnsOnStderr(t *testing.T) {
	dir := seedInitDir(t)
	_, stderr, err := withArgs(t,
		"manifest", "add",
		"-C", dir, "--primary",
		"--name", "x", "--version", "1", "--purl", "pkg:generic/x@1",
		"--scope", "optional",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stderr.String(), "scope") {
		t.Fatalf("stderr missing scope warning: %q", stderr.String())
	}
}

func TestManifestAdd_FromStdin(t *testing.T) {
	dir := seedInitDir(t)

	// Construct a root cmd manually so we can plug stdin.
	cmd := newRootCmd()
	stdinBody := `{"name":"stdin-lib","version":"1","license":"MIT","purl":"pkg:generic/stdin-lib@1"}`
	cmd.SetIn(strings.NewReader(stdinBody))

	// Buffered stdout/stderr for assertions.
	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"manifest", "add", "-C", dir, "--from", "-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr: %s", err, errBuf.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	if m.Components.Components[0].Name != "stdin-lib" {
		t.Fatalf("stdin name not lifted: %+v", m.Components.Components[0])
	}
}

func TestManifestAdd_CSVRejectsPedigreeFromFile(t *testing.T) {
	dir := seedInitDir(t)
	// Pre-create a CSV target so --into picks it up.
	csvPath := filepath.Join(dir, ".components.csv")
	if err := os.WriteFile(csvPath, []byte("#component-manifest/v1\n"+
		"name,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags\n"+
		"seed,1,,,,,,,,,,,,,\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromPath := filepath.Join(dir, "comp.json")
	if err := os.WriteFile(fromPath, []byte(`{
  "name":"libx","version":"1",
  "pedigree": { "notes": "vendored" }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := withArgs(t,
		"manifest", "add",
		"-C", dir,
		"--into", csvPath,
		"--from", fromPath,
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitValidationError, err)
	}
	if !strings.Contains(stderr.String(), "pedigree") {
		t.Fatalf("stderr missing pedigree message: %q", stderr.String())
	}
}

func TestManifestAdd_ExternalRepeated(t *testing.T) {
	dir := seedInitDir(t)
	_, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir,
		"--name", "x", "--version", "1", "--purl", "pkg:generic/x@1",
		"--external", "vcs=https://git.example/x",
		"--external", "support=https://support.example/x",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	refs := m.Components.Components[0].ExternalReferences
	if len(refs) != 2 {
		t.Fatalf("external_references count: got %d want 2: %+v", len(refs), refs)
	}
}

func TestManifestAdd_ExternalMalformed(t *testing.T) {
	dir := seedInitDir(t)
	_, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir,
		"--name", "x", "--version", "1",
		"--external", "not-a-pair",
	)
	if got := exitCodeOf(err); got != exitUsageError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitUsageError, err)
	}
}

func TestManifestAdd_InitThenAddThenValidate(t *testing.T) {
	dir := seedInitDir(t)
	if _, _, err := withArgs(t,
		"manifest", "add",
		"-C", dir,
		"--name", "libx", "--version", "1",
		"--license", "MIT", "--purl", "pkg:generic/libx@1",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	// Reach the primary with `validate`.
	_, _, err := withArgs(t, "validate", dir)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("validate after add: exit %d, err=%v", got, err)
	}
}
