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

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// RemoveOptions configures a single `bomtique manifest remove` call.
type RemoveOptions struct {
	// FromDir anchors the walks. Defaults to CWD.
	FromDir string

	// Into restricts the pool search to a single manifest path. When
	// set, any multi-file match is disambiguated to this file.
	Into string

	// Primary, when true, skips the pool entirely and only scrubs the
	// primary manifest's depends-on.
	Primary bool

	// DryRun, when true, reports what would change and does not write.
	DryRun bool

	// Ref is the user-supplied reference: a `pkg:` purl or a
	// `name@version` string (§10.2).
	Ref string
}

// RemoveResult summarises a Remove invocation.
type RemoveResult struct {
	// DryRun echoes RemoveOptions.DryRun for the caller.
	DryRun bool

	// PoolPath is the components-manifest file from which the target
	// component was removed. Empty when Primary=true.
	PoolPath string

	// PoolComponentName names the component that was removed, for
	// stdout confirmation.
	PoolComponentName string

	// PrimaryPath is the primary manifest file, populated whenever the
	// remove reached it (either to scrub a depends-on edge, or because
	// --primary targeted it directly).
	PrimaryPath string

	// PrimaryEdgeScrubbed is true when the primary's depends-on had an
	// entry matching Ref and that entry was (or would be, on dry-run)
	// removed.
	PrimaryEdgeScrubbed bool

	// ScrubbedEdges lists every depends-on edge removed from a pool
	// component. Each entry names the manifest path and the referring
	// component by its identity string.
	ScrubbedEdges []ScrubbedEdge
}

// ScrubbedEdge records one depends-on edge removed during a Remove.
type ScrubbedEdge struct {
	ManifestPath string
	FromName     string
	FromIdentity string
	Ref          string
}

// ErrRemoveNotFound is returned when the pool contains no component
// matching the supplied ref.
var ErrRemoveNotFound = errors.New("no component in the pool matches the supplied ref")

// ErrRemoveMultiMatch is returned when the target ref matches
// components in more than one manifest file and --into was not set
// to disambiguate.
type ErrRemoveMultiMatch struct {
	Ref  string
	Hits []string
}

func (e *ErrRemoveMultiMatch) Error() string {
	return fmt.Sprintf("%q matches components in %d files: %s (use --into <path> to disambiguate)",
		e.Ref, len(e.Hits), strings.Join(e.Hits, ", "))
}

// Remove is the entry point for `bomtique manifest remove`.
func Remove(opts RemoveOptions) (*RemoveResult, error) {
	ref, err := graph.ParseRef(strings.TrimSpace(opts.Ref))
	if err != nil {
		return nil, fmt.Errorf("parse ref %q: %w", opts.Ref, err)
	}

	fromDir := opts.FromDir
	if fromDir == "" {
		fromDir = "."
	}
	primaryPath, err := LocatePrimary(fromDir)
	if err != nil {
		return nil, err
	}

	if opts.Primary {
		return removePrimaryEdge(primaryPath, ref, opts.DryRun)
	}

	return removeFromPool(primaryPath, fromDir, opts.Into, ref, opts.DryRun)
}

// removePrimaryEdge scrubs one matching entry from the primary's
// depends-on. A miss is a hard error so scripting remains honest.
func removePrimaryEdge(primaryPath string, ref graph.Ref, dryRun bool) (*RemoveResult, error) {
	data, err := os.ReadFile(primaryPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", primaryPath, err)
	}
	m, err := manifest.ParseJSON(data, primaryPath)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", primaryPath, err)
	}
	if m.Kind != manifest.KindPrimary || m.Primary == nil {
		return nil, fmt.Errorf("%s is not a primary manifest", primaryPath)
	}

	kept, scrubbed := filterDependsOn(m.Primary.Primary.DependsOn, ref)
	if !scrubbed {
		return nil, fmt.Errorf("%s: depends-on has no entry matching %q", primaryPath, ref.Raw)
	}
	m.Primary.Primary.DependsOn = kept

	res := &RemoveResult{
		DryRun:              dryRun,
		PrimaryPath:         primaryPath,
		PrimaryEdgeScrubbed: true,
	}
	if dryRun {
		return res, nil
	}
	if err := writeManifest(primaryPath, m); err != nil {
		return nil, err
	}
	return res, nil
}

