// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package graph_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/graph"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/pool"
)

func strPtr(s string) *string { return &s }

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

func componentsManifest(cs ...manifest.Component) *manifest.Manifest {
	return &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatJSON,
		Components: &manifest.ComponentsManifest{
			Schema:     manifest.SchemaComponentsV1,
			Components: cs,
		},
	}
}

// -----------------------------------------------------------------------------
// §10.2 — reference parsing.
// -----------------------------------------------------------------------------

func TestParseRef_Purl(t *testing.T) {
	ref, err := graph.ParseRef("pkg:generic/acme/libfoo@1.0.0")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	if ref.Kind != graph.RefPurl {
		t.Fatalf("Kind: got %v, want RefPurl", ref.Kind)
	}
	if ref.Purl != "pkg:generic/acme/libfoo@1.0.0" {
		t.Fatalf("Purl: got %q", ref.Purl)
	}
}

func TestParseRef_PurlInvalid(t *testing.T) {
	_, err := graph.ParseRef("pkg:bogus-not-a-real-purl")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, graph.ErrInvalidPurlReference) {
		t.Fatalf("expected ErrInvalidPurlReference, got %v", err)
	}
}

func TestParseRef_NameVersionLastAt(t *testing.T) {
	// Scoped npm identifier — last-@ split per §10.2 keeps the leading `@scope/name`.
	ref, err := graph.ParseRef("@angular/core@1.0.0")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	if ref.Kind != graph.RefNameVersion {
		t.Fatalf("Kind: got %v, want RefNameVersion", ref.Kind)
	}
	if ref.Name != "@angular/core" || ref.Version != "1.0.0" {
		t.Fatalf("split mismatch: name=%q version=%q", ref.Name, ref.Version)
	}
}

func TestParseRef_PlainNameVersion(t *testing.T) {
	ref, err := graph.ParseRef("libfoo@1.0.0")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	if ref.Name != "libfoo" || ref.Version != "1.0.0" {
		t.Fatalf("split mismatch: name=%q version=%q", ref.Name, ref.Version)
	}
}

