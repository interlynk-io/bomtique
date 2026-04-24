// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// discoveryFilenames are the bomtique-conventional manifest basenames.
// Spec §12.5 leaves discovery implementation-defined; we match exactly
// these three names (all leading with `.` so they act as hidden files
// in the producer's source tree).
var discoveryFilenames = map[string]struct{}{
	".primary.json":    {},
	".components.json": {},
	".components.csv":  {},
}

// discoveryExcludedDirs are directories whose names we always skip
// during a walk. These are the common high-volume build directories
// where user manifests never live, plus `testdata` — which the Go
// toolchain convention reserves for test fixtures that are
// intentionally excluded from builds. A negative conformance fixture
// (e.g. `primary-manifest/v2`) is legitimate test content that must
// not poison a dev-loop `bomtique validate` run.
var discoveryExcludedDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	".venv":        {},
	"testdata":     {},
}

// discover walks `root` and returns every file whose basename matches
// the bomtique discovery conventions. The traversal:
//
//   - Skips every directory whose name starts with `.` (matches the
//     standard hidden-dir convention; covers `.git`, `.venv`, `.vscode`,
//     etc.) plus the named-excluded set above.
//   - Skips symbolic links encountered as directories (filepath.WalkDir
//     refuses symlink-follow by default).
//   - Visits each directory's entries in lexicographic order
//     (filepath.WalkDir delegates to os.ReadDir which sorts).
//
// The returned slice preserves that deterministic order — two runs
// against the same tree yield the same sequence of paths.
func discover(root string) ([]string, error) {
	var found []string
	walkFn := func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if _, skip := discoveryExcludedDirs[name]; skip {
				return fs.SkipDir
			}
			return nil
		}
		// Skip symlinked files too, even if they point at something
		// matching the discovery filenames — §18.2's symlink refusal
		// is a runtime invariant, not just an emit-time one.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if _, ok := discoveryFilenames[d.Name()]; ok {
			found = append(found, path)
		}
		return nil
	}
	if err := filepath.WalkDir(root, walkFn); err != nil {
		return nil, err
	}
	return found, nil
}
