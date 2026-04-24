// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package hash_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/interlynk-io/bomtique/internal/hash"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// referenceDirectoryDigest computes the §8.4 digest from first principles.
// The caller supplies the exact set of relative paths (forward-slash,
// NFC-normalised) that SHOULD be included and their byte contents. The
// test passes only when the consumer's implementation arrives at the same
// digest as this spec-literal reference.
func referenceDirectoryDigest(t *testing.T, alg hash.Algorithm, files map[string]string) string {
	t.Helper()
	type entry struct {
		rel     string
		content string
	}
	ents := make([]entry, 0, len(files))
	for rel, content := range files {
		ents = append(ents, entry{rel, content})
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].rel < ents[j].rel })

	var buf bytes.Buffer
	for _, e := range ents {
		per := alg.New()
		_, _ = per.Write([]byte(e.content))
		buf.WriteString(hex.EncodeToString(per.Sum(nil)))
		buf.WriteString("  ")
		buf.WriteString(e.rel)
		buf.WriteByte('\n')
	}
	final := alg.New()
	_, _ = final.Write(buf.Bytes())
	return hex.EncodeToString(final.Sum(nil))
}

// writeFiles materialises a directory tree under root. The layout map's
// keys are forward-slash relative paths; a "/"-suffixed key denotes an
// empty directory to create (useful for symlink target prep).
func writeFiles(t *testing.T, root string, layout map[string]string) {
	t.Helper()
	for relPath, content := range layout {
		abs := filepath.Join(root, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
}

func TestDirectory_NestedNoFilter(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.c":       "A\n",
		"b.h":       "B\n",
		"sub/c.txt": "sub-c\n",
	})
	want := referenceDirectoryDigest(t, hash.SHA256, map[string]string{
		"a.c":       "A\n",
		"b.h":       "B\n",
		"sub/c.txt": "sub-c\n",
	})
	got, err := hash.Directory(dir, ".", hash.SHA256, nil, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if got != want {
		t.Fatalf("digest mismatch\n  got  %q\n  want %q", got, want)
	}
}

func TestDirectory_ExtensionFilterBareAndPrefixed(t *testing.T) {
	// Spec §8.3: "Each string MAY begin with `*.` or `.`, both of which
	// are stripped; comparison is case-insensitive."
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.c":       "A\n",
		"a.h":       "A-header\n",
		"b.txt":     "text\n",
		"sub/c.CPP": "cpp mixed case\n",
	})
	want := referenceDirectoryDigest(t, hash.SHA256, map[string]string{
		"a.c":       "A\n",
		"a.h":       "A-header\n",
		"sub/c.CPP": "cpp mixed case\n",
	})
	// Use all three allowed input shapes — bare, leading dot, and `*.`.
	got, err := hash.Directory(dir, ".", hash.SHA256, []string{"c", ".h", "*.cpp"}, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if got != want {
		t.Fatalf("digest mismatch\n  got  %q\n  want %q", got, want)
	}
}

func TestDirectory_SkipsHiddenDirsAndFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.c":              "A\n",
		"b.c":              "B\n",
		".dotfile.c":       "hidden-file\n",
		".git/config":      "hidden-dir content\n",
		".venv/lib/huge.c": "venv content\n",
	})
	want := referenceDirectoryDigest(t, hash.SHA256, map[string]string{
		"a.c": "A\n",
		"b.c": "B\n",
	})
	got, err := hash.Directory(dir, ".", hash.SHA256, []string{"c"}, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if got != want {
		t.Fatalf("digest mismatch\n  got  %q\n  want %q", got, want)
	}
}

func TestDirectory_SkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Windows developer mode")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.c": "A\n",
	})
	// Symlinked file and symlinked directory, both inside the walk root.
	outsideFile := filepath.Join(t.TempDir(), "exfil.c")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	outsideDir := filepath.Join(t.TempDir(), "outdir")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "poisoned.c"), []byte("bad\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, "link.c")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(dir, "linked-dir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	want := referenceDirectoryDigest(t, hash.SHA256, map[string]string{
		"a.c": "A\n",
	})
	got, err := hash.Directory(dir, ".", hash.SHA256, []string{"c"}, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if got != want {
		t.Fatalf("symlinks leaked into digest\n  got  %q\n  want %q", got, want)
	}
}

func TestDirectory_EmptyAfterFilter(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"only.txt": "nope\n",
	})
	_, err := hash.Directory(dir, ".", hash.SHA256, []string{"c"}, 0)
	if !errors.Is(err, hash.ErrEmptyDirectory) {
		t.Fatalf("expected ErrEmptyDirectory, got %v", err)
	}
}

