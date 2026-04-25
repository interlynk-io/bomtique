// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
	"github.com/interlynk-io/bomtique/internal/pool"
	"github.com/interlynk-io/bomtique/internal/purl"
	"github.com/interlynk-io/bomtique/internal/regfetch"
)

// UpdateOptions configures a `bomtique manifest update` call.
type UpdateOptions struct {
	FromDir string
	Into    string
	Ref     string
	DryRun  bool

	// Primary, when true, redirects the update to the primary
	// manifest's primary component instead of locating a pool entry
	// by Ref. Ref MUST be empty when Primary is set.
	Primary bool

	// ToVersion bumps the target component's `version` to this value.
	// When set and the current `purl` carries a version segment equal
	// to the old version, the purl is bumped in lockstep; otherwise
	// the purl is left unchanged with a stderr note.
	ToVersion string

	// Standard field replacements. An empty string means "no change"
	// for scalar flags. Slice fields replace wholesale when non-nil.
	Name          string
	Version       string
	Type          string
	Description   string
	License       string
	Purl          string
	CPE           string
	Scope         string
	Supplier      string
	SupplierEmail string
	SupplierURL   string
	Website       string
	VCS           string
	Distribution  string
	IssueTracker  string

	ExternalRefs []ExternalRefSpec
	DependsOn    []string
	Tags         []string

	// --clear-* explicit null-outs. Each resets the corresponding
	// optional field to nil / empty on the target component.
	ClearLicense         bool
	ClearDescription     bool
	ClearSupplier        bool
	ClearPurl            bool
	ClearCPE             bool
	ClearScope           bool
	ClearExternalRefs    bool
	ClearDependsOn       bool
	ClearTags            bool
	ClearPedigreePatches bool

	// Refresh, when true, re-fetches metadata from the importer
	// matching the target component's existing purl and layers flag
	// values on top. Update fails with ErrUnsupportedRef if no
	// importer matches. Default (false): no fetch is attempted; the
	// update is purely flag-driven.
	//
	// The BOMTIQUE_OFFLINE=1 env var disables the fetch (the purl is
	// still validated against the importer set, but no HTTP call is
	// made).
	Refresh  bool
	Registry *regfetch.Registry
	Client   *regfetch.Client
	Ctx      context.Context
}

// UpdateResult reports what changed.
type UpdateResult struct {
	DryRun            bool
	Path              string // components manifest path
	PrimaryPath       string // when the primary's depends-on also changed
	OldRef            string // identity string before update
	NewRef            string // identity string after update
	FieldsChanged     []string
	PurlVersionBumped bool // --to triggered a lockstep purl bump
}

// ErrUpdateNotFound is returned when no component matches the ref.
var ErrUpdateNotFound = errors.New("no component matches the supplied ref")

