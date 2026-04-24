// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	conformanceRoot  = "testdata/conformance"
	goldenSDE        = "1700000000"
	goldenDirName    = "golden"
	expectErrorsFile = "expect-errors.txt"
)

// TestConformance_Positive drives every fixture under
// testdata/conformance/positive/. For each, `bomtique generate` runs
// against the fixture directory with a fixed SOURCE_DATE_EPOCH, and
// each produced .cdx.json is byte-compared to the golden sibling under
// <fixture>/golden/.
//
// New fixtures: drop a directory in, run this test once with
// BOMTIQUE_REGENERATE_GOLDEN=1 to populate golden/, then lock it in.
func TestConformance_Positive(t *testing.T) {
	root := filepath.Join(conformanceRoot, "positive")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read positive dir: %v", err)
	}
	regen := os.Getenv("BOMTIQUE_REGENERATE_GOLDEN") == "1"
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fixture := e.Name()
		t.Run(fixture, func(t *testing.T) {
			runPositiveFixture(t, filepath.Join(root, fixture), regen)
		})
	}
}

func runPositiveFixture(t *testing.T, fixtureDir string, regen bool) {
	t.Helper()

	outDir := t.TempDir()
	_, _, err := withArgs(t,
		"generate", fixtureDir,
		"--out", outDir,
		"--source-date-epoch", goldenSDE,
	)
	if code := exitCodeOf(err); code != exitOK {
		t.Fatalf("generate exit %d: %v", code, err)
	}

	goldenDir := filepath.Join(fixtureDir, goldenDirName)
	if regen {
		if err := os.RemoveAll(goldenDir); err != nil {
			t.Fatalf("clean golden: %v", err)
		}
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := copyTree(outDir, goldenDir); err != nil {
			t.Fatalf("copy golden: %v", err)
		}
		t.Logf("regenerated golden under %s", goldenDir)
		return
	}

	diffTrees(t, goldenDir, outDir)
}

// TestConformance_Determinism replays every positive fixture twice with
// the same fixed SOURCE_DATE_EPOCH and asserts the per-file outputs are
// byte-identical across runs — a catch-all for any determinism
// regression that slips past M8's focused tests.
func TestConformance_Determinism(t *testing.T) {
	root := filepath.Join(conformanceRoot, "positive")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read positive dir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fixture := e.Name()
		t.Run(fixture, func(t *testing.T) {
			runDeterminismFixture(t, filepath.Join(root, fixture))
		})
	}
}

func runDeterminismFixture(t *testing.T, fixtureDir string) {
	t.Helper()
	runA := t.TempDir()
	runB := t.TempDir()
	for i, outDir := range []string{runA, runB} {
		_, _, err := withArgs(t,
			"generate", fixtureDir,
			"--out", outDir,
			"--source-date-epoch", goldenSDE,
		)
		if code := exitCodeOf(err); code != exitOK {
			t.Fatalf("generate run %d exit %d: %v", i+1, code, err)
		}
	}
	diffTrees(t, runA, runB)
}

// TestConformance_Negative drives every fixture under
// testdata/conformance/negative/. Each fixture carries an
// expect-errors.txt file listing substrings that MUST appear in
// validate's stderr; the test also asserts a non-zero exit code.
func TestConformance_Negative(t *testing.T) {
	root := filepath.Join(conformanceRoot, "negative")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read negative dir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fixture := e.Name()
		t.Run(fixture, func(t *testing.T) {
			runNegativeFixture(t, filepath.Join(root, fixture))
		})
	}
}

func runNegativeFixture(t *testing.T, fixtureDir string) {
	t.Helper()

	expected, err := readExpectErrors(filepath.Join(fixtureDir, expectErrorsFile))
	if err != nil {
		t.Fatalf("read expect-errors.txt: %v", err)
	}

	_, stderr, err := withArgs(t, "validate", fixtureDir)
	if code := exitCodeOf(err); code == exitOK {
		t.Fatalf("expected non-zero exit, got 0; stderr=%s", stderr.String())
	}

	combined := stderr.String()
	for _, sub := range expected {
		if !strings.Contains(combined, sub) {
			t.Fatalf("missing expected substring %q\nstderr:\n%s", sub, combined)
		}
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// readExpectErrors parses one substring-per-line from a fixture's
// expect-errors.txt. Blank lines and `#`-prefix comments are skipped.
func readExpectErrors(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

// diffTrees walks `want` and asserts every file in it has a byte-equal
// counterpart in `got`. Extra files in `got` that aren't in `want`
// fail too. Directories match structurally.
func diffTrees(t *testing.T, want, got string) {
	t.Helper()

	wantFiles := gatherFiles(t, want)
	gotFiles := gatherFiles(t, got)

	for rel, wantBytes := range wantFiles {
		gotBytes, ok := gotFiles[rel]
		if !ok {
			t.Errorf("output missing file %s", rel)
			continue
		}
		if !bytes.Equal(wantBytes, gotBytes) {
			t.Errorf("%s: byte mismatch\nwant: %s\n got: %s", rel, wantBytes, gotBytes)
		}
	}
	for rel := range gotFiles {
		if _, ok := wantFiles[rel]; !ok {
			t.Errorf("unexpected extra output file %s", rel)
		}
	}
}

func gatherFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
