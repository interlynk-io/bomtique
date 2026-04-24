// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// readManifests expands each argument (file path or glob) and parses
// every resulting manifest via manifest.ParseFile. Directory arguments
// are a future-M11 concern and return a diagnostic today.
//
// Files whose parse fails return a hard error so the caller can map to
// exit codes; routing between validation errors and I/O errors is the
// caller's job too.
func readManifests(args []string) ([]*manifest.Manifest, error) {
	var out []*manifest.Manifest
	for _, arg := range args {
		matches, err := expandArg(arg)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			m, err := manifest.ParseFile(path)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			out = append(out, m)
		}
	}
	return out, nil
}

// expandArg resolves a single user-supplied argument into a list of
// manifest file paths. Globs match; bare files pass through; directory
// walking is reserved for M11 and returns an error today.
func expandArg(arg string) ([]string, error) {
	info, err := os.Stat(arg)
	if err == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("%s: directory argument — discovery lands in M11; pass file paths explicitly for now", arg)
		}
		return []string{arg}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", arg, err)
	}
	// Non-existent path: try as glob before giving up.
	matches, gerr := filepath.Glob(arg)
	if gerr != nil {
		return nil, fmt.Errorf("glob %s: %w", arg, gerr)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s: no such file (and no glob matches)", arg)
	}
	return matches, nil
}

// partitionedSet separates parsed manifests by kind. This is the
// processing-set shape §12.1 / §12.2 talk about.
type partitionedSet struct {
	Primaries  []*manifest.Manifest
	Components []*manifest.Manifest
}

func partition(manifests []*manifest.Manifest) partitionedSet {
	var ps partitionedSet
	for _, m := range manifests {
		if m == nil {
			continue
		}
		switch m.Kind {
		case manifest.KindPrimary:
			ps.Primaries = append(ps.Primaries, m)
		case manifest.KindComponents:
			ps.Components = append(ps.Components, m)
		}
	}
	return ps
}

// provenanceIndex maps a pool component's identity key to the source
// manifest's directory. It's built from the *input* components
// manifests so a caller can, given a deduped Pool, recover each
// surviving component's "which manifest did you come from" directory
// for path-bearing fields (license.file, hash.file, patch diff.url).
//
// First occurrence wins, matching pool.Build's "first occurrence by
// merge order" dedup rule. Name-only identities may collide across
// manifests; we accept the first for the same reason.
type provenanceIndex map[string]string

func buildProvenanceIndex(componentsManifests []*manifest.Manifest) (provenanceIndex, error) {
	idx := make(provenanceIndex)
	for _, m := range componentsManifests {
		if m == nil || m.Components == nil {
			continue
		}
		dir := filepath.Dir(m.Path)
		for i := range m.Components.Components {
			c := &m.Components.Components[i]
			id, err := pool.Identify(c)
			if err != nil {
				return nil, fmt.Errorf("%s: components[%d]: %w", m.Path, i, err)
			}
			key := id.Key()
			if _, ok := idx[key]; !ok {
				idx[key] = dir
			}
		}
	}
	return idx, nil
}

// manifestDirFor returns the provenance directory for a pool component,
// falling back to "" when lookup fails (unknown identity — shouldn't
// happen for components the dedupe pass kept).
func (idx provenanceIndex) manifestDirFor(c *manifest.Component) string {
	id, err := pool.Identify(c)
	if err != nil {
		return ""
	}
	return idx[id.Key()]
}

// filterByTags drops pool components whose `tags` don't include at
// least one of the requested tags. §6.2 defines this as a per-build
// filter. An empty `tags` argument leaves the slice untouched. A
// component with no tags is excluded when a filter is active.
func filterByTags(components []manifest.Component, tags []string) []manifest.Component {
	if len(tags) == 0 {
		return components
	}
	wanted := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		wanted[t] = struct{}{}
	}
	out := make([]manifest.Component, 0, len(components))
	for _, c := range components {
		for _, t := range c.Tags {
			if _, ok := wanted[t]; ok {
				out = append(out, c)
				break
			}
		}
	}
	return out
}
