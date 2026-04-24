// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// readManifests expands each argument and parses every resulting
// manifest via manifest.ParseFile. Arguments are resolved as:
//
//   - Zero args → discover under `.` (CWD).
//   - A directory → discover under it (see discover()).
//   - A file → parse directly.
//   - Anything else → try as a glob; error if nothing matches.
//
// Files without a schema marker are silently skipped per §12.5 —
// regardless of whether they were found via discovery or supplied
// explicitly. Any other parse error bubbles up so the CLI can map it
// to an exit code.
func readManifests(args []string) ([]*manifest.Manifest, error) {
	paths, err := resolveArgs(args)
	if err != nil {
		return nil, err
	}
	var out []*manifest.Manifest
	for _, path := range paths {
		m, err := manifest.ParseFile(path)
		if err != nil {
			if errors.Is(err, manifest.ErrNoSchemaMarker) {
				// §12.5: a file without a schema marker MUST be
				// ignored silently.
				continue
			}
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// resolveArgs turns the raw positional argument list into a flat list
// of manifest-candidate file paths. Empty args triggers a CWD
// discovery walk per M11; non-empty args expand each in turn via
// expandArg.
func resolveArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		found, err := discover(".")
		if err != nil {
			return nil, fmt.Errorf("discover .: %w", err)
		}
		if len(found) == 0 {
			diag.Warn("discovery: no manifest files found under CWD (looked for %s)",
				discoveryFilenamesForMessage())
		}
		return found, nil
	}
	var paths []string
	for _, arg := range args {
		matches, err := expandArg(arg)
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	return paths, nil
}

// expandArg resolves a single user-supplied argument into a list of
// manifest file paths. Directory arguments trigger a discovery walk
// under that directory (§12.5 non-normative). Globs match; bare files
// pass through.
func expandArg(arg string) ([]string, error) {
	info, err := os.Stat(arg)
	if err == nil {
		if info.IsDir() {
			found, derr := discover(arg)
			if derr != nil {
				return nil, fmt.Errorf("discover %s: %w", arg, derr)
			}
			return found, nil
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

// discoveryFilenamesForMessage renders the discovery basenames in a
// stable comma-separated form for diagnostics.
func discoveryFilenamesForMessage() string {
	// Two adjacent calls return the same string; the map has a fixed
	// three-entry set whose order we pin manually for determinism.
	return ".primary.json, .components.json, .components.csv"
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