// Update mutates either an existing pool component (default) or the
// primary manifest's primary component (when opts.Primary is set).
func Update(opts UpdateOptions) (*UpdateResult, error) {
	if opts.Primary {
		return updatePrimary(opts)
	}
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
		oldID  string
	}
	var hits []hit
	cache := map[string]*manifest.Manifest{}

	var targetPath string
	if opts.Into != "" {
		targetPath, err = canonicalise(opts.Into, fromDir)
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
			id, _ := pool.Identify(c)
			hits = append(hits, hit{
				path:   p,
				parsed: m,
				index:  i,
				name:   c.Name,
				oldID:  id.String(),
			})
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
	// Work on a clone so we can validate before committing.
	original := chosen.parsed.Components.Components[chosen.index]
	updated := original

	var fields []string
	purlBumped := false

	// --to version: bump version, sync purl if segments match.
	if strings.TrimSpace(opts.ToVersion) != "" {
		newVer := strings.TrimSpace(opts.ToVersion)
		oldVer := ""
		if updated.Version != nil {
			oldVer = *updated.Version
		}
		if oldVer != newVer {
			updated.Version = &newVer
			fields = append(fields, "version")

			if updated.Purl != nil && *updated.Purl != "" {
				bumped, didBump, err := bumpPurlVersion(*updated.Purl, oldVer, newVer)
				if err != nil {
					return nil, fmt.Errorf("bump purl: %w", err)
				}
				if didBump {
					updated.Purl = &bumped
					fields = append(fields, "purl")
					purlBumped = true
				} else {
					diag.Warn("purl %q left unchanged: version segment does not match old version %q (update manually if needed)", *updated.Purl, oldVer)
				}
			}
		}
	}

	// Registry-metadata refresh (M14.7). Only fires on --refresh to
	// keep `update` predictable for callers doing plain field
	// rewrites. The fetched component fills fields, but any
	// subsequent override from flags takes precedence.
	if opts.Refresh {
		fetched, err := fetchUpdatedMetadata(opts, &updated)
		if err != nil {
			return nil, err
		}
		if fetched != nil {
			merged, _ := MergeComponent(&updated, fetched)
			if merged != nil {
				updated = *merged
				fields = append(fields, "regfetch")
			}
		}
	}

	// Regular scalar / slice field overrides.
	applyOverrides(&updated, opts, &fields)

	// --clear-* null-outs.
	applyClears(&updated, opts, &fields)

	if len(fields) == 0 {
		return nil, fmt.Errorf("update with no changes requested: supply at least one field flag, --to, or --clear-*")
	}

	// Identity collision against siblings in the whole pool.
	newID, err := pool.Identify(&updated)
	if err != nil {
		return nil, fmt.Errorf("identify updated component: %w", err)
	}
	for _, p := range poolPaths {
		m := cache[p]
		for i := range m.Components.Components {
			if p == chosen.path && i == chosen.index {
				continue
			}
			other := &m.Components.Components[i]
			otherID, err := pool.Identify(other)
			if err != nil {
				continue
			}
			if otherID.Key() == newID.Key() {
				return nil, &ErrIdentityCollision{
					Existing: otherID.String(),
					Incoming: newID.String(),
					Path:     p,
				}
			}
		}
	}

	// Commit the update in-place.
	chosen.parsed.Components.Components[chosen.index] = updated

	// Validate the whole manifest.
	if errs := validate.Manifest(chosen.parsed, validate.Options{SkipFilesystem: true}); len(errs) > 0 {
		// Revert in-memory so a dry-run caller or test sees the original.
		chosen.parsed.Components.Components[chosen.index] = original
		return nil, &ErrInitValidation{Errors: errs}
	}

	res := &UpdateResult{
		DryRun:            opts.DryRun,
		Path:              chosen.path,
		OldRef:            chosen.oldID,
		NewRef:            newID.String(),
		FieldsChanged:     fields,
		PurlVersionBumped: purlBumped,
	}

	if opts.DryRun {
		// Revert so on-disk state stays untouched if the caller
		// re-reads the manifest later.
		chosen.parsed.Components.Components[chosen.index] = original
		return res, nil
	}

	if err := writeManifest(chosen.path, chosen.parsed); err != nil {
		return nil, err
	}
	return res, nil
}

