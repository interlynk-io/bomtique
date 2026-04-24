// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrAbsolutePath signals that a manifest relative path was actually an
// absolute path — POSIX (`/…`), Windows drive-letter (`C:\…` or `C:/…`),
// or Windows UNC (`\\server\share\…` / `//server/share/…`). Per §4.3 a
// consumer MUST reject any such path regardless of the platform the
// consumer is running on — manifests are portable.
var ErrAbsolutePath = errors.New("path is absolute")

// ErrTraversal signals that a relative path, after joining to the manifest
// directory and lexical cleaning, escapes the manifest directory via `..`
// (§4.3). There is no opt-in toggle to permit this.
var ErrTraversal = errors.New("path escapes manifest directory")

// ErrEmptyPath signals an empty string where a relative path was required.
var ErrEmptyPath = errors.New("empty path")

// ErrNullByte signals that a path contains NUL (0x00), which some OS
// interfaces treat as a terminator and therefore as a confusion vector.
var ErrNullByte = errors.New("path contains NUL byte")

// ResolveRelative joins relPath onto manifestDir (both NFC-normalized per
// §4.6) and returns the cleaned absolute-form path, rejecting any input
// that §4.3 forbids:
//
//   - empty string;
//   - NUL byte in the path;
//   - POSIX absolute (`/…`);
//   - Windows UNC (`\\…` or `//…`);
//   - Windows drive-letter (`X:\…`, `X:/…`, or the bare drive-relative `X:foo`);
//   - post-join `..` escape of manifestDir.
//
// The returned path is a purely lexical result. Symlink and regular-file
// checks live in Open / CheckNoSymlinks (§18.2).
func ResolveRelative(manifestDir, relPath string) (string, error) {
	if relPath == "" {
		return "", ErrEmptyPath
	}
	if strings.ContainsRune(relPath, 0) || strings.ContainsRune(manifestDir, 0) {
		return "", ErrNullByte
	}

	relPath = ToNFC(relPath)
	manifestDir = ToNFC(manifestDir)

	if err := rejectAbsolute(relPath); err != nil {
		return "", err
	}

	joined := filepath.Clean(filepath.Join(manifestDir, relPath))
	base := filepath.Clean(manifestDir)

	if !isWithin(base, joined) {
		return "", fmt.Errorf("%w: %q resolves outside %q", ErrTraversal, relPath, manifestDir)
	}
	return joined, nil
}

// rejectAbsolute returns ErrAbsolutePath if relPath carries any of the
// forms §4.3 forbids. The check is deliberately OS-independent: a
// manifest produced on Linux must be rejected on Windows for carrying a
// `/etc/passwd` path, and vice versa.
func rejectAbsolute(relPath string) error {
	if strings.HasPrefix(relPath, "/") {
		return fmt.Errorf("%w: %q (POSIX absolute)", ErrAbsolutePath, relPath)
	}
	if strings.HasPrefix(relPath, `\\`) || strings.HasPrefix(relPath, "//") {
		return fmt.Errorf("%w: %q (UNC)", ErrAbsolutePath, relPath)
	}
	// A single leading backslash on a Windows-style path (e.g. `\foo`)
	// is the Windows "root of current drive" form — also absolute.
	if strings.HasPrefix(relPath, `\`) {
		return fmt.Errorf("%w: %q (rooted)", ErrAbsolutePath, relPath)
	}
	if len(relPath) >= 2 && relPath[1] == ':' {
		c := relPath[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return fmt.Errorf("%w: %q (Windows drive-letter)", ErrAbsolutePath, relPath)
		}
	}
	return nil
}

// isWithin reports whether child lies within parent (equal, or a
// descendant). Both arguments must already be lexically cleaned.
func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	return true
}
