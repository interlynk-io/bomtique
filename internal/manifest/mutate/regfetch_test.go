// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/regfetch"
)

// fakePoolImporter is a local test importer that matches "pkg:fake/"
// refs and produces a canned Component.
type fakePoolImporter struct {
	FetchFn func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error)
}

func (f *fakePoolImporter) Name() string            { return "fake" }
func (f *fakePoolImporter) Matches(ref string) bool { return strings.HasPrefix(ref, "pkg:fake/") }
func (f *fakePoolImporter) Fetch(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
	if f.FetchFn != nil {
		return f.FetchFn(ctx, c, ref)
	}
	return &manifest.Component{
		Name:    "fetched-name",
		Version: strPtr("9.9.9"),
		Purl:    strPtr(ref),
		License: &manifest.License{Expression: "MIT"},
	}, nil
}

func registryWith(imp *fakePoolImporter) *regfetch.Registry {
	r := regfetch.NewRegistry()
	r.Register(imp)
	return r
}

func TestAdd_Regfetch_DefaultAutoFetches(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := registryWith(&fakePoolImporter{})

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "ignored", Version: "1.0",
		Purl:     "pkg:fake/thing@1.0",
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	// Flag-layer name wins over the fetched "fetched-name".
	if c.Name != "ignored" {
		t.Fatalf("flag should override fetched name: got %q", c.Name)
	}
	// Fetched license lands (no --license flag to override it).
	if c.License == nil || c.License.Expression != "MIT" {
		t.Fatalf("fetched license missing: %+v", c.License)
	}
}

func TestAdd_Regfetch_OfflineSkipsFetch(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			t.Fatal("Fetch called under --offline")
			return nil, nil
		},
	})

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "local", Version: "1.0",
		Purl:     "pkg:fake/thing@1.0",
		Offline:  true,
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	// No license because no fetch happened and no --license flag.
	if c.License != nil {
		t.Fatalf("--offline leaked fetched data: %+v", c.License)
	}
}

func TestAdd_Regfetch_OnlineRequiresMatch(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := regfetch.NewRegistry() // empty

	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "x", Version: "1", Purl: "pkg:unknown/x@1",
		Online:   true,
		Registry: r,
	})
	if !errors.Is(err, regfetch.ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef, got %v", err)
	}
}

func TestAdd_Regfetch_OnlineWithoutPurlOrURL(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "x", Version: "1",
		Online: true,
	})
	if !errors.Is(err, regfetch.ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef for --online without ref, got %v", err)
	}
}

func TestAdd_Regfetch_OfflineOnlineMutuallyExclusive(t *testing.T) {
	_, err := Add(AddOptions{Offline: true, Online: true})
	if err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
}

func TestAdd_Regfetch_URLShapedName(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	imp := &fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			if !strings.HasPrefix(ref, "https://") {
				t.Fatalf("importer got non-URL ref: %q", ref)
			}
			return &manifest.Component{
				Name:    "from-url",
				Version: strPtr("1"),
				Purl:    strPtr("pkg:fake/from-url@1"),
			}, nil
		},
	}
	// Override Matches so the URL form is accepted.
	imp2 := &urlImporter{inner: imp}
	r := regfetch.NewRegistry()
	r.Register(imp2)

	_, err := Add(AddOptions{
		FromDir:  dir,
		Name:     "https://example.com/foo",
		Version:  "1",
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
}

// urlImporter wraps fakePoolImporter to match on https:// prefix
// instead of pkg:fake/.
type urlImporter struct {
	inner *fakePoolImporter
}

func (u *urlImporter) Name() string            { return "fake-url" }
func (u *urlImporter) Matches(ref string) bool { return strings.HasPrefix(ref, "https://") }
func (u *urlImporter) Fetch(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
	return u.inner.Fetch(ctx, c, ref)
}

func TestAdd_Regfetch_VendoredAtBypassedByOffline(t *testing.T) {
	// Confirm that --offline on a vendored-at add still does the
	// synthesis (hash directive + ancestor) — regfetch is unrelated.
	dir := t.TempDir()
	// github primary so derivation works.
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	mkVendorDir(t, dir, "src/vendor")

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "v", Version: "1",
		VendoredAt:      "./src/vendor",
		UpstreamName:    "u",
		UpstreamVersion: "1",
		Offline:         true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	if c.Purl == nil || !strings.Contains(*c.Purl, "src/vendor") {
		t.Fatalf("derived purl missing under --offline: %+v", c.Purl)
	}
}

func TestUpdate_Regfetch_OfflineOnlineMutuallyExclusive(t *testing.T) {
	_, err := Update(UpdateOptions{Offline: true, Online: true})
	if err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
}

func TestUpdate_Regfetch_OnlineRefreshesFromImporter(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:fake/x@1")},
	})
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			return &manifest.Component{
				Name:        "x",
				Version:     strPtr("1"),
				Purl:        strPtr("pkg:fake/x@1"),
				Description: strPtr("refreshed description"),
			}, nil
		},
	})

	res, err := Update(UpdateOptions{
		FromDir:  dir,
		Ref:      "pkg:fake/x@1",
		Online:   true,
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !containsStr(res.FieldsChanged, "regfetch") {
		t.Fatalf("FieldsChanged should include regfetch: %v", res.FieldsChanged)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	d := cm.Components[0].Description
	if d == nil || *d != "refreshed description" {
		t.Fatalf("description not refreshed: %+v", d)
	}
}

func TestUpdate_Regfetch_OnlineRefreshWithFlagOverride(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:fake/x@1")},
	})
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			return &manifest.Component{
				Name:        "x",
				Version:     strPtr("1"),
				Purl:        strPtr("pkg:fake/x@1"),
				Description: strPtr("from registry"),
			}, nil
		},
	})
	// Flag-supplied description must win.
	_, err := Update(UpdateOptions{
		FromDir:     dir,
		Ref:         "pkg:fake/x@1",
		Online:      true,
		Registry:    r,
		Description: "from flag",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	cm, _ := parseComponentsFile(filepath.Join(dir, ".components.json"))
	d := cm.Components[0].Description
	if d == nil || *d != "from flag" {
		t.Fatalf("flag should win: %+v", d)
	}
}

func TestUpdate_Regfetch_OnlineRequiresPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1")}, // no purl
	})
	_, err := Update(UpdateOptions{
		FromDir: dir,
		Ref:     "x@1",
		Online:  true,
	})
	if !errors.Is(err, regfetch.ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef, got %v", err)
	}
}

func TestUpdate_Regfetch_DefaultSkipsFetch(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:fake/x@1")},
	})
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			t.Fatal("Fetch called under default mode (expected skip)")
			return nil, nil
		},
	})
	// No --online. The fake importer must NOT fire.
	_, err := Update(UpdateOptions{
		FromDir:     dir,
		Ref:         "pkg:fake/x@1",
		Description: "flag only",
		Registry:    r,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
}
