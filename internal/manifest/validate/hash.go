// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/hash"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// validateHashEntry enforces §8 per-entry rules:
//
//   - exactly one form — literal (algorithm + value), file (algorithm +
//     file), or path (algorithm + path, with optional extensions);
//   - algorithm is in the §8.1 allowlist;
//   - literal form value is lowercase hex of the right length;
//   - file / path targets exist, aren't symlinks, and don't escape the
//     manifest directory (delegated to safefs);
//   - directory-form paths produce at least one eligible file under the
//     §8.4 walk.
//
// Filesystem-dependent checks are skipped when Options.SkipFilesystem is
// true, which is useful for pre-flight validation before the source
// tree is populated (or for tests).
func (v *validator) validateHashEntry(h *manifest.Hash, ptr string) {
	form := classifyHashForm(h)
	if form == hashFormMixed {
		v.add(Error{
			Kind:    ErrHashForm,
			Pointer: ptr,
			Message: "hash entry must be exactly one form — literal (value), file, or path (§8)",
		})
		return
	}
	if form == hashFormNone {
		v.add(Error{
			Kind:    ErrHashForm,
			Pointer: ptr,
			Message: "hash entry missing value, file, and path — pick exactly one form (§8)",
		})
		return
	}

	if strings.TrimSpace(h.Algorithm) == "" {
		v.add(Error{
			Kind:    ErrRequiredField,
			Pointer: ptr + "/algorithm",
			Message: "hash algorithm is required",
		})
		return
	}
	alg, err := hash.Parse(h.Algorithm)
	if err != nil {
		v.add(Error{
			Kind:    ErrHashAlgorithm,
			Pointer: ptr + "/algorithm",
			Value:   h.Algorithm,
			Message: err.Error(),
		})
		return
	}

	switch form {
	case hashFormLiteral:
		if err := hash.ValidateLiteralValue(alg, *h.Value); err != nil {
			v.add(Error{
				Kind:    ErrHashValue,
				Pointer: ptr + "/value",
				Value:   *h.Value,
				Message: err.Error(),
			})
		}
	case hashFormFile:
		if !v.opts.SkipFilesystem {
			v.checkFilesystemFile(*h.File, ptr+"/file")
		}
	case hashFormPath:
		if !v.opts.SkipFilesystem {
			v.checkFilesystemPath(*h.Path, h.Extensions, alg, ptr)
		}
	}
}

// hashForm classifies which of the §8 forms a hash entry represents.
type hashForm int

const (
	hashFormNone hashForm = iota
	hashFormLiteral
	hashFormFile
	hashFormPath
	hashFormMixed
)

func classifyHashForm(h *manifest.Hash) hashForm {
	var present int
	var form hashForm
	if h.Value != nil && *h.Value != "" {
		present++
		form = hashFormLiteral
	}
	if h.File != nil && *h.File != "" {
		present++
		form = hashFormFile
	}
	if h.Path != nil && *h.Path != "" {
		present++
		form = hashFormPath
	}
	switch present {
	case 0:
		return hashFormNone
	case 1:
		return form
	}
	return hashFormMixed
}

// checkFilesystemFile verifies that the §8.2 file target exists, is a
// regular file, and sits inside the manifest directory. Size-cap is
// deferred to the hashing pass (M7) — validation only cares about
// resolvability.
func (v *validator) checkFilesystemFile(relPath, ptr string) {
	if v.manifestDir == "" {
		// No path to resolve against — in-memory manifest. Skip silently.
		return
	}
	abs, err := safefs.ResolveRelative(v.manifestDir, relPath)
	if err != nil {
		v.add(mapResolveError(err, ptr, relPath))
		return
	}
	if err := safefs.CheckNoSymlinks(v.manifestDir, abs); err != nil {
		if errors.Is(err, safefs.ErrSymlink) {
			v.add(Error{
				Kind:    ErrSymlink,
				Pointer: ptr,
				Value:   relPath,
				Message: err.Error(),
			})
			return
		}
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr,
			Value:   relPath,
			Message: err.Error(),
		})
		return
	}
	info, err := os.Lstat(abs)
	if err != nil {
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr,
			Value:   relPath,
			Message: err.Error(),
		})
		return
	}
	if !info.Mode().IsRegular() {
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr,
			Value:   relPath,
			Message: "hash file target is not a regular file (§8.2)",
		})
	}
}

