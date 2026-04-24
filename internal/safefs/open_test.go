// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

func TestReadFile_Regular(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	want := []byte("hello world")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := safefs.ReadFile(dir, "file.txt", 0)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadFile_NestedRegular(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := safefs.ReadFile(dir, "sub/deep/f.txt", 0)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestReadFile_Missing(t *testing.T) {
	dir := t.TempDir()
	_, err := safefs.ReadFile(dir, "nope.txt", 0)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestReadFile_Directory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "asdir"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := safefs.ReadFile(dir, "asdir", 0)
	if !errors.Is(err, safefs.ErrNotRegular) {
		t.Fatalf("expected ErrNotRegular, got %v", err)
	}
}

func TestReadFile_SymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Windows developer mode / admin; skipping")
	}
	dir := t.TempDir()
	// Real file outside the manifest directory tree.
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("exfil"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := safefs.ReadFile(dir, "link.txt", 0)
	if !errors.Is(err, safefs.ErrSymlink) {
		t.Fatalf("expected ErrSymlink, got %v", err)
	}
}

func TestReadFile_SymlinkIntermediateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Windows developer mode / admin; skipping")
	}
	dir := t.TempDir()
	// A real subdir containing a real file.
	realSub := filepath.Join(t.TempDir(), "realsub")
	if err := os.MkdirAll(realSub, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realSub, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A symlink inside the manifest dir pointing at realSub.
	if err := os.Symlink(realSub, filepath.Join(dir, "sub")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := safefs.ReadFile(dir, "sub/file.txt", 0)
	if !errors.Is(err, safefs.ErrSymlink) {
		t.Fatalf("expected ErrSymlink on intermediate, got %v", err)
	}
}

func TestReadFile_OversizeRejected(t *testing.T) {
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("A"), 100)
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Cap well below actual size.
	_, err := safefs.ReadFile(dir, "big.bin", 10)
	if !errors.Is(err, safefs.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestReadFile_ExactCap(t *testing.T) {
	// File size exactly equal to the cap must read successfully.
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("B"), 10)
	if err := os.WriteFile(filepath.Join(dir, "edge.bin"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := safefs.ReadFile(dir, "edge.bin", 10)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestOpen_StreamingOversize(t *testing.T) {
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("C"), 1024)
	if err := os.WriteFile(filepath.Join(dir, "stream.bin"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	rc, err := safefs.Open(dir, "stream.bin", 100)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	// Drain in small chunks; the 101st byte should trigger ErrFileTooLarge.
	buf := make([]byte, 32)
	total := 0
	for {
		n, err := rc.Read(buf)
		total += n
		if err == nil {
			continue
		}
		if errors.Is(err, safefs.ErrFileTooLarge) {
			break
		}
		if errors.Is(err, io.EOF) {
			t.Fatalf("unexpected EOF at %d bytes, wanted oversize error", total)
		}
		t.Fatalf("unexpected error %v at %d bytes", err, total)
	}
	if total > 100 {
		t.Fatalf("read past cap before erroring: total=%d, cap=100", total)
	}
}

func TestOpen_StreamingWithinCap(t *testing.T) {
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("D"), 50)
	if err := os.WriteFile(filepath.Join(dir, "small.bin"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	rc, err := safefs.Open(dir, "small.bin", 100)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch")
	}
}

func TestCheckNoSymlinks_Clean(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "c.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	abs, err := safefs.ResolveRelative(dir, "a/b/c.txt")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := safefs.CheckNoSymlinks(dir, abs); err != nil {
		t.Fatalf("clean walk should pass: %v", err)
	}
}
