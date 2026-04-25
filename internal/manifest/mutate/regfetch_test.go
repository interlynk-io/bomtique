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

func registryWith(imp regfetch.Importer) *regfetch.Registry {
	r := regfetch.NewRegistry()
	r.Register(imp)
	return r
}

func TestAdd_Ref_PurlFetches(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := registryWith(&fakePoolImporter{})

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "ignored", Version: "1.0",
		Ref:      "pkg:fake/thing@1.0",
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

func TestAdd_Ref_EmptyDoesNothing(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			t.Fatal("Fetch called when --ref was empty")
			return nil, nil
		},
	})

	res, err := Add(AddOptions{
		FromDir: dir,
		Name:    "local", Version: "1.0",
		Purl: "pkg:fake/thing@1.0", // literal purl, no fetch
		// Ref intentionally empty
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	// No license because no fetch happened and no --license flag.
	if c.License != nil {
		t.Fatalf("empty --ref leaked fetched data: %+v", c.License)
	}
}

func TestAdd_Ref_BomtiqueOfflineEnvSkipsFetch(t *testing.T) {
	t.Setenv("BOMTIQUE_OFFLINE", "1")
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			t.Fatal("Fetch called under BOMTIQUE_OFFLINE=1")
			return nil, nil
		},
	})

	res, err := Add(AddOptions{
		FromDir:  dir,
		Name:     "local",
		Version:  "1.0",
		Ref:      "pkg:fake/thing@1.0",
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	// No license because BOMTIQUE_OFFLINE skipped the fetch.
	if c.License != nil {
		t.Fatalf("BOMTIQUE_OFFLINE leaked fetched data: %+v", c.License)
	}
}

func TestAdd_Ref_NoMatchErrors(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := regfetch.NewRegistry() // empty

	_, err := Add(AddOptions{
		FromDir: dir,
		Name:    "x", Version: "1",
		Ref:      "pkg:unknown/x@1",
		Registry: r,
	})
	if !errors.Is(err, regfetch.ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef, got %v", err)
	}
}

func TestAdd_Ref_URLForm(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	captured := ""
	imp := &urlImporter{
		FetchFn: func(ref string) (*manifest.Component, error) {
			captured = ref
			return &manifest.Component{
				Name:    "from-url",
				Version: strPtr("1"),
				Purl:    strPtr("pkg:fake/from-url@1"),
			}, nil
		},
	}
	r := registryWith(imp)

	_, err := Add(AddOptions{
		FromDir:  dir,
		Name:     "real-name",
		Version:  "1",
		Ref:      "https://example.com/foo/v1",
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if captured != "https://example.com/foo/v1" {
		t.Fatalf("URL ref not passed through: got %q", captured)
	}
}

// urlImporter matches https:// refs, used to confirm URL form works.
type urlImporter struct {
	FetchFn func(ref string) (*manifest.Component, error)
}

func (u *urlImporter) Name() string            { return "fake-url" }
func (u *urlImporter) Matches(ref string) bool { return strings.HasPrefix(ref, "https://") }
func (u *urlImporter) Fetch(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
	return u.FetchFn(ref)
}

func TestAdd_Ref_NameAsURLNoLongerTriggersFetch(t *testing.T) {
	// Regression guard: passing a URL via --name (the old footgun)
	// must not be interpreted as an importer ref.
	dir := t.TempDir()
	seedPrimary(t, dir)
	r := registryWith(&urlImporter{
		FetchFn: func(ref string) (*manifest.Component, error) {
			t.Fatalf("URL-shaped --name must not trigger fetch: ref=%q", ref)
			return nil, nil
		},
	})

	res, err := Add(AddOptions{
		FromDir:  dir,
		Name:     "https://example.com/foo",
		Version:  "1",
		Registry: r,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	if cm.Components[0].Name != "https://example.com/foo" {
		t.Fatalf("--name stored verbatim: got %q", cm.Components[0].Name)
	}
}

func TestAdd_Ref_VendoredAtNoFetchByDefault(t *testing.T) {
	// vendored-at synthesis is independent of regfetch; with no --ref
	// the importer must never fire and the synthesis must still happen.
	dir := t.TempDir()
	seedPrimaryWithPurl(t, dir, "pkg:github/acme/repo@1")
	mkVendorDir(t, dir, "src/vendor")

	res, err := Add(AddOptions{
		FromDir:         dir,
		Name:            "v",
		Version:         "1",
		VendoredAt:      "./src/vendor",
		UpstreamName:    "u",
		UpstreamVersion: "1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	cm, _ := parseComponentsFile(res.Path)
	c := cm.Components[0]
	if c.Purl == nil || !strings.Contains(*c.Purl, "src/vendor") {
		t.Fatalf("derived purl missing: %+v", c.Purl)
	}
}

func TestUpdate_Refresh_FetchesFromImporter(t *testing.T) {
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
		Refresh:  true,
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

func TestUpdate_Refresh_FlagOverridesFetched(t *testing.T) {
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
		Refresh:     true,
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

func TestUpdate_Refresh_RequiresPurl(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1")}, // no purl
	})
	_, err := Update(UpdateOptions{
		FromDir: dir,
		Ref:     "x@1",
		Refresh: true,
	})
	if !errors.Is(err, regfetch.ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef, got %v", err)
	}
}

func TestUpdate_Refresh_DefaultSkipsFetch(t *testing.T) {
	dir := t.TempDir()
	seedPrimary(t, dir)
	seedPoolWith(t, filepath.Join(dir, ".components.json"), []manifest.Component{
		{Name: "x", Version: strPtr("1"), Purl: strPtr("pkg:fake/x@1")},
	})
	r := registryWith(&fakePoolImporter{
		FetchFn: func(ctx context.Context, c *regfetch.Client, ref string) (*manifest.Component, error) {
			t.Fatal("Fetch called without --refresh")
			return nil, nil
		},
	})
	// No Refresh. The fake importer must NOT fire.
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
