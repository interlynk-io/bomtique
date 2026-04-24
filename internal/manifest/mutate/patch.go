// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// ResolvesSpec is the input shape for a single `--resolves …` flag.
// Name is required when the spec is emitted to the manifest; the other
// fields are optional.
type ResolvesSpec struct {
	Type        string // "security" | "defect" | "enhancement"
	Name        string
	ID          string
	URL         string
	Description string
}

// PatchOptions configures a `bomtique manifest patch` call.
type PatchOptions struct {
	FromDir string
	Into    string
	Ref     string
	DryRun  bool

	// DiffPath is the relative path to the on-disk diff file. Resolved
	// against the containing components manifest's directory per §4.3.
	// Bomtique does NOT read the diff content; scan reads it later via
	// safefs.
	DiffPath string

	// Type is the §7.4 patch type: unofficial | monkey | backport | cherry-pick.
	Type string

	Resolves []ResolvesSpec

	// Notes, when non-empty, is merged into pedigree.notes. By default
	// appends with a "\n\n" separator if notes already exist;
	// ReplaceNotes=true overwrites.
	Notes        string
	ReplaceNotes bool
}

// PatchResult summarises what Patch did.
type PatchResult struct {
	DryRun        bool
	Path          string
	Ref           string
	ComponentName string
	PatchType     string
	ResolvesCount int
	NotesReplaced bool
	NotesAppended bool
}

var validPatchTypes = map[string]struct{}{
	"unofficial":  {},
	"monkey":      {},
	"backport":    {},
	"cherry-pick": {},
}

var validResolvesTypes = map[string]struct{}{
	"security":    {},
	"defect":      {},
	"enhancement": {},
}

// ErrPatchInvalidType is returned for an invalid --type value.
var ErrPatchInvalidType = errors.New("invalid patch type (allowed: unofficial, monkey, backport, cherry-pick)")

// ErrPatchInvalidResolvesType is returned for an invalid resolves
// entry type.
var ErrPatchInvalidResolvesType = errors.New("invalid resolves type (allowed: security, defect, enhancement)")

// Patch appends a Patch entry to the target component's
// pedigree.patches and optionally merges pedigree.notes.
func Patch(opts PatchOptions) (*PatchResult, error) {
	ref, err := graph.ParseRef(strings.TrimSpace(opts.Ref))
	if err != nil {
		return nil, fmt.Errorf("parse ref %q: %w", opts.Ref, err)
	}

	patchType := strings.TrimSpace(opts.Type)
	if _, ok := validPatchTypes[patchType]; !ok {
		return nil, fmt.Errorf("%w: got %q", ErrPatchInvalidType, patchType)
	}

	diffPath := strings.TrimSpace(opts.DiffPath)
	if diffPath == "" {
		return nil, errors.New("--diff-path is required")
	}

	for i, r := range opts.Resolves {
		if r.Type != "" {
			if _, ok := validResolvesTypes[r.Type]; !ok {
				return nil, fmt.Errorf("%w: resolves[%d].type=%q", ErrPatchInvalidResolvesType, i, r.Type)
			}
		}
		if strings.TrimSpace(r.Name) == "" && strings.TrimSpace(r.ID) == "" {
			return nil, fmt.Errorf("--resolves entry %d needs at least name=… or id=…", i)
		}
	}

	fromDir := opts.FromDir
	if fromDir == "" {
		fromDir = "."
	}
	primaryPath, err := LocatePrimary(fromDir)
	if err != nil {
		return nil, err
	}

	poolPaths, err := discoverComponentsManifests(filepath.Dir(primaryPath))
	if err != nil {
		return nil, err
	}

	// Locate the target component.
	type hit struct {
		path   string
		parsed *manifest.Manifest
		index  int
		name   string
	}
	cache := map[string]*manifest.Manifest{}

	var targetPath string
	if opts.Into != "" {
		targetPath, err = canonicalise(opts.Into, fromDir)
		if err != nil {
			return nil, err
		}
	}

	var hits []hit
	for _, p := range poolPaths {
		m, err := parseComponentsManifestCached(cache, p)
		if err != nil {
			return nil, err
		}
		for i := range m.Components.Components {
			c := &m.Components.Components[i]
			match, err := componentMatchesRef(c, ref)
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
			if targetPath != "" && p != targetPath {
				continue
			}
			hits = append(hits, hit{path: p, parsed: m, index: i, name: c.Name})
			break
		}
	}

	if len(hits) == 0 {
		return nil, fmt.Errorf("%w (ref %q)", ErrUpdateNotFound, ref.Raw)
	}
	if len(hits) > 1 {
		paths := make([]string, len(hits))
		for i, h := range hits {
			paths[i] = h.path
		}
		return nil, &ErrRemoveMultiMatch{Ref: ref.Raw, Hits: paths}
	}

	chosen := hits[0]

	// §4.3: the diff path is relative to the components-manifest
	// directory. Reject absolute / traversal / UNC / drive-letter.
	manifestDir := filepath.Dir(chosen.path)
	if _, err := safefs.ResolveRelative(manifestDir, diffPath); err != nil {
		return nil, fmt.Errorf("diff path %q: %w", diffPath, err)
	}

	c := &chosen.parsed.Components.Components[chosen.index]
	original := *c
	if c.Pedigree == nil {
		c.Pedigree = &manifest.Pedigree{}
	}

	// Build the Patch entry.
	p := manifest.Patch{
		Type: patchType,
		Diff: &manifest.Diff{URL: &diffPath},
	}
	if len(opts.Resolves) > 0 {
		p.Resolves = make([]manifest.Resolves, 0, len(opts.Resolves))
		for _, r := range opts.Resolves {
			entry := manifest.Resolves{}
			if t := strings.TrimSpace(r.Type); t != "" {
				entry.Type = &t
			}
			if n := strings.TrimSpace(r.Name); n != "" {
				entry.Name = &n
			}
			if id := strings.TrimSpace(r.ID); id != "" {
				entry.ID = &id
			}
			if u := strings.TrimSpace(r.URL); u != "" {
				entry.URL = &u
			}
			if d := strings.TrimSpace(r.Description); d != "" {
				entry.Description = &d
			}
			p.Resolves = append(p.Resolves, entry)
		}
	}
	c.Pedigree.Patches = append(c.Pedigree.Patches, p)

	// Notes merge.
	notesReplaced := false
	notesAppended := false
	if note := strings.TrimSpace(opts.Notes); note != "" {
		if opts.ReplaceNotes || c.Pedigree.Notes == nil || strings.TrimSpace(*c.Pedigree.Notes) == "" {
			c.Pedigree.Notes = &note
			notesReplaced = opts.ReplaceNotes
		} else {
			combined := *c.Pedigree.Notes + "\n\n" + note
			c.Pedigree.Notes = &combined
			notesAppended = true
		}
	}

	// Validate the whole manifest.
	if errs := validate.Manifest(chosen.parsed, validate.Options{SkipFilesystem: true}); len(errs) > 0 {
		*c = original
		return nil, &ErrInitValidation{Errors: errs}
	}

	res := &PatchResult{
		DryRun:        opts.DryRun,
		Path:          chosen.path,
		Ref:           ref.Raw,
		ComponentName: chosen.name,
		PatchType:     patchType,
		ResolvesCount: len(opts.Resolves),
		NotesReplaced: notesReplaced,
		NotesAppended: notesAppended,
	}

	if opts.DryRun {
		*c = original
		return res, nil
	}

	if err := writeManifest(chosen.path, chosen.parsed); err != nil {
		return nil, err
	}
	return res, nil
}