func TestParseRef_RejectsBareName(t *testing.T) {
	_, err := graph.ParseRef("bare-name")
	if !errors.Is(err, graph.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

func TestParseRef_RejectsWhitespace(t *testing.T) {
	_, err := graph.ParseRef("libfoo @1.0.0")
	if !errors.Is(err, graph.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

func TestParseRef_RejectsEmpty(t *testing.T) {
	_, err := graph.ParseRef("")
	if !errors.Is(err, graph.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

func TestParseRef_RejectsTrailingAt(t *testing.T) {
	_, err := graph.ParseRef("libfoo@")
	if !errors.Is(err, graph.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// Pool index + resolve.
// -----------------------------------------------------------------------------

func TestPoolIndex_ResolvePurl(t *testing.T) {
	m := componentsManifest(
		manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
		manifest.Component{Name: "b", Version: strPtr("1")}, // no purl
	)
	p, err := pool.Build([]*manifest.Manifest{m})
	if err != nil {
		t.Fatalf("pool.Build: %v", err)
	}
	idx, err := graph.NewPoolIndex(p)
	if err != nil {
		t.Fatalf("NewPoolIndex: %v", err)
	}
	ref, _ := graph.ParseRef("pkg:generic/a@1")
	i, ok := idx.Resolve(ref)
	if !ok || idx.Components()[i].Name != "a" {
		t.Fatalf("resolve purl: got (%d, %v)", i, ok)
	}
}

func TestPoolIndex_ResolveNameVersion(t *testing.T) {
	m := componentsManifest(
		manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
		manifest.Component{Name: "b", Version: strPtr("2")},
	)
	p, err := pool.Build([]*manifest.Manifest{m})
	if err != nil {
		t.Fatalf("pool.Build: %v", err)
	}
	idx, _ := graph.NewPoolIndex(p)

	ref, _ := graph.ParseRef("b@2")
	i, ok := idx.Resolve(ref)
	if !ok || idx.Components()[i].Name != "b" {
		t.Fatalf("resolve b@2: got (%d, %v)", i, ok)
	}
}

func TestPoolIndex_ResolveMiss(t *testing.T) {
	m := componentsManifest(manifest.Component{Name: "a", Version: strPtr("1")})
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	ref, _ := graph.ParseRef("missing@9")
	if _, ok := idx.Resolve(ref); ok {
		t.Fatal("expected miss")
	}
}

// -----------------------------------------------------------------------------
// §10.4 single-primary semantics.
// -----------------------------------------------------------------------------

func TestPerPrimary_SingleEmptyDependsOnGivesWholePool(t *testing.T) {
	m := componentsManifest(
		manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
		manifest.Component{Name: "b", Version: strPtr("1"), Purl: strPtr("pkg:generic/b@1")},
		manifest.Component{Name: "c", Version: strPtr("1"), Purl: strPtr("pkg:generic/c@1")},
	)
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	r, err := graph.PerPrimary(idx, primary, false)
	if err != nil {
		t.Fatalf("PerPrimary: %v", err)
	}
	if len(r.Components) != 3 {
		t.Fatalf("expected whole pool (3), got %d", len(r.Components))
	}
}

func TestPerPrimary_SingleClosureWithUnreachableWarning(t *testing.T) {
	m := componentsManifest(
		manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
		manifest.Component{Name: "unreached", Version: strPtr("1"), Purl: strPtr("pkg:generic/unreached@1")},
	)
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{Name: "app", Version: strPtr("1"), DependsOn: []string{"pkg:generic/a@1"}}
	var r *graph.Reachability
	warns := captureWarnings(t, func() {
		var err error
		r, err = graph.PerPrimary(idx, primary, false)
		if err != nil {
			t.Fatalf("PerPrimary: %v", err)
		}
	})
	if len(r.Components) != 1 {
		t.Fatalf("closure size: got %d, want 1", len(r.Components))
	}
	if !strings.Contains(warns, "unreached@1") || !strings.Contains(warns, "not reachable") {
		t.Fatalf("expected unreachable warning citing unreached@1, got: %s", warns)
	}
}

func TestPerPrimary_MultiPrimaryRequiresDependsOn(t *testing.T) {
	m := componentsManifest(manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")})
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{Name: "app", Version: strPtr("1")} // empty depends-on
	_, err := graph.PerPrimary(idx, primary, true /* multi */)
	if !errors.Is(err, graph.ErrMultiPrimaryMissingDepsOn) {
		t.Fatalf("expected ErrMultiPrimaryMissingDepsOn, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// Transitive closure + cycle tolerance.
// -----------------------------------------------------------------------------

func TestTransitiveClosure_FollowsDependsOn(t *testing.T) {
	m := componentsManifest(
		manifest.Component{
			Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1"),
			DependsOn: []string{"pkg:generic/b@1"},
		},
		manifest.Component{
			Name: "b", Version: strPtr("1"), Purl: strPtr("pkg:generic/b@1"),
			DependsOn: []string{"pkg:generic/c@1"},
		},
		manifest.Component{Name: "c", Version: strPtr("1"), Purl: strPtr("pkg:generic/c@1")},
		manifest.Component{Name: "orphan", Version: strPtr("1"), Purl: strPtr("pkg:generic/orphan@1")},
	)
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		DependsOn: []string{"pkg:generic/a@1"},
	}
	r, err := graph.PerPrimary(idx, primary, false)
	if err != nil {
		t.Fatalf("PerPrimary: %v", err)
	}
	if len(r.Components) != 3 {
		t.Fatalf("closure size: got %d, want 3 (a, b, c)", len(r.Components))
	}
	if len(r.Unreachable) != 1 || idx.Components()[r.Unreachable[0]].Name != "orphan" {
		t.Fatalf("unreachable: got %v", r.Unreachable)
	}
}

func TestTransitiveClosure_CyclesTerminate(t *testing.T) {
	m := componentsManifest(
		manifest.Component{
			Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1"),
			DependsOn: []string{"pkg:generic/b@1"},
		},
		manifest.Component{
			Name: "b", Version: strPtr("1"), Purl: strPtr("pkg:generic/b@1"),
			DependsOn: []string{"pkg:generic/a@1"}, // cycle back
		},
	)
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		DependsOn: []string{"pkg:generic/a@1"},
	}
	r, err := graph.PerPrimary(idx, primary, false)
	if err != nil {
		t.Fatalf("PerPrimary: %v", err)
	}
	if len(r.Components) != 2 {
		t.Fatalf("expected both nodes reached across cycle, got %d", len(r.Components))
	}
}

func TestTransitiveClosure_UnresolvedEdgeWarnsAndDrops(t *testing.T) {
	m := componentsManifest(
		manifest.Component{
			Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1"),
			DependsOn: []string{"pkg:generic/missing@9"},
		},
	)
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		DependsOn: []string{"pkg:generic/a@1"},
	}
	var r *graph.Reachability
	warns := captureWarnings(t, func() {
		var err error
		r, err = graph.PerPrimary(idx, primary, false)
		if err != nil {
			t.Fatalf("PerPrimary: %v", err)
		}
	})
	if len(r.Components) != 1 {
		t.Fatalf("referring component must survive edge drop: got %d", len(r.Components))
	}
	if !strings.Contains(warns, "unresolved depends-on") || !strings.Contains(warns, "missing@9") {
		t.Fatalf("expected unresolved-edge warning, got: %s", warns)
	}
}

func TestTransitiveClosure_UnresolvedRootAttributedToPrimary(t *testing.T) {
	m := componentsManifest(manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")})
	p, _ := pool.Build([]*manifest.Manifest{m})
	idx, _ := graph.NewPoolIndex(p)

	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		DependsOn: []string{"pkg:generic/nope@1"},
	}
	var r *graph.Reachability
	warns := captureWarnings(t, func() {
		var err error
		r, err = graph.PerPrimary(idx, primary, false)
		if err != nil {
			t.Fatalf("PerPrimary: %v", err)
		}
	})
	if len(r.UnresolvedRoots) != 1 {
		t.Fatalf("expected 1 unresolved root, got %d", len(r.UnresolvedRoots))
	}
	if !strings.Contains(warns, "on primary") {
		t.Fatalf("warning should name primary: %s", warns)
	}
}

// -----------------------------------------------------------------------------
// §10.4 multi-primary + orphan-across-all.
// -----------------------------------------------------------------------------

func TestForProcessingSet_OrphanAcrossAllOncePerRun(t *testing.T) {
	m := componentsManifest(
		manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")},
		manifest.Component{Name: "b", Version: strPtr("1"), Purl: strPtr("pkg:generic/b@1")},
		manifest.Component{Name: "orphan", Version: strPtr("1"), Purl: strPtr("pkg:generic/orphan@1")},
	)
	p, _ := pool.Build([]*manifest.Manifest{m})

	p1 := &manifest.Component{Name: "app1", Version: strPtr("1"), DependsOn: []string{"pkg:generic/a@1"}}
	p2 := &manifest.Component{Name: "app2", Version: strPtr("1"), DependsOn: []string{"pkg:generic/b@1"}}

	var results []*graph.Reachability
	warns := captureWarnings(t, func() {
		var err error
		results, err = graph.ForProcessingSet(p, []*manifest.Component{p1, p2})
		if err != nil {
			t.Fatalf("ForProcessingSet: %v", err)
		}
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 per-primary results, got %d", len(results))
	}
	orphanWarns := strings.Count(warns, "not reachable from any primary")
	if orphanWarns != 1 {
		t.Fatalf("orphan-across-all warning fires %d times, want 1", orphanWarns)
	}
	if !strings.Contains(warns, "orphan@1") {
		t.Fatalf("orphan warning should name orphan@1: %s", warns)
	}
}

func TestForProcessingSet_EmptyPrimariesIsError(t *testing.T) {
	p, _ := pool.Build(nil)
	_, err := graph.ForProcessingSet(p, nil)
	if err == nil {
		t.Fatal("expected error for empty primaries")
	}
}
