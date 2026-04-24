// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package pool_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

// -----------------------------------------------------------------------------
// Fixtures / helpers.
// -----------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

func compManifest(path string, cs ...manifest.Component) *manifest.Manifest {
	return &manifest.Manifest{
		Path:   path,
		Kind:   manifest.KindComponents,
		Format: manifest.FormatJSON,
		Components: &manifest.ComponentsManifest{
			Schema:     manifest.SchemaComponentsV1,
			Components: cs,
		},
	}
}

// captureWarnings redirects diag output to a buffer for the duration of
// fn and returns the captured text. Warnings made outside fn are not
// captured.
func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	diag.SetSink(&buf)
	diag.Reset()
	t.Cleanup(func() {
		diag.SetSink(nil)
		diag.Reset()
	})
	fn()
	return buf.String()
}

// -----------------------------------------------------------------------------
// Identity extraction (§11).
// -----------------------------------------------------------------------------

func TestIdentify_PurlPrecedence(t *testing.T) {
	c := manifest.Component{
		Name:    "libfoo",
		Version: strPtr("1.0.0"),
		Purl:    strPtr("pkg:generic/libfoo@1.0.0"),
	}
	id, err := pool.Identify(&c)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Kind != pool.KindPurl {
		t.Fatalf("Kind: got %v, want Purl", id.Kind)
	}
	if id.Purl != "pkg:generic/libfoo@1.0.0" {
		t.Fatalf("Purl: got %q", id.Purl)
	}
}

func TestIdentify_PurlCanonicalises(t *testing.T) {
	// Upper-case type segment is normalised to lower-case by the purl
	// canonicaliser; two inputs differing only in case should canonicalise equal.
	a := manifest.Component{
		Name: "libfoo",
		Purl: strPtr("pkg:GENERIC/libfoo@1.0.0"),
	}
	b := manifest.Component{
		Name: "libfoo",
		Purl: strPtr("pkg:generic/libfoo@1.0.0"),
	}
	ia, err := pool.Identify(&a)
	if err != nil {
		t.Fatalf("Identify a: %v", err)
	}
	ib, err := pool.Identify(&b)
	if err != nil {
		t.Fatalf("Identify b: %v", err)
	}
	if ia.Purl != ib.Purl {
		t.Fatalf("canonicalisation differs: %q vs %q", ia.Purl, ib.Purl)
	}
	if ia.Key() != ib.Key() {
		t.Fatalf("keys differ: %q vs %q", ia.Key(), ib.Key())
	}
}

func TestIdentify_NameVersionFallback(t *testing.T) {
	c := manifest.Component{Name: "libfoo", Version: strPtr("1.0.0")}
	id, err := pool.Identify(&c)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Kind != pool.KindNameVersion {
		t.Fatalf("Kind: got %v, want NameVersion", id.Kind)
	}
	if id.Version != "1.0.0" {
		t.Fatalf("Version: got %q", id.Version)
	}
}

func TestIdentify_NameOnlyFallback(t *testing.T) {
	c := manifest.Component{Name: "libfoo"}
	id, err := pool.Identify(&c)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Kind != pool.KindNameOnly {
		t.Fatalf("Kind: got %v, want NameOnly", id.Kind)
	}
}

func TestIdentify_EmptyNameFails(t *testing.T) {
	c := manifest.Component{Name: "   "}
	if _, err := pool.Identify(&c); err == nil {
		t.Fatal("expected error for empty name")
	}
}

// -----------------------------------------------------------------------------
// Direct dedup pass — four §11 cases.
// -----------------------------------------------------------------------------

