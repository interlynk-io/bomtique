// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoNetworkImportsOutsideRegfetch enforces the consumer-path
// network invariant from TASKS.md M14.7: only cmd/bomtique and
// internal/regfetch may import net/http or call net.Dial. `scan`,
// `validate`, and the emitters must stay socket-free.
//
// The check is a lexical import scan — good enough to catch
// accidental regressions; a malicious contributor could bypass it,
// but that's out of scope.
func TestNoNetworkImportsOutsideRegfetch(t *testing.T) {
	repoRoot := findRepoRoot(t)
	allowedPrefixes := []string{
		filepath.Join(repoRoot, "cmd", "bomtique") + string(os.PathSeparator),
		filepath.Join(repoRoot, "internal", "regfetch") + string(os.PathSeparator),
	}
	// Tests are allowed anywhere — they may need httptest locally.
	// The enforcement applies to production .go files only.
	forbiddenRE := regexp.MustCompile(`\bnet/http\b|\bnet\.Dial\b|\bnet\.DefaultResolver\b`)

	type violation struct {
		path string
		line int
		text string
	}
	var violations []violation

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			name := d.Name()
			if path != repoRoot && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "schemas") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(path, prefix) {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if forbiddenRE.MatchString(line) {
				// Ignore comment-only lines so the spec / doc
				// prose can mention `net/http` in passing.
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				violations = append(violations, violation{
					path: path,
					line: i + 1,
					text: strings.TrimSpace(line),
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", repoRoot, err)
	}

	if len(violations) > 0 {
		for _, v := range violations {
			t.Errorf("network import leaked into consumer path: %s:%d %s", v.path, v.line, v.text)
		}
		t.Fatalf("network isolation broken: %d violation(s) — only cmd/bomtique and internal/regfetch may import net/http", len(violations))
	}
}

// findRepoRoot walks up from the test binary's working directory
// until it finds a go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from test CWD")
		}
		dir = parent
	}
}