func TestDirectory_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := hash.Directory(dir, ".", hash.SHA256, nil, 0)
	if !errors.Is(err, hash.ErrEmptyDirectory) {
		t.Fatalf("expected ErrEmptyDirectory, got %v", err)
	}
}

func TestDirectory_RegularFileFallback(t *testing.T) {
	// §8.3: path pointing at a regular file uses File form; extensions ignored.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"abc.txt": "abc",
	})
	got, err := hash.Directory(dir, "abc.txt", hash.SHA256, []string{"c"}, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	// Same known FIPS 180-4 vector for "abc".
	const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != abcSHA256 {
		t.Fatalf("fallback digest mismatch\n  got  %q\n  want %q", got, abcSHA256)
	}
}

func TestDirectory_PerFileSizeCap(t *testing.T) {
	dir := t.TempDir()
	// Two eligible files; only the oversize one should trigger failure.
	writeFiles(t, dir, map[string]string{
		"ok.c":  "small\n",
		"big.c": "",
	})
	// Overwrite big.c with > cap bytes.
	big := bytes.Repeat([]byte("X"), 200)
	if err := os.WriteFile(filepath.Join(dir, "big.c"), big, 0o600); err != nil {
		t.Fatalf("write big: %v", err)
	}
	_, err := hash.Directory(dir, ".", hash.SHA256, []string{"c"}, 50)
	if !errors.Is(err, safefs.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestDirectory_ForwardSlashRelativePaths(t *testing.T) {
	// §8.4 step 5: rel paths use `/` regardless of host OS. The reference
	// routine uses forward slashes, so matching means the implementation
	// did the filepath.ToSlash conversion.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"sub/deep/file.c": "x\n",
	})
	want := referenceDirectoryDigest(t, hash.SHA256, map[string]string{
		"sub/deep/file.c": "x\n",
	})
	got, err := hash.Directory(dir, ".", hash.SHA256, nil, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if got != want {
		t.Fatalf("slash normalisation mismatch\n  got  %q\n  want %q", got, want)
	}
}

func TestDirectory_SortedDeterministically(t *testing.T) {
	// Create files in non-alphabetical write order; digest must be stable
	// and match the byte-wise sorted reference.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"z.c": "Z\n",
		"a.c": "A\n",
		"m.c": "M\n",
	})
	want := referenceDirectoryDigest(t, hash.SHA256, map[string]string{
		"a.c": "A\n",
		"m.c": "M\n",
		"z.c": "Z\n",
	})
	got, err := hash.Directory(dir, ".", hash.SHA256, nil, 0)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if got != want {
		t.Fatalf("ordering mismatch\n  got  %q\n  want %q", got, want)
	}
}

func TestDirectory_DifferentAlgorithmsDiffer(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.c": "A\n",
	})
	gotSHA256, err := hash.Directory(dir, ".", hash.SHA256, nil, 0)
	if err != nil {
		t.Fatalf("SHA-256: %v", err)
	}
	gotSHA3, err := hash.Directory(dir, ".", hash.SHA3_256, nil, 0)
	if err != nil {
		t.Fatalf("SHA-3-256: %v", err)
	}
	if gotSHA256 == gotSHA3 {
		t.Fatalf("SHA-256 and SHA-3-256 produced identical digest — bug")
	}
}

func TestDirectory_NotRegularOrDir(t *testing.T) {
	// A named pipe / device would take extra setup; skipping a fifo across
	// platforms is awkward. This test uses a non-existent path to hit
	// ResolveRelative's sibling path and confirm error propagation.
	dir := t.TempDir()
	_, err := hash.Directory(dir, "does-not-exist", hash.SHA256, nil, 0)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}