// applyOverrides layers flag-supplied scalars / slices on top of the
// current component. Same semantics as Add: non-empty string flags
// replace pointer fields; non-nil slice inputs replace slice fields
// wholesale.
func applyOverrides(c *manifest.Component, opts UpdateOptions, fields *[]string) {
	setStr := func(field string, flagVal string, target **string) {
		if strings.TrimSpace(flagVal) == "" {
			return
		}
		v := strings.TrimSpace(flagVal)
		if *target != nil && **target == v {
			return
		}
		*target = &v
		*fields = append(*fields, field)
	}

	if v := strings.TrimSpace(opts.Name); v != "" && v != c.Name {
		c.Name = v
		*fields = append(*fields, "name")
	}
	// --version flag (distinct from --to which drives purl sync).
	setStr("version", opts.Version, &c.Version)
	setStr("type", opts.Type, &c.Type)
	setStr("description", opts.Description, &c.Description)
	setStr("purl", opts.Purl, &c.Purl)
	setStr("cpe", opts.CPE, &c.CPE)
	setStr("scope", opts.Scope, &c.Scope)

	if v := strings.TrimSpace(opts.License); v != "" {
		if c.License == nil || c.License.Expression != v {
			c.License = &manifest.License{Expression: v}
			*fields = append(*fields, "license")
		}
	}

	if sn := strings.TrimSpace(opts.Supplier); sn != "" || opts.SupplierEmail != "" || opts.SupplierURL != "" {
		s := &manifest.Supplier{Name: sn}
		if e := strings.TrimSpace(opts.SupplierEmail); e != "" {
			s.Email = &e
		}
		if u := strings.TrimSpace(opts.SupplierURL); u != "" {
			s.URL = &u
		}
		c.Supplier = s
		*fields = append(*fields, "supplier")
	}

	newRefs := []manifest.ExternalRef{}
	newRefs = appendExternalRef(newRefs, "website", opts.Website)
	newRefs = appendExternalRef(newRefs, "vcs", opts.VCS)
	newRefs = appendExternalRef(newRefs, "distribution", opts.Distribution)
	newRefs = appendExternalRef(newRefs, "issue-tracker", opts.IssueTracker)
	for _, spec := range opts.ExternalRefs {
		newRefs = appendExternalRef(newRefs, spec.Type, spec.URL)
	}
	if len(newRefs) > 0 {
		c.ExternalReferences = newRefs
		*fields = append(*fields, "external_references")
	}

	if opts.DependsOn != nil {
		c.DependsOn = append([]string(nil), opts.DependsOn...)
		*fields = append(*fields, "depends-on")
	}
	if opts.Tags != nil {
		c.Tags = append([]string(nil), opts.Tags...)
		*fields = append(*fields, "tags")
	}
}

// applyClears processes --clear-* flags.
func applyClears(c *manifest.Component, opts UpdateOptions, fields *[]string) {
	if opts.ClearLicense && c.License != nil {
		c.License = nil
		*fields = append(*fields, "clear:license")
	}
	if opts.ClearDescription && c.Description != nil {
		c.Description = nil
		*fields = append(*fields, "clear:description")
	}
	if opts.ClearSupplier && c.Supplier != nil {
		c.Supplier = nil
		*fields = append(*fields, "clear:supplier")
	}
	if opts.ClearPurl && c.Purl != nil {
		c.Purl = nil
		*fields = append(*fields, "clear:purl")
	}
	if opts.ClearCPE && c.CPE != nil {
		c.CPE = nil
		*fields = append(*fields, "clear:cpe")
	}
	if opts.ClearScope && c.Scope != nil {
		c.Scope = nil
		*fields = append(*fields, "clear:scope")
	}
	if opts.ClearExternalRefs && c.ExternalReferences != nil {
		c.ExternalReferences = nil
		*fields = append(*fields, "clear:external_references")
	}
	if opts.ClearDependsOn && c.DependsOn != nil {
		c.DependsOn = nil
		*fields = append(*fields, "clear:depends-on")
	}
	if opts.ClearTags && c.Tags != nil {
		c.Tags = nil
		*fields = append(*fields, "clear:tags")
	}
	if opts.ClearPedigreePatches && c.Pedigree != nil && c.Pedigree.Patches != nil {
		c.Pedigree.Patches = nil
		*fields = append(*fields, "clear:pedigree.patches")
	}
}