// checkFilesystemPath validates §8.3 path targets: regular files fall
// through to the File-form check (ignoring extensions), and directories
// trigger the §8.4 walk to ensure the filter produces at least one file.
func (v *validator) checkFilesystemPath(relPath string, extensions []string, alg hash.Algorithm, ptr string) {
	if v.manifestDir == "" {
		return
	}
	abs, err := safefs.ResolveRelative(v.manifestDir, relPath)
	if err != nil {
		v.add(mapResolveError(err, ptr+"/path", relPath))
		return
	}
	if err := safefs.CheckNoSymlinks(v.manifestDir, abs); err != nil {
		if errors.Is(err, safefs.ErrSymlink) {
			v.add(Error{
				Kind:    ErrSymlink,
				Pointer: ptr + "/path",
				Value:   relPath,
				Message: err.Error(),
			})
			return
		}
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr + "/path",
			Value:   relPath,
			Message: err.Error(),
		})
		return
	}
	info, err := os.Lstat(abs)
	if err != nil {
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr + "/path",
			Value:   relPath,
			Message: err.Error(),
		})
		return
	}
	switch {
	case info.Mode().IsRegular():
		// §8.3 first bullet — regular file falls through to File form.
		return
	case info.IsDir():
		if err := v.validateDirectoryHasEligibleFiles(abs, relPath, extensions, alg, ptr); err != nil {
			// Already recorded inside validateDirectoryHasEligibleFiles.
			_ = err
		}
	default:
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr + "/path",
			Value:   relPath,
			Message: "hash path is neither a regular file nor a directory (§8.3)",
		})
	}
}

// validateDirectoryHasEligibleFiles runs a §8.4-conformant walk that
// stops as soon as one eligible file is found. Full hashing is deferred
// to the emit phase (M7); validation only needs the non-empty check.
func (v *validator) validateDirectoryHasEligibleFiles(absDir, relPath string, extensions []string, alg hash.Algorithm, ptr string) error {
	extFilter := lowerExtensions(extensions)

	found := false
	sentinel := errors.New("found")
	walk := func(path string, d fs.DirEntry, werr error) error {
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
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if extFilter != nil && !matchesLowerExt(name, extFilter) {
			return nil
		}
		found = true
		return sentinel
	}
	if err := filepath.WalkDir(absDir, walk); err != nil && !errors.Is(err, sentinel) {
		v.add(Error{
			Kind:    ErrHashFilesystem,
			Pointer: ptr + "/path",
			Value:   relPath,
			Message: err.Error(),
		})
		return err
	}
	if !found {
		v.add(Error{
			Kind:    ErrEmptyDirectory,
			Pointer: ptr + "/path",
			Value:   relPath,
			Message: fmt.Sprintf("directory produced no eligible files under the §8.4 walk (alg=%s)", alg.SpecName()),
		})
		return errors.New("empty")
	}
	return nil
}

func lowerExtensions(exts []string) []string {
	if len(exts) == 0 {
		return nil
	}
	out := make([]string, 0, len(exts))
	for _, e := range exts {
		e = strings.TrimPrefix(e, "*.")
		e = strings.TrimPrefix(e, ".")
		e = strings.ToLower(e)
		if e != "" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func matchesLowerExt(name string, filter []string) bool {
	lower := strings.ToLower(name)
	for _, ext := range filter {
		if strings.HasSuffix(lower, "."+ext) {
			return true
		}
	}
	return false
}

func mapResolveError(err error, ptr, value string) Error {
	switch {
	case errors.Is(err, safefs.ErrAbsolutePath):
		return Error{Kind: ErrPathTraversal, Pointer: ptr, Value: value, Message: err.Error()}
	case errors.Is(err, safefs.ErrTraversal):
		return Error{Kind: ErrPathTraversal, Pointer: ptr, Value: value, Message: err.Error()}
	case errors.Is(err, safefs.ErrNullByte):
		return Error{Kind: ErrPathTraversal, Pointer: ptr, Value: value, Message: err.Error()}
	case errors.Is(err, safefs.ErrEmptyPath):
		return Error{Kind: ErrRequiredField, Pointer: ptr, Value: value, Message: err.Error()}
	}
	return Error{Kind: ErrHashFilesystem, Pointer: ptr, Value: value, Message: err.Error()}
}