func TestBuild_DuplicatePurl(t *testing.T) {
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
		manifest.Component{Name: "libfoo-other", Version: strPtr("9"), Purl: strPtr("pkg:generic/libfoo@1")},
	)
	var p *pool.Pool
	warns := captureWarnings(t, func() {
		var err error
		p, err = pool.Build([]*manifest.Manifest{m})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	if got := len(p.Components); got != 1 {
		t.Fatalf("pool size: got %d, want 1", got)
	}
	if p.Components[0].Name != "libfoo" {
		t.Fatalf("kept wrong entry: %+v", p.Components[0])
	}
	if !strings.Contains(warns, "duplicate purl") {
		t.Fatalf("expected duplicate-purl warning, got: %s", warns)
	}
}

func TestBuild_SameNVDifferentPurlsBothKept(t *testing.T) {
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:acme/libfoo@1")},
	)
	var p *pool.Pool
	warns := captureWarnings(t, func() {
		var err error
		p, err = pool.Build([]*manifest.Manifest{m})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	if got := len(p.Components); got != 2 {
		t.Fatalf("pool size: got %d, want 2 (distinct purls)", got)
	}
	if !strings.Contains(warns, "differing purls") {
		t.Fatalf("expected differing-purl warning, got: %s", warns)
	}
}

func TestBuild_DuplicateNoPurlNameVersion(t *testing.T) {
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1")},
		manifest.Component{Name: "libfoo", Version: strPtr("1")},
	)
	var p *pool.Pool
	warns := captureWarnings(t, func() {
		var err error
		p, err = pool.Build([]*manifest.Manifest{m})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	if got := len(p.Components); got != 1 {
		t.Fatalf("pool size: got %d, want 1", got)
	}
	if !strings.Contains(warns, "duplicate (name, version)") {
		t.Fatalf("expected no-purl dup warning, got: %s", warns)
	}
}

func TestBuild_DuplicateNameOnly(t *testing.T) {
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Hashes: []manifest.Hash{{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))}}},
		manifest.Component{Name: "libfoo", Hashes: []manifest.Hash{{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("b", 64))}}},
	)
	var p *pool.Pool
	warns := captureWarnings(t, func() {
		var err error
		p, err = pool.Build([]*manifest.Manifest{m})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	if got := len(p.Components); got != 1 {
		t.Fatalf("pool size: got %d, want 1", got)
	}
	if !strings.Contains(warns, "duplicate name-only") {
		t.Fatalf("expected name-only dup warning, got: %s", warns)
	}
}

// -----------------------------------------------------------------------------
// Secondary mixed purl / no-purl pass.
// -----------------------------------------------------------------------------

func TestBuild_SecondaryMerge(t *testing.T) {
	// purl-bearing has no hashes; no-purl has hashes. After merge the
	// pool contains one entry carrying the purl-bearing's identity AND
	// the no-purl entry's hashes.
	purlBearing := manifest.Component{
		Name:    "libfoo",
		Version: strPtr("1.0.0"),
		Purl:    strPtr("pkg:generic/libfoo@1.0.0"),
	}
	noPurl := manifest.Component{
		Name:    "libfoo",
		Version: strPtr("1.0.0"),
		Hashes: []manifest.Hash{{
			Algorithm: "SHA-256",
			Value:     strPtr(strings.Repeat("a", 64)),
		}},
	}
	m := compManifest("a.json", purlBearing, noPurl)

	var p *pool.Pool
	warns := captureWarnings(t, func() {
		var err error
		p, err = pool.Build([]*manifest.Manifest{m})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	if got := len(p.Components); got != 1 {
		t.Fatalf("pool size: got %d, want 1 after merge", got)
	}
	merged := p.Components[0]
	if merged.Purl == nil || *merged.Purl != "pkg:generic/libfoo@1.0.0" {
		t.Fatalf("merge lost purl: %+v", merged.Purl)
	}
	if len(merged.Hashes) != 1 {
		t.Fatalf("merge lost hashes: %+v", merged.Hashes)
	}
	if !strings.Contains(warns, "merging no-purl") {
		t.Fatalf("expected secondary-merge warning, got: %s", warns)
	}
}

func TestBuild_SecondaryFieldConflict(t *testing.T) {
	purlBearing := manifest.Component{
		Name:        "libfoo",
		Version:     strPtr("1.0.0"),
		Purl:        strPtr("pkg:generic/libfoo@1.0.0"),
		Description: strPtr("purl-bearing says hi"),
	}
	noPurl := manifest.Component{
		Name:        "libfoo",
		Version:     strPtr("1.0.0"),
		Description: strPtr("no-purl says something different"),
	}
	m := compManifest("a.json", purlBearing, noPurl)

	var p *pool.Pool
	warns := captureWarnings(t, func() {
		var err error
		p, err = pool.Build([]*manifest.Manifest{m})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	merged := p.Components[0]
	if *merged.Description != "purl-bearing says hi" {
		t.Fatalf("conflict resolution kept wrong description: %q", *merged.Description)
	}
	if !strings.Contains(warns, "field conflict") || !strings.Contains(warns, "description") {
		t.Fatalf("expected field-conflict warning for description, got: %s", warns)
	}
}

func TestBuild_SecondaryNameOnlyNotMatched(t *testing.T) {
	// §11 last paragraph: name-only components are NOT matched by the
	// secondary pass. Here the purl-bearing has (libfoo, 1.0.0); a
	// name-only "libfoo" is kept as its own entry, no merge.
	m := compManifest("a.json",
		manifest.Component{
			Name:    "libfoo",
			Version: strPtr("1.0.0"),
			Purl:    strPtr("pkg:generic/libfoo@1.0.0"),
		},
		manifest.Component{
			Name:   "libfoo",
			Hashes: []manifest.Hash{{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))}},
		},
	)
	p, err := pool.Build([]*manifest.Manifest{m})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := len(p.Components); got != 2 {
		t.Fatalf("pool size: got %d, want 2", got)
	}
}

// -----------------------------------------------------------------------------
// Multiple manifests — input order preserved, provenance in warnings.
// -----------------------------------------------------------------------------

func TestBuild_AcrossMultipleManifestsOrderPreserved(t *testing.T) {
	m1 := compManifest("a.json",
		manifest.Component{Name: "alpha", Version: strPtr("1"), Purl: strPtr("pkg:generic/alpha@1")},
	)
	m2 := compManifest("b.json",
		manifest.Component{Name: "beta", Version: strPtr("1"), Purl: strPtr("pkg:generic/beta@1")},
	)
	m3 := compManifest("c.json",
		manifest.Component{Name: "gamma", Version: strPtr("1"), Purl: strPtr("pkg:generic/gamma@1")},
	)
	p, err := pool.Build([]*manifest.Manifest{m1, m2, m3})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantOrder := []string{"alpha", "beta", "gamma"}
	if len(p.Components) != len(wantOrder) {
		t.Fatalf("pool size: got %d, want %d", len(p.Components), len(wantOrder))
	}
	for i, want := range wantOrder {
		if p.Components[i].Name != want {
			t.Fatalf("pool[%d]: got %q, want %q", i, p.Components[i].Name, want)
		}
	}
}

func TestBuild_WarningIdentifiesSourceManifest(t *testing.T) {
	m1 := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
	)
	m2 := compManifest("b.json",
		manifest.Component{Name: "libfoo-copy", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
	)
	warns := captureWarnings(t, func() {
		if _, err := pool.Build([]*manifest.Manifest{m1, m2}); err != nil {
			t.Fatalf("Build: %v", err)
		}
	})
	if !strings.Contains(warns, "a.json#/components/0") || !strings.Contains(warns, "b.json#/components/0") {
		t.Fatalf("warning doesn't cite source manifests: %s", warns)
	}
}

// -----------------------------------------------------------------------------
// Primary-vs-pool distinctness.
// -----------------------------------------------------------------------------

func TestCheckPrimaryDistinct_CollisionRejected(t *testing.T) {
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
	)
	p, err := pool.Build([]*manifest.Manifest{m})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	primary := &manifest.Component{
		Name:    "libfoo",
		Version: strPtr("1"),
		Purl:    strPtr("pkg:generic/libfoo@1"),
	}
	if err := pool.CheckPrimaryDistinct(primary, p); err == nil {
		t.Fatal("expected collision error, got nil")
	}
}

func TestCheckPrimaryDistinct_DifferentIdentities(t *testing.T) {
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
	)
	p, err := pool.Build([]*manifest.Manifest{m})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	primary := &manifest.Component{
		Name:    "libfoo-app",
		Version: strPtr("1"),
		Purl:    strPtr("pkg:generic/libfoo-app@1"),
	}
	if err := pool.CheckPrimaryDistinct(primary, p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckPrimaryDistinct_CrossKindNotACollision(t *testing.T) {
	// §11: name-only primary and (name, version) pool entry share Name
	// but different Kind → not a collision.
	m := compManifest("a.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1")},
	)
	p, err := pool.Build([]*manifest.Manifest{m})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	primary := &manifest.Component{
		Name:   "libfoo",
		Hashes: []manifest.Hash{{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))}},
	}
	if err := pool.CheckPrimaryDistinct(primary, p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// No-input and nil-safety.
// -----------------------------------------------------------------------------

func TestBuild_EmptyInputProducesEmptyPool(t *testing.T) {
	p, err := pool.Build(nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(p.Components) != 0 {
		t.Fatalf("expected empty pool, got %d", len(p.Components))
	}
}

func TestBuild_IgnoresPrimariesMixedIn(t *testing.T) {
	cm := compManifest("c.json",
		manifest.Component{Name: "libfoo", Version: strPtr("1"), Purl: strPtr("pkg:generic/libfoo@1")},
	)
	pm := &manifest.Manifest{
		Path: "p.json", Kind: manifest.KindPrimary, Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema:  manifest.SchemaPrimaryV1,
			Primary: manifest.Component{Name: "acme-app", Version: strPtr("1"), Purl: strPtr("pkg:generic/acme-app@1")},
		},
	}
	p, err := pool.Build([]*manifest.Manifest{pm, cm})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(p.Components) != 1 || p.Components[0].Name != "libfoo" {
		t.Fatalf("primary leaked into pool: %+v", p.Components)
	}
}
