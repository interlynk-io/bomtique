// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package hash

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

// ErrEmptyDirectory signals that the directory-digest walk ended with zero
// eligible files after hidden / symlink / extension filtering (§8.4 step 3).
var ErrEmptyDirectory = errors.New("directory digest produced no eligible files")

// ErrNotRegularOrDir signals that `hashes[].path` resolved to neither a
// regular file nor a directory. Only those two forms are valid targets
// for §8.3.
var ErrNotRegularOrDir = errors.New("hash path is neither a regular file nor a directory")

// Directory computes the §8.3 / §8.4 hash:
//
//   - If relPath resolves to a regular file, falls back to File form
//     (§8.3 first bullet). The `extensions` filter is ignored in that case.
//   - If relPath resolves to a directory, runs the §8.4 deterministic
//     walk: skip subdirs starting with '.', skip hidden files, skip
//     symbolic links at every layer, apply case-insensitive extension
//     filter (leading '*.' / '.' stripped), hash each remaining file with
//     the declared algorithm, build the sorted manifest string of
//     `<hex><SP><SP><rel-path><LF>` lines, hash that manifest, return the
//     resulting lowercase hex digest.
//
// A zero or negative maxSize uses safefs.DefaultMaxFileSize (10 MiB); the
// cap applies per regular file encountered during the walk.
func Directory(manifestDir, relPath string, alg Algorithm, extensions []string, maxSize int64) (string, error) {
	if maxSize <= 0 {
		maxSize = safefs.DefaultMaxFileSize
	}

	absPath, err := safefs.ResolveRelative(manifestDir, relPath)
	if err != nil {
		return "", err
	}
	if err := safefs.CheckNoSymlinks(manifestDir, absPath); err != nil {
		return "", err
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		return "", err
	}
	switch {
	case info.Mode().IsRegular():
		return File(manifestDir, relPath, alg, maxSize)
	case info.IsDir():
		return directoryDigest(absPath, alg, extensions, maxSize)
	default:
		return "", fmt.Errorf("%w: %q", ErrNotRegularOrDir, relPath)
	}
}

type fileEntry struct {
	rel    string // forward-slash, NFC
	digest string // lowercase hex
}

func directoryDigest(absDir string, alg Algorithm, extensions []string, maxSize int64) (string, error) {
	extFilter := normalizeExtensions(extensions)

	var entries []fileEntry

	walkFn := func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if path == absDir {
			return nil
		}
		name := d.Name()

		if d.IsDir() {
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		// Symlinks (file or dir) are skipped unconditionally (§8.4).
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if extFilter != nil && !matchesExt(name, extFilter) {
			return nil
		}

		digest, err := hashFileAtAbs(path, alg, maxSize)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}
		entries = append(entries, fileEntry{
			rel:    safefs.ToNFC(filepath.ToSlash(rel)),
			digest: digest,
		})
		return nil
	}

	if err := filepath.WalkDir(absDir, walkFn); err != nil {
		return "", err
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("%w: %s", ErrEmptyDirectory, absDir)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteString(e.digest)
		buf.WriteString("  ")
		buf.WriteString(e.rel)
		buf.WriteByte('\n')
	}

	h := alg.New()
	_, _ = h.Write(buf.Bytes()) // hash.Hash.Write is documented never to fail
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashFileAtAbs opens a file that the caller has already verified sits
// under a safely-walked directory, applies the per-read size cap, and
// returns the lowercase hex digest of its bytes. The size cap is enforced
// by reading maxSize+1 bytes and failing if the underlying file still had
// anything to yield.
func hashFileAtAbs(abs string, alg Algorithm, maxSize int64) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := alg.New()
	n, err := io.Copy(h, io.LimitReader(f, maxSize+1))
	if err != nil {
		return "", err
	}
	if n > maxSize {
		return "", fmt.Errorf("%s: %w (limit %d bytes)", abs, safefs.ErrFileTooLarge, maxSize)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// normalizeExtensions strips the optional `*.` / `.` prefix, NFC-
// normalises (§4.6), lowercases (§8.3 case-insensitive), and deduplicates
// in order. Empty entries are dropped.
func normalizeExtensions(exts []string) []string {
	if len(exts) == 0 {
		return nil
	}
	out := make([]string, 0, len(exts))
	seen := make(map[string]struct{}, len(exts))
	for _, raw := range exts {
		e := raw
		e = strings.TrimPrefix(e, "*.")
		e = strings.TrimPrefix(e, ".")
		e = safefs.ToNFC(e)
		e = strings.ToLower(e)
		if e == "" {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// matchesExt reports whether basename (already just a leaf name) ends with
// `.<ext>` for any ext in filter, after NFC + lowercase normalisation on
// the basename side.
func matchesExt(basename string, filter []string) bool {
	normalized := strings.ToLower(safefs.ToNFC(basename))
	for _, ext := range filter {
		if strings.HasSuffix(normalized, "."+ext) {
			return true
		}
	}
	return false
}