// fetchUpdatedMetadata refetches metadata for the target component
// using its existing purl as the importer ref. Honoured only when
// opts.Refresh is set. Returns ErrUnsupportedRef when the purl isn't
// importable. Honors BOMTIQUE_OFFLINE=1 by skipping the HTTP call.
func fetchUpdatedMetadata(opts UpdateOptions, updated *manifest.Component) (*manifest.Component, error) {
	var ref string
	if updated.Purl != nil {
		ref = strings.TrimSpace(*updated.Purl)
	}
	if !strings.HasPrefix(ref, "pkg:") {
		return nil, fmt.Errorf("%w: --refresh requires the target component to carry a pkg: purl", regfetch.ErrUnsupportedRef)
	}
	registry := opts.Registry
	if registry == nil {
		registry = regfetch.Default()
	}
	imp := registry.Match(ref)
	if imp == nil {
		return nil, fmt.Errorf("%w: %q", regfetch.ErrUnsupportedRef, ref)
	}
	if os.Getenv("BOMTIQUE_OFFLINE") == "1" {
		return nil, nil
	}
	client := opts.Client
	if client == nil {
		client = regfetch.NewClient()
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return imp.Fetch(ctx, client, ref)
}

// bumpPurlVersion returns a new purl string with its version segment
// replaced when the current segment equals oldVer. didBump reports
// whether a change occurred.
func bumpPurlVersion(raw, oldVer, newVer string) (updated string, didBump bool, err error) {
	p, err := purl.Parse(raw)
	if err != nil {
		return raw, false, err
	}
	if p.Version != oldVer {
		return raw, false, nil
	}
	p.Version = newVer
	return p.String(), true, nil
}

// updatePrimary mutates the primary component inside .primary.json.
// Same field semantics as the pool path (--to, --refresh, scalar
// overrides, --clear-*), but operates on the single primary
// component without identity-collision checks (primary is its own
// role, not a pool member).
func updatePrimary(opts UpdateOptions) (*UpdateResult, error) {
	if strings.TrimSpace(opts.Ref) != "" {
		return nil, fmt.Errorf("--primary takes no <ref> argument; remove %q", opts.Ref)
	}
	if strings.TrimSpace(opts.Into) != "" {
		return nil, errors.New("--into is not valid with --primary; the primary always lives in .primary.json")
	}

	fromDir := opts.FromDir
	if fromDir == "" {
		fromDir = "."
	}
	primaryPath, err := LocatePrimary(fromDir)
	if err != nil {
		return nil, err
	}
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

	original := m.Primary.Primary
	updated := original

	var fields []string
	purlBumped := false

	// --to version: bump version, sync purl segment when it matches.
	if newVer := strings.TrimSpace(opts.ToVersion); newVer != "" {
		oldVer := ""
		if updated.Version != nil {
			oldVer = *updated.Version
		}
		if oldVer != newVer {
			updated.Version = &newVer
			fields = append(fields, "version")
			if updated.Purl != nil && *updated.Purl != "" {
				bumped, didBump, err := bumpPurlVersion(*updated.Purl, oldVer, newVer)
				if err != nil {
					return nil, fmt.Errorf("bump purl: %w", err)
				}
				if didBump {
					updated.Purl = &bumped
					fields = append(fields, "purl")
					purlBumped = true
				} else {
					diag.Warn("purl %q left unchanged: version segment does not match old version %q (update manually if needed)", *updated.Purl, oldVer)
				}
			}
		}
	}

	if opts.Refresh {
		fetched, err := fetchUpdatedMetadata(opts, &updated)
		if err != nil {
			return nil, err
		}
		if fetched != nil {
			merged, _ := MergeComponent(&updated, fetched)
			if merged != nil {
				updated = *merged
				fields = append(fields, "regfetch")
			}
		}
	}

	applyOverrides(&updated, opts, &fields)
	applyClears(&updated, opts, &fields)

	if len(fields) == 0 {
		return nil, fmt.Errorf("update with no changes requested: supply at least one field flag, --to, or --clear-*")
	}

	oldRef := primaryIdentString(&original)
	newRef := primaryIdentString(&updated)

	m.Primary.Primary = updated
	if errs := validate.Manifest(m, validate.Options{SkipFilesystem: true}); len(errs) > 0 {
		m.Primary.Primary = original
		return nil, &ErrInitValidation{Errors: errs}
	}

	res := &UpdateResult{
		DryRun:            opts.DryRun,
		Path:              primaryPath,
		OldRef:            oldRef,
		NewRef:            newRef,
		FieldsChanged:     fields,
		PurlVersionBumped: purlBumped,
	}
	if opts.DryRun {
		m.Primary.Primary = original
		return res, nil
	}
	if err := writeManifest(primaryPath, m); err != nil {
		return nil, err
	}
	return res, nil
}

// primaryIdentString returns a human-readable identity for the
// primary component: its purl when present, else "<name>@<version>",
// else just the name.
func primaryIdentString(c *manifest.Component) string {
	if c.Purl != nil && strings.TrimSpace(*c.Purl) != "" {
		return *c.Purl
	}
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		return c.Name + "@" + *c.Version
	}
	return c.Name
}
