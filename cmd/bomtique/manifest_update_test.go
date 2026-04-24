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

func TestManifestUpdate_ReplaceLicense(t *testing.T) {
	dir := seedInitAndAdd(t) // libx @ 1.0, MIT-ish default (add didn't set license)

	// First set a license via update so we have something to replace.
	if _, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
		"--license", "MIT",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("initial update: %v", err)
	}

	stdout, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
		"--license", "Apache-2.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "updated pkg:generic/libx@1.0") {
		t.Fatalf("stdout missing confirmation: %q", stdout.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	if m.Components.Components[0].License.Expression != "Apache-2.0" {
		t.Fatalf("license not updated: %+v", m.Components.Components[0].License)
	}
}

func TestManifestUpdate_ToBumpsVersionAndPurl(t *testing.T) {
	dir := seedInitAndAdd(t)
	stdout, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
		"--to", "2.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "purl bumped in lockstep") {
		t.Fatalf("stdout missing purl-bump note: %q", stdout.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	c := m.Components.Components[0]
	if *c.Version != "2.0" {
		t.Fatalf("version: got %q", derefOrEmpty(c.Version))
	}
	if *c.Purl != "pkg:generic/libx@2.0" {
		t.Fatalf("purl: got %q", derefOrEmpty(c.Purl))
	}
}

func TestManifestUpdate_NotFoundExits1(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, stderr, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/missing@1",
		"--license", "MIT",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitValidationError, err)
	}
	if !strings.Contains(stderr.String(), "no component") {
		t.Fatalf("stderr missing 'no component': %q", stderr.String())
	}
}

func TestManifestUpdate_NoChangesErrors(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit: got %d want %d; err=%v", got, exitValidationError, err)
	}
}

func TestManifestUpdate_DryRunNoWrite(t *testing.T) {
	dir := seedInitAndAdd(t)
	poolPath := filepath.Join(dir, ".components.json")
	before, _ := os.ReadFile(poolPath)

	stdout, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
		"--license", "MIT",
		"--dry-run",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit: got %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "would update") {
		t.Fatalf("stdout missing 'would update': %q", stdout.String())
	}

	after, _ := os.ReadFile(poolPath)
	if string(before) != string(after) {
		t.Fatalf("--dry-run wrote to disk")
	}
}

func TestManifestUpdate_ClearLicense(t *testing.T) {
	dir := seedInitAndAdd(t)
	// Seed a license first.
	if _, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
		"--license", "MIT",
	); exitCodeOf(err) != exitOK {
		t.Fatal(err)
	}
	if _, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"pkg:generic/libx@1.0",
		"--clear-license",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("clear-license: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".components.json"))
	m, _ := manifest.ParseJSON(data, "c")
	if m.Components.Components[0].License != nil {
		t.Fatalf("license not cleared: %+v", m.Components.Components[0].License)
	}
}

func TestManifestUpdate_RequiresOneArg(t *testing.T) {
	dir := seedInitAndAdd(t)
	_, _, err := withArgs(t,
		"manifest", "update", "-C", dir,
		"--license", "MIT",
	)
	if err == nil {
		t.Fatal("expected error when ref arg is missing")
	}
}

func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
