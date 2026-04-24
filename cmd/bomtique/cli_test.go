// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
)

// withArgs runs the root command with args under an isolated stdout /
// stderr capture, returning the captured bytes and any error.  Commands
// signal exit codes via exitErr wrapping; tests inspect those rather
// than calling os.Exit.
func withArgs(t *testing.T, args ...string) (stdout, stderr *bytes.Buffer, err error) {
	t.Helper()
	cmd := newRootCmd()

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)

	// diag writes to os.Stderr by default; redirect into our own buffer
	// so tests can assert on warning output without touching the real
	// stderr.
	diag.SetSink(stderr)
	diag.Reset()
	t.Cleanup(func() {
		diag.SetSink(nil)
		diag.Reset()
	})

	err = cmd.Execute()
	return
}

// exitCodeOf unwraps an *exitErr to its code, or returns 1 for any
// other non-nil error, or 0 for nil.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitErr
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// -----------------------------------------------------------------------------
// validate
// -----------------------------------------------------------------------------

func TestValidate_CleanAppendixExampleExitsZero(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix", "b1.json")
	_, _, err := withArgs(t, "validate", path)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit code: got %d, want 0; err=%v", got, err)
	}
}

func TestValidate_BrokenManifestExitsOne(t *testing.T) {
	// Craft a manifest with an empty name.
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	bad := `{"schema":"primary-manifest/v1","primary":{"name":"","version":"1"}}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, stderr, err := withArgs(t, "validate", path)
	if got := exitCodeOf(err); got != exitValidationError {
		t.Fatalf("exit code: got %d, want 1; err=%v", got, err)
	}
	if !strings.Contains(stderr.String(), "required-field") {
		t.Fatalf("expected required-field diagnostic in stderr:\n%s", stderr.String())
	}
}

func TestValidate_MissingFileExitsIOError(t *testing.T) {
	_, _, err := withArgs(t, "validate", "/nonexistent/path/to/nothing.json")
	if got := exitCodeOf(err); got != exitIOError {
		t.Fatalf("exit code: got %d, want 3; err=%v", got, err)
	}
}

func TestValidate_MissingArgsIsUsageError(t *testing.T) {
	_, _, err := withArgs(t, "validate")
	if err == nil {
		t.Fatal("expected error from missing args")
	}
	// cobra signals usage errors as bare errors; our exit handler maps
	// those to exitValidationError (1).  That's a known gap — we rely
	// on cobra's own stderr "Error: requires at least 1 arg(s)".
	if !strings.Contains(err.Error(), "arg") {
		t.Fatalf("expected arg-count error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// generate
// -----------------------------------------------------------------------------

func TestGenerate_WritesPerPrimaryFile(t *testing.T) {
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	out := t.TempDir()
	_, _, err := withArgs(t,
		"generate",
		filepath.Join(appendix, "b3_server_primary.json"),
		filepath.Join(appendix, "b3_shared_components.json"),
		"--out", out,
		"--source-date-epoch", "1700000000",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit code: got %d, want 0; err=%v", got, err)
	}
	want := filepath.Join(out, "acme-server-1.0.0.cdx.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected output %s: %v", want, err)
	}
}

func TestGenerate_ByteIdenticalWithSDE(t *testing.T) {
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	args := []string{
		"generate",
		filepath.Join(appendix, "b3_server_primary.json"),
		filepath.Join(appendix, "b3_shared_components.json"),
		"--source-date-epoch", "1700000000",
		"--stdout",
	}
	stdoutA, _, err := withArgs(t, args...)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	stdoutB, _, err := withArgs(t, args...)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if !bytes.Equal(stdoutA.Bytes(), stdoutB.Bytes()) {
		t.Fatalf("byte mismatch between runs:\n--- a:\n%s\n--- b:\n%s", stdoutA.Bytes(), stdoutB.Bytes())
	}
}

func TestGenerate_FormatSPDXWritesPerPrimary(t *testing.T) {
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	out := t.TempDir()
	_, _, err := withArgs(t,
		"generate",
		filepath.Join(appendix, "b1.json"),
		"--format", "spdx",
		"--out", out,
		"--source-date-epoch", "1700000000",
	)
	if got := exitCodeOf(err); got != exitOK {
		t.Fatalf("exit code: got %d, want 0; err=%v", got, err)
	}
	want := filepath.Join(out, "acme-server-1.0.0.spdx.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected output %s: %v", want, err)
	}
	// Sanity-check the SPDX document shape.
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), `"spdxVersion":"SPDX-2.3"`) {
		t.Fatalf("output is not SPDX 2.3:\n%s", data)
	}
	if !strings.Contains(string(data), `"relationshipType":"DESCRIBES"`) {
		t.Fatalf("output missing DESCRIBES relationship:\n%s", data)
	}
}

func TestGenerate_UnknownFormatIsUsageError(t *testing.T) {
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	_, _, err := withArgs(t,
		"generate",
		filepath.Join(appendix, "b1.json"),
		"--format", "bogus",
	)
	if got := exitCodeOf(err); got != exitUsageError {
		t.Fatalf("exit code: got %d, want 2; err=%v", got, err)
	}
}

func TestGenerate_StdoutNDJSON(t *testing.T) {
	// With a single primary, --stdout writes one JSON line.
	appendix := filepath.Join("..", "..", "internal", "manifest", "testdata", "appendix")
	stdout, _, err := withArgs(t,
		"generate",
		filepath.Join(appendix, "b1.json"),
		"--stdout",
		"--source-date-epoch", "1700000000",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if strings.Contains(got, "\n") {
		t.Fatalf("single primary should yield single JSON line:\n%s", got)
	}
	if !strings.HasPrefix(got, `{"bomFormat":"CycloneDX"`) {
		t.Fatalf("unexpected stdout:\n%s", got)
	}
}

// -----------------------------------------------------------------------------
// manifest schema
// -----------------------------------------------------------------------------

func TestManifestSchema_PrintsPlaceholder(t *testing.T) {
	stdout, _, err := withArgs(t, "manifest", "schema")
	if err != nil {
		t.Fatalf("manifest schema: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "primary-manifest/v1") || !strings.Contains(out, "component-manifest/v1") {
		t.Fatalf("placeholder should mention both schema markers:\n%s", out)
	}
	if !strings.Contains(out, "draft/2020-12") {
		t.Fatalf("placeholder should cite draft 2020-12 $schema:\n%s", out)
	}
}
