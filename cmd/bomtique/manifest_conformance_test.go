// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestManifestConformance_Positive walks every fixture directory
// under testdata/manifest, applies the recorded command against a
// fresh tmpdir seeded from initial/, and asserts the resulting tree
// byte-matches golden/.
//
// A companion determinism pass reruns each fixture against a second
// tmpdir and asserts the two result trees are byte-equal.
//
// Regenerate goldens with BOMTIQUE_REGENERATE_MANIFEST_GOLDEN=1.
func TestManifestConformance_Positive(t *testing.T) {
	root := filepath.Join("testdata", "manifest")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no testdata/manifest fixtures present")
		}
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			runManifestFixture(t, filepath.Join(root, name))
		})
	}
}

// TestManifestConformance_Determinism reruns every fixture in a
// fresh tmpdir and asserts the second run's output tree is byte-
// identical to the first. Mutation writes go through the same
// `mutate.Write*` engine as the production code, so this is a cheap
// belt-and-braces check against accidental nondeterminism.
func TestManifestConformance_Determinism(t *testing.T) {
	root := filepath.Join("testdata", "manifest")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no testdata/manifest fixtures present")
		}
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			first := t.TempDir()
			second := t.TempDir()
			seedInitial(t, filepath.Join(root, name, "initial"), first)
			seedInitial(t, filepath.Join(root, name, "initial"), second)
			args := readFixtureCmd(t, filepath.Join(root, name, "cmd.txt"))

			runFixtureCommand(t, args, first)
			runFixtureCommand(t, args, second)

			firstTree := captureTree(t, first)
			secondTree := captureTree(t, second)
			if !reflect.DeepEqual(firstTree, secondTree) {
				t.Fatalf("determinism drift between runs\nfirst: %v\nsecond: %v", firstTree, secondTree)
			}
		})
	}
}

// runManifestFixture is the core per-fixture assertion. It seeds a
// tmpdir, runs the recorded command, and compares the resulting
// tree against golden/ byte-for-byte.
func runManifestFixture(t *testing.T, fixtureDir string) {
	t.Helper()
	tmp := t.TempDir()
	seedInitial(t, filepath.Join(fixtureDir, "initial"), tmp)

	args := readFixtureCmd(t, filepath.Join(fixtureDir, "cmd.txt"))
	runFixtureCommand(t, args, tmp)

	goldenDir := filepath.Join(fixtureDir, "golden")
	if os.Getenv("BOMTIQUE_REGENERATE_MANIFEST_GOLDEN") == "1" {
		regenerateGolden(t, tmp, goldenDir)
		return
	}
	assertTreeMatchesGolden(t, tmp, goldenDir)
}

// seedInitial copies src recursively into dst. Missing src is a
// no-op so fixtures that start empty (init-basic) can omit the
// initial/ directory entirely.
func seedInitial(t *testing.T, src, dst string) {
	t.Helper()
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("stat %s: %v", src, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", src)
	}
	walk := func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}
	if err := filepath.WalkDir(src, walk); err != nil {
		t.Fatalf("seed from %s: %v", src, err)
	}
}

// readFixtureCmd reads cmd.txt. Format: one arg per line, blank
// lines and lines starting with `#` ignored. The harness prepends
// `-C <tmpdir>` to the first subcommand's argument list.
func readFixtureCmd(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no command supplied", path)
	}
	return out
}

// runFixtureCommand runs the recorded command against the seeded
// tmpdir. `-C <tmpdir>` is appended after the subcommand so it
// takes precedence over any `-C` the author accidentally wrote.
func runFixtureCommand(t *testing.T, args []string, tmp string) {
	t.Helper()
	if len(args) < 2 {
		t.Fatalf("command too short: %v", args)
	}
	// `manifest <sub>` lives at args[0..=1]; insert `-C tmp` after
	// the subcommand so Cobra's flag parser picks it up.
	full := append([]string(nil), args[:2]...)
	full = append(full, "-C", tmp)
	full = append(full, args[2:]...)

	_, stderr, err := withArgs(t, full...)
	if code := exitCodeOf(err); code != exitOK {
		t.Fatalf("fixture command %v exited %d\nstderr:\n%s", full, code, stderr.String())
	}
}

// captureTree walks `root` and returns a map keyed by forward-slash
// relative path → byte contents. Used by the determinism check and
// by the golden comparison.
func captureTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func assertTreeMatchesGolden(t *testing.T, produced, goldenDir string) {
	t.Helper()
	golden := captureTree(t, goldenDir)
	got := captureTree(t, produced)

	for rel, want := range golden {
		have, ok := got[rel]
		if !ok {
			t.Errorf("missing output file: %s", rel)
			continue
		}
		if string(have) != string(want) {
			t.Errorf("file %s differs:\n--- want ---\n%s\n--- got ---\n%s", rel, want, have)
		}
	}
	for rel := range got {
		if _, ok := golden[rel]; !ok {
			t.Errorf("unexpected output file: %s", rel)
		}
	}
}

func regenerateGolden(t *testing.T, produced, goldenDir string) {
	t.Helper()
	if err := os.RemoveAll(goldenDir); err != nil {
		t.Fatalf("clear golden: %v", err)
	}
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatalf("mkdir golden: %v", err)
	}
	tree := captureTree(t, produced)
	for rel, data := range tree {
		target := filepath.Join(goldenDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", target, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}
	_ = fmt.Sprintf // keep import if unused
}
