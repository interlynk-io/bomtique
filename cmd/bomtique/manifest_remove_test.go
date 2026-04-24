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

// seedInitAndAdd runs `manifest init` then one `manifest add` so the
// directory has a primary + components.json ready for remove tests.
func seedInitAndAdd(t *testing.T) string {
	t.Helper()
	dir := seedInitDir(t)
	if _, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "libx", "--version", "1.0", "--purl", "pkg:generic/libx@1.0",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("seed add: %v", err)
	}
	return dir
}

func TestManifestRemove_PoolHappyPath(t *testing.T) {
	dir := seedInitAndAdd(t)
	stdout, _, err := withArgs(t,
		"manifest", "remove", "-C", dir, "pkg:generic/libx@1.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "removed libx") {
		t.Fatalf("stdout missing 'removed libx': %q", stdout.String())
	}

	// Components file should be empty (len 0) — note this is a spec
	// violation (§5.2) but manifest-level; validator will flag it on
	// the next `validate` run. Remove itself does not enforce §5.2 so
	// users can add another component.
	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	if len(m.Components.Components) != 0 {
		t.Fatalf("components not emptied: %+v", m.Components.Components)
	}
}

func TestManifestRemove_ScrubsPrimaryEdge(t *testing.T) {
	dir := seedInitAndAdd(t)
	// Add the ref to primary depends-on.
	if _, _, err := withArgs(t,
		"manifest", "add", "-C", dir, "--primary",
		"--name", "libx", "--version", "1.0", "--purl", "pkg:generic/libx@1.0",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	stdout, stderr, err := withArgs(t,
		"manifest", "remove", "-C", dir, "pkg:generic/libx@1.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "also scrubbed") {
		t.Fatalf("stdout missing 'also scrubbed': %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "scrubbed depends-on edge") {
		t.Fatalf("stderr missing scrub warning: %q", stderr.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".primary.json"))
	m, _ := manifest.ParseJSON(data, "p")
	if len(m.Primary.Primary.DependsOn) != 0 {
		t.Fatalf("primary depends-on not scrubbed: %v", m.Primary.Primary.DependsOn)
	}
}

func TestManifestRemove_NotFoundExits1(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, stderr, err := withArgs(t,
		"manifest", "remove", "-C", dir, "pkg:generic/missing@1",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitValidationError, err)
	}
	if !strings.Contains(stderr.String(), "no component") {
		t.Fatalf("stderr missing 'no component': %q", stderr.String())
	}
}

func TestManifestRemove_PrimaryFormLeavesPool(t *testing.T) {
	dir := seedInitAndAdd(t)
	if _, _, err := withArgs(t,
		"manifest", "add", "-C", dir, "--primary",
		"--name", "libx", "--version", "1.0", "--purl", "pkg:generic/libx@1.0",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}

	_, _, err := withArgs(t,
		"manifest", "remove", "-C", dir, "--primary", "pkg:generic/libx@1.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}

	// Pool should still carry libx.
	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	if len(m.Components.Components) != 1 {
		t.Fatalf("pool unexpectedly modified: len=%d", len(m.Components.Components))
	}
	// Primary depends-on should be empty.
	data2, _ := os.ReadFile(filepath.Join(dir, ".primary.json"))
	m2, _ := manifest.ParseJSON(data2, "p")
	if len(m2.Primary.Primary.DependsOn) != 0 {
		t.Fatalf("primary depends-on not scrubbed: %v", m2.Primary.Primary.DependsOn)
	}
}

func TestManifestRemove_DryRunNoWrite(t *testing.T) {
	dir := seedInitAndAdd(t)
	before, _ := os.ReadFile(filepath.Join(dir, ".components.json"))

	stdout, _, err := withArgs(t,
		"manifest", "remove", "-C", dir, "--dry-run", "pkg:generic/libx@1.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "would remove") {
		t.Fatalf("stdout missing 'would remove': %q", stdout.String())
	}

	after, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	if string(before) != string(after) {
		t.Fatalf("--dry-run wrote to disk\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestManifestRemove_RequiresOneArg(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, _, err := withArgs(t, "manifest", "remove", "-C", dir)
	if err == nil {
		t.Fatal("expected error when ref arg is missing")
	}
}

func TestManifestRemove_EndToEnd_InitAddRemoveValidate(t *testing.T) {
	dir := seedInitAndAdd(t)
	if _, _, err := withArgs(t,
		"manifest", "add", "-C", dir,
		"--name", "liby", "--version", "2", "--purl", "pkg:generic/liby@2",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	// Remove libx.
	if _, _, err := withArgs(t,
		"manifest", "remove", "-C", dir, "pkg:generic/libx@1.0",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	// Validate still passes — liby remains.
	if _, _, err := withArgs(t, "validate", dir); exitCodeOf(err) != exitOK {
		t.Fatalf("validate after remove: %v", err)
	}
}
