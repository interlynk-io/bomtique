// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxFileSize is the RECOMMENDED per-read cap from §8 — 10 MiB.
// M9's CLI exposes `--max-file-size` to override.
const DefaultMaxFileSize int64 = 10 * 1024 * 1024

// ErrSymlink signals that one of the path components, or the target
// itself, is a symbolic link. §18.2 requires the consumer to refuse such
// reads; the default is not opt-outable from inside this package.
var ErrSymlink = errors.New("path component is a symbolic link")

// ErrNotRegular signals that a path referenced where a regular file was
// expected (e.g. hash `file`, license `file`) resolves to a directory,
// device, or socket.
var ErrNotRegular = errors.New("path is not a regular file")

// ErrFileTooLarge signals that a read overran the configured size cap.
// The manifest is rejected rather than silently truncated (§8).
var ErrFileTooLarge = errors.New("file exceeds maximum size")

// CheckNoSymlinks walks each path component of resolvedAbs starting from
// manifestDir and fails with ErrSymlink if any segment — directory or
// final leaf — is a symbolic link (§18.2). manifestDir itself is treated
// as trusted starting ground and not checked; the caller is expected to
// pass a directory it has already decided to read manifests from.
func CheckNoSymlinks(manifestDir, resolvedAbs string) error {
	base := filepath.Clean(manifestDir)
	target := filepath.Clean(resolvedAbs)

	rel, err := filepath.Rel(base, target)
	if err != nil {
		return fmt.Errorf("safefs: cannot relativize %q against %q: %w", target, base, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		// Defensive: ResolveRelative should have caught this.
		return fmt.Errorf("%w: %q", ErrTraversal, resolvedAbs)
	}

	cur := base
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q", ErrSymlink, cur)
		}
	}
	return nil
}

// Open resolves relPath against manifestDir (§4.3), verifies no component
// along the way is a symlink (§18.2), confirms the target is a regular
// file, and returns a ReadCloser that enforces the maxSize cap (§8).
//
// Reading more than maxSize bytes from the returned reader yields
// ErrFileTooLarge rather than silent truncation. A zero or negative
// maxSize is treated as DefaultMaxFileSize.
func Open(manifestDir, relPath string, maxSize int64) (io.ReadCloser, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxFileSize
	}
	abs, err := ResolveRelative(manifestDir, relPath)
	if err != nil {
		return nil, err
	}
	if err := CheckNoSymlinks(manifestDir, abs); err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q", ErrNotRegular, relPath)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	return &cappedFile{f: f, remain: maxSize, path: abs}, nil
}

// ReadFile is a convenience that opens relPath under the size cap and
// returns the entire content. Callers that need streaming should use
// Open directly.
func ReadFile(manifestDir, relPath string, maxSize int64) ([]byte, error) {
	rc, err := Open(manifestDir, relPath, maxSize)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// cappedFile wraps *os.File and enforces a byte budget on its Read path.
// The first Read that would exceed the budget returns ErrFileTooLarge;
// the underlying file is left open so Close is still meaningful.
type cappedFile struct {
	f      *os.File
	remain int64
	path   string
}

func (c *cappedFile) Read(p []byte) (int, error) {
	if c.remain <= 0 {
		// Probe for leftover bytes; if any, the file is oversized.
		var b [1]byte
		n, err := c.f.Read(b[:])
		if n > 0 {
			return 0, fmt.Errorf("%s: %w", c.path, ErrFileTooLarge)
		}
		if errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.remain {
		p = p[:c.remain]
	}
	n, err := c.f.Read(p)
	c.remain -= int64(n)
	return n, err
}

func (c *cappedFile) Close() error { return c.f.Close() }