// removeFromPool finds and drops the target component from exactly
// one components manifest, then scrubs any depends-on edges pointing
// at it in every other reachable manifest (pool + primary).
func removeFromPool(primaryPath, fromDir, into string, ref graph.Ref, dryRun bool) (*RemoveResult, error) {
	rootDir := filepath.Dir(primaryPath)
	poolPaths, err := discoverComponentsManifests(rootDir)
	if err != nil {
		return nil, err
	}

	type poolHit struct {
		path    string
		parsed  *manifest.Manifest
		hitIdx  int
		hitName string
	}
	var hits []poolHit

	// Cache parsed manifests keyed by path so we don't re-parse when we
	// scrub the same file.
	cache := map[string]*manifest.Manifest{}

	targetPath := ""
	if into != "" {
		targetPath, err = canonicalise(into, fromDir)
		if err != nil {
			return nil, err
		}
	}

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
			hits = append(hits, poolHit{
				path:    p,
				parsed:  m,
				hitIdx:  i,
				hitName: c.Name,
			})
			break // at most one hit per file (§11 guarantees)
		}
	}

	if len(hits) == 0 {
		return nil, fmt.Errorf("%w (ref %q)", ErrRemoveNotFound, ref.Raw)
	}
	if len(hits) > 1 {
		paths := make([]string, len(hits))
		for i, h := range hits {
			paths[i] = h.path
		}
		return nil, &ErrRemoveMultiMatch{Ref: ref.Raw, Hits: paths}
	}

	chosen := hits[0]
	// Drop the entry.
	chosen.parsed.Components.Components = append(
		chosen.parsed.Components.Components[:chosen.hitIdx],
		chosen.parsed.Components.Components[chosen.hitIdx+1:]...,
	)

	res := &RemoveResult{
		DryRun:            dryRun,
		PoolPath:          chosen.path,
		PoolComponentName: chosen.hitName,
		PrimaryPath:       primaryPath,
	}

	// Scrub every reachable pool manifest for depends-on edges pointing
	// at the removed component. The chosen manifest's slice is already
	// one shorter (the target was dropped above), so we never revisit
	// the removed entry here.
	for _, p := range poolPaths {
		m, err := parseComponentsManifestCached(cache, p)
		if err != nil {
			return nil, err
		}
		for i := range m.Components.Components {
			c := &m.Components.Components[i]
			kept, scrubbed := filterDependsOn(c.DependsOn, ref)
			if scrubbed {
				id, _ := pool.Identify(c)
				res.ScrubbedEdges = append(res.ScrubbedEdges, ScrubbedEdge{
					ManifestPath: p,
					FromName:     c.Name,
					FromIdentity: id.String(),
					Ref:          ref.Raw,
				})
				c.DependsOn = kept
			}
		}
	}

	// Scrub primary depends-on.
	primaryChanged := false
	primaryManifest, err := parsePrimaryCached(cache, primaryPath)
	if err != nil {
		return nil, err
	}
	kept, scrubbed := filterDependsOn(primaryManifest.Primary.Primary.DependsOn, ref)
	if scrubbed {
		primaryManifest.Primary.Primary.DependsOn = kept
		res.PrimaryEdgeScrubbed = true
		primaryChanged = true
	}

	// Warn on stderr per scrubbed edge so --warnings-as-errors semantics
	// work as expected.
	for _, e := range res.ScrubbedEdges {
		diag.Warn("scrubbed depends-on edge %q from %s (component %s)",
			e.Ref, e.ManifestPath, e.FromIdentity)
	}
	if res.PrimaryEdgeScrubbed {
		diag.Warn("scrubbed depends-on edge %q from primary %s", ref.Raw, primaryPath)
	}

	if dryRun {
		return res, nil
	}

	// Write the chosen pool manifest.
	if err := writeManifest(chosen.path, chosen.parsed); err != nil {
		return nil, err
	}
	// Write every other pool manifest that had a scrub.
	scrubbedPaths := map[string]struct{}{}
	for _, e := range res.ScrubbedEdges {
		scrubbedPaths[e.ManifestPath] = struct{}{}
	}
	for p := range scrubbedPaths {
		if p == chosen.path {
			continue
		}
		m := cache[p]
		if err := writeManifest(p, m); err != nil {
			return nil, err
		}
	}
	if primaryChanged {
		if err := writeManifest(primaryPath, primaryManifest); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// componentMatchesRef compares a pool component's identity against a
// parsed ref.
func componentMatchesRef(c *manifest.Component, ref graph.Ref) (bool, error) {
	id, err := pool.Identify(c)
	if err != nil {
		// Skip unidentifiable components; they'd fail validation
		// elsewhere. Returning false lets the walk keep going.
		return false, nil
	}
	switch ref.Kind {
	case graph.RefPurl:
		return id.Kind == pool.KindPurl && id.Purl == ref.Purl, nil
	case graph.RefNameVersion:
		return id.Kind == pool.KindNameVersion &&
			id.Name == ref.Name &&
			id.Version == ref.Version, nil
	default:
		return false, fmt.Errorf("unsupported ref kind")
	}
}

// filterDependsOn returns the depends-on list with any edges matching
// ref removed, and reports whether at least one edge was dropped.
func filterDependsOn(deps []string, ref graph.Ref) (kept []string, scrubbed bool) {
	if len(deps) == 0 {
		return deps, false
	}
	kept = make([]string, 0, len(deps))
	for _, raw := range deps {
		parsed, err := graph.ParseRef(raw)
		if err != nil {
			// Malformed edge: keep it. Validator will catch it.
			kept = append(kept, raw)
			continue
		}
		if parsed.Kind != ref.Kind {
			kept = append(kept, raw)
			continue
		}
		switch ref.Kind {
		case graph.RefPurl:
			if parsed.Purl == ref.Purl {
				scrubbed = true
				continue
			}
		case graph.RefNameVersion:
			if parsed.Name == ref.Name && parsed.Version == ref.Version {
				scrubbed = true
				continue
			}
		}
		kept = append(kept, raw)
	}
	if !scrubbed {
		return deps, false
	}
	if len(kept) == 0 {
		kept = nil
	}
	return kept, true
}

// discoverComponentsManifests walks root and returns every
// .components.json / .components.csv path, applying the same dir
// exclusions as cmd/bomtique/discovery.
func discoverComponentsManifests(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if _, skip := locateExcludedDirs[name]; skip {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		switch d.Name() {
		case ".components.json", ".components.csv":
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func parseComponentsManifestCached(cache map[string]*manifest.Manifest, path string) (*manifest.Manifest, error) {
	if m, ok := cache[path]; ok {
		return m, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m *manifest.Manifest
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		m, err = manifest.ParseCSV(data, path)
	} else {
		m, err = manifest.ParseJSON(data, path)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Kind != manifest.KindComponents || m.Components == nil {
		return nil, fmt.Errorf("%s is not a components manifest", path)
	}
	cache[path] = m
	return m, nil
}

func parsePrimaryCached(cache map[string]*manifest.Manifest, path string) (*manifest.Manifest, error) {
	if m, ok := cache[path]; ok {
		return m, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	m, err := manifest.ParseJSON(data, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Kind != manifest.KindPrimary || m.Primary == nil {
		return nil, fmt.Errorf("%s is not a primary manifest", path)
	}
	cache[path] = m
	return m, nil
}

func canonicalise(p, fromDir string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(fromDir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
