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

func TestManifestInit_HappyPath(t *testing.T) {
	dir := t.TempDir()
	stdout, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "acme-app",
		"--version", "1.0.0",
		"--license", "Apache-2.0",
		"--purl", "pkg:generic/acme-app@1.0.0",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit code: got %d want 0; err=%v", got, err)
	}

	target := filepath.Join(dir, ".primary.json")
	if !strings.Contains(stdout.String(), target) {
		t.Fatalf("stdout missing confirmation for %s: %q", target, stdout.String())
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	m, err := manifest.ParseJSON(data, target)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Primary.Primary.Name != "acme-app" {
		t.Fatalf("name: got %q", m.Primary.Primary.Name)
	}
}

func TestManifestInit_RefusesExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "p", "--version", "1",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("first init: %v", err)
	}

	_, stderr, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "p2", "--version", "2",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit code: got %d want %d; err=%v", got, exitValidationError, err)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("stderr missing 'already exists' hint: %q", stderr.String())
	}
}

func TestManifestInit_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "p", "--version", "1",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("first init: %v", err)
	}

	stdout, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--force",
		"--name", "p2", "--version", "2",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("--force init: got exit %d, err=%v", got, err)
	}
	if !strings.Contains(stdout.String(), "overwrote") {
		t.Fatalf("stdout missing 'overwrote' verb: %q", stdout.String())
	}

	target := filepath.Join(dir, ".primary.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.ParseJSON(data, target)
	if err != nil {
		t.Fatal(err)
	}
	if m.Primary.Primary.Name != "p2" {
		t.Fatalf("force didn't update name: got %q", m.Primary.Primary.Name)
	}
}

func TestManifestInit_MissingNameIsUsageError(t *testing.T) {
	dir := t.TempDir()
	_, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--version", "1",
	)
	// MarkFlagRequired surfaces as a non-exitErr error; cobra's default
	// maps this to exit 1 via our main routing. We accept either the
	// usage code OR the default validation code as "rejected".
	if err == nil {
		t.Fatal("expected error when --name is missing")
	}
}

func TestManifestInit_ValidationErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	// Name alone but no version/purl/hashes → §6.1 identity failure.
	_, stderr, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "p",
	)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit code: got %d want %d; err=%v", got, exitValidationError, err)
	}
	if !strings.Contains(stderr.String(), "validation:") {
		t.Fatalf("stderr missing validation prefix: %q", stderr.String())
	}
}

func TestManifestInit_DogfoodRoundTrip(t *testing.T) {
	// Run init, then the existing `validate` command against the same
	// directory: exit 0 proves the output is a conforming processing set
	// (modulo needing components, which §12.1 requires at least one
	// primary and the validator tolerates a pool-less run as long as
	// there is a primary).
	dir := t.TempDir()
	if _, _, err := withArgs(t,
		"manifest", "init",
		"-C", dir,
		"--name", "p", "--version", "1", "--license", "Apache-2.0",
	); exitCodeOf(err) != exitOK {
		t.Fatalf("init: %v", err)
	}
	_, _, err := withArgs(t, "validate", filepath.Join(dir, ".primary.json"))
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("validate after init: exit %d, err=%v", got, err)
	}
}
