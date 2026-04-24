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

func TestManifestPatch_Happy(t *testing.T) {
	dir := seedInitAndAdd(t)
	stdout, _, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./patches/fix.patch",
		"--type", "backport",
		"--resolves", "type=security,name=CVE-2024-1,url=https://example.com",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "registered backport patch on libx") {
		t.Fatalf("stdout missing confirmation: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "1 resolves entry") {
		t.Fatalf("stdout missing resolves count: %q", stdout.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	ped := m.Components.Components[0].Pedigree
	if ped == nil || len(ped.Patches) != 1 {
		t.Fatalf("pedigree.patches: %+v", ped)
	}
}

func TestManifestPatch_AbsoluteDiffRejected(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, stderr, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "/tmp/evil.patch",
		"--type", "backport",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stderr.String(), "diff path") {
		t.Fatalf("stderr missing diff path message: %q", stderr.String())
	}
}

func TestManifestPatch_InvalidTypeExits1(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, stderr, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./p.patch",
		"--type", "garbage",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stderr.String(), "invalid patch type") {
		t.Fatalf("stderr missing 'invalid patch type': %q", stderr.String())
	}
}

func TestManifestPatch_RequiresTypeFlag(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, _, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./p.patch",
	)
	if err == nil {
		t.Fatal("expected usage error for missing --type")
	}
}

func TestManifestPatch_NotesAppend(t *testing.T) {
	dir := seedInitAndAdd(t)
	// First patch sets initial notes.
	if _, _, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./p1.patch",
		"--type", "backport",
		"--notes", "first",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	// Second patch appends.
	stdout, _, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./p2.patch",
		"--type", "backport",
		"--notes", "second",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "pedigree.notes appended") {
		t.Fatalf("stdout missing 'pedigree.notes appended': %q", stdout.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	notes := *m.Components.Components[0].Pedigree.Notes
	if notes != "first\n\nsecond" {
		t.Fatalf("notes: got %q", notes)
	}
}

func TestManifestPatch_ResolvesParseError(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, _, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./p.patch",
		"--type", "backport",
		"--resolves", "malformed",
	)
	if got := exitCodeOf(err); got != exitUsageError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitUsageError, err)
	}
}

func TestManifestPatch_DryRunNoWrite(t *testing.T) {
	dir := seedInitAndAdd(t)
	before, _ := os.ReadFile(filepath.Join(dir, ".components.json"))

	stdout, _, err := withArgs(t,
		"manifest", "patch", "-C", dir,
		"pkg:generic/libx@1.0", "./p.patch",
		"--type", "backport",
		"--dry-run",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "would register") {
		t.Fatalf("stdout missing 'would register': %q", stdout.String())
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	if string(before) != string(after) {
		t.Fatal("--dry-run wrote to disk")
	}
}
