// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// ErrPrimaryNotFound indicates that no primary manifest was found when
// walking up from the starting directory.
var ErrPrimaryNotFound = errors.New("primary manifest not found (run `bomtique manifest init` first)")

// locateExcludedDirs mirrors cmd/bomtique/discovery.go. Keeping the
// list in sync is a manual discipline documented in TASKS.md M14.0.
var locateExcludedDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	".venv":        {},
	"testdata":     {},
}

// conventionalPrimaryName is the filename discovery recognises as a
// primary manifest (cmd/bomtique/discovery.go).
const conventionalPrimaryName = ".primary.json"

// conventionalComponentNames are the filenames discovery recognises as
// components manifests. Order matters: when multiple candidates exist
// in the same directory, JSON wins over CSV for mutation targets
// because JSON can carry every field.
var conventionalComponentNames = []string{
	".components.json",
	".components.csv",
}

// LocatePrimary walks up from fromDir looking for a primary manifest.
// It returns the path to the first matching file found, or
// ErrPrimaryNotFound. Directories are scanned in the same order as
// cmd/bomtique/discovery: .primary.json is checked before any other
// file in the same directory, and every excluded directory name from
// the discovery walk is honoured here too.
//
// The function does not open or parse every JSON file it encounters;
// it only inspects files whose basename matches conventionalPrimaryName.
// Producers following the §16.2 naming convention are the only supported
// shape. Projects using custom filenames pass the path explicitly
// through the caller's flag surface and do not reach this function.
func LocatePrimary(fromDir string) (string, error) {
	abs, err := absClean(fromDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(abs, conventionalPrimaryName)
		if ok, err := isPrimaryManifest(candidate); err != nil {
			return "", err
		} else if ok {
			return candidate, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", ErrPrimaryNotFound
		}
		if shouldStopAt(parent) {
			return "", ErrPrimaryNotFound
		}
		abs = parent
	}
}

// LocateOrCreateComponents resolves the target components manifest for
// a mutation. Resolution order:
//
//  1. If flagInto is non-empty, use it verbatim. Relative paths are
//     resolved against fromDir. `created` is true when the file does
//     not yet exist.
//  2. Otherwise, walk up from fromDir looking for a
//     .components.json / .components.csv sibling. The first hit wins.
//  3. Otherwise, locate the nearest primary manifest and return
//     "<primaryDir>/.components.json" with created=true so the caller
//     creates it on first write.
//  4. If no primary is found either, return ErrPrimaryNotFound — the
//     user must run `bomtique manifest init` before adding pool
//     components.
func LocateOrCreateComponents(fromDir, flagInto string) (path string, created bool, err error) {
	abs, err := absClean(fromDir)
	if err != nil {
		return "", false, err
	}
	if flagInto != "" {
		target := flagInto
		if !filepath.IsAbs(target) {
			target = filepath.Join(abs, target)
		}
		target = filepath.Clean(target)
		switch _, serr := os.Stat(target); {
		case serr == nil:
			return target, false, nil
		case errors.Is(serr, fs.ErrNotExist):
			return target, true, nil
		default:
			return "", false, fmt.Errorf("stat %s: %w", target, serr)
		}
	}

	// Walk up looking for an existing components manifest.
	cur := abs
	for {
		for _, name := range conventionalComponentNames {
			candidate := filepath.Join(cur, name)
			if ok, err := isComponentsManifest(candidate); err != nil {
				return "", false, err
			} else if ok {
				return candidate, false, nil
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur || shouldStopAt(parent) {
			break
		}
		cur = parent
	}

	// No components manifest found; fall back to "alongside the primary".
	primary, err := LocatePrimary(abs)
	if err != nil {
		return "", false, err
	}
	return filepath.Join(filepath.Dir(primary), ".components.json"), true, nil
}

// shouldStopAt returns true when a directory should terminate an
// upward walk. It matches the cmd/bomtique/discovery exclusion set
// applied to directory names; crossing an excluded boundary would
// normally be the wrong move for mutation commands (we don't want a
// stray `.primary.json` inside `.git/` or `testdata/` to win).
func shouldStopAt(dir string) bool {
	name := filepath.Base(dir)
	if strings.HasPrefix(name, ".") && name != "." && name != ".." {
		return true
	}
	if _, skip := locateExcludedDirs[name]; skip {
		return true
	}
	return false
}

// isPrimaryManifest returns true when path exists, is a regular file,
// and parses with schema marker primary-manifest/v1. Errors other than
// "does not exist" / "not a manifest" are propagated.
func isPrimaryManifest(path string) (bool, error) {
	return isMarkerFile(path, manifest.KindPrimary)
}

// isComponentsManifest is the components-manifest counterpart.
func isComponentsManifest(path string) (bool, error) {
	return isMarkerFile(path, manifest.KindComponents)
}

func isMarkerFile(path string, want manifest.Kind) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	// Symlinks are skipped — §18.2 applies at the discovery layer too.
	if info.Mode()&fs.ModeSymlink != 0 {
		return false, nil
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	var m *manifest.Manifest
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		m, err = manifest.ParseCSV(data, path)
	default:
		m, err = manifest.ParseJSON(data, path)
	}
	if err != nil {
		// A file sitting at the conventional path that fails to parse
		// is a hard error — the user almost certainly means this to be
		// their manifest, and silently walking past it would hide the
		// bug. Surface it.
		if errors.Is(err, manifest.ErrNoSchemaMarker) {
			return false, nil
		}
		return false, err
	}
	return m.Kind == want, nil
}

func absClean(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	return filepath.Clean(abs), nil
}
