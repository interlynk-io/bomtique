// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package hash_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/interlynk-io/bomtique/internal/hash"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// Known SHA-256 vector for "abc" from FIPS 180-4.
const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func TestFile_KnownVector(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abc.txt"), []byte("abc"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := hash.File(dir, "abc.txt", hash.SHA256, 0)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if got != abcSHA256 {
		t.Fatalf("digest mismatch\n  got  %q\n  want %q", got, abcSHA256)
	}
}

func TestFile_EmptyFile(t *testing.T) {
	// The SHA-256 of the empty byte string.
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty"), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := hash.File(dir, "empty", hash.SHA256, 0)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if got != emptySHA256 {
		t.Fatalf("empty-file digest mismatch: got %q", got)
	}
}

func TestFile_RejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Windows developer mode")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("abc"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := hash.File(dir, "link.txt", hash.SHA256, 0)
	if !errors.Is(err, safefs.ErrSymlink) {
		t.Fatalf("expected ErrSymlink, got %v", err)
	}
}

func TestFile_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := hash.File(dir, "../escape", hash.SHA256, 0)
	if !errors.Is(err, safefs.ErrTraversal) {
		t.Fatalf("expected ErrTraversal, got %v", err)
	}
}

func TestFile_OversizeRejected(t *testing.T) {
	dir := t.TempDir()
	big := bytes.Repeat([]byte("A"), 100)
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := hash.File(dir, "big.bin", hash.SHA256, 10)
	if !errors.Is(err, safefs.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestFile_Directory(t *testing.T) {
	// File form targeted at a directory is not a regular file → reject.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "asdir"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := hash.File(dir, "asdir", hash.SHA256, 0)
	if !errors.Is(err, safefs.ErrNotRegular) {
		t.Fatalf("expected ErrNotRegular, got %v", err)
	}
}
