// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"
)

// TestGenerate_OutputValidateCycloneDX asserts that a normal CDX run
// with --output-validate compiles the schema, validates the emitted
// output, and exits 0. This is the positive check: bomtique's own
// emitter produces schema-conformant CycloneDX 1.7 JSON.
func TestGenerate_OutputValidateCycloneDX(t *testing.T) {
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	out := t.TempDir()
	_, _, err := withArgs(t,
		"generate",
		filepath.Join(appendix, "b3_server_primary.json"),
		filepath.Join(appendix, "b3_shared_components.json"),
		"--out", out,
		"--source-date-epoch", "1700000000",
		"--output-validate",
	)
	if code := exitCodeOf(err); code != exitOK {
		t.Fatalf("exit code: got %d, want 0; err=%v", code, err)
	}
}

// TestGenerate_OutputValidateSPDX asserts the same for SPDX 2.3.
func TestGenerate_OutputValidateSPDX(t *testing.T) {
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	out := t.TempDir()
	_, _, err := withArgs(t,
		"generate",
		filepath.Join(appendix, "b1.json"),
		"--format", "spdx",
		"--out", out,
		"--source-date-epoch", "1700000000",
		"--output-validate",
	)
	if code := exitCodeOf(err); code != exitOK {
		t.Fatalf("exit code: got %d, want 0; err=%v", code, err)
	}
}
