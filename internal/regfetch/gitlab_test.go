// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
)

// newFakeGitLab stands up an httptest.Server that mimics the two
// endpoints GitLabImporter hits.
func newFakeGitLab(t *testing.T, projectHandler, tagHandler http.HandlerFunc) (*httptest.Server, *GitLabImporter) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/repository/tags/") {
			if tagHandler != nil {
				tagHandler(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if projectHandler != nil {
			projectHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return srv, &GitLabImporter{BaseURL: srv.URL}
}

func TestGitLab_Matches(t *testing.T) {
	imp := &GitLabImporter{}
	cases := []struct {
		ref  string
		want bool
	}{
		{"https://gitlab.com/group/project", true},
		{"https://gitlab.com/group/sub/project/-/tree/v1", true},
		{"pkg:gitlab/group/project@v1", true},
		{"pkg:gitlab/group/sub/project@v1", true},
		{"pkg:github/foo/bar", false},
		{"https://github.com/foo/bar", false},
		{"random", false},
	}
	for _, tc := range cases {
		if got := imp.Matches(tc.ref); got != tc.want {
			t.Fatalf("Matches(%q) = %v want %v", tc.ref, got, tc.want)
		}
	}
}

func TestGitLab_ParseRef(t *testing.T) {
	cases := []struct {
		raw  string
		path string
		ref  string
	}{
		{"https://gitlab.com/acme/libx", "acme/libx", ""},
		{"https://gitlab.com/acme/libx/-/tree/v1", "acme/libx", "v1"},
		{"https://gitlab.com/acme/libx/-/tags/v1.2.3", "acme/libx", "v1.2.3"},
		{"https://gitlab.com/acme/sub/libx/-/tree/main", "acme/sub/libx", "main"},
		{"https://gitlab.com/acme/libx.git", "acme/libx", ""},
		{"pkg:gitlab/acme/libx@v1", "acme/libx", "v1"},
		{"pkg:gitlab/acme/sub/libx@v1", "acme/sub/libx", "v1"},
		{"pkg:gitlab/acme/libx", "acme/libx", ""},
	}
	for _, tc := range cases {
		got, ref, err := parseGitLabRef(tc.raw)
		if err != nil {
			t.Fatalf("parseGitLabRef(%q): %v", tc.raw, err)
		}
		if got != tc.path || ref != tc.ref {
			t.Fatalf("parseGitLabRef(%q) = (%q,%q) want (%q,%q)", tc.raw, got, ref, tc.path, tc.ref)
		}
	}
}

func TestGitLab_ParseRefRejectsSingleSegment(t *testing.T) {
	_, _, err := parseGitLabRef("https://gitlab.com/only-one")
	if !errors.Is(err, ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef, got %v", err)
	}
}

func TestGitLab_FetchHappyPurl(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v4/projects/") {
				t.Fatalf("path: %q", r.URL.Path)
			}
			// Ensure project path is URL-encoded with %2F. r.URL.Path
			// decodes percent-encoding; r.RequestURI preserves the
			// raw wire form.
			if !strings.Contains(r.RequestURI, "acme%2Flibx") {
				t.Fatalf("expected URL-encoded slashes in path, got %q", r.RequestURI)
			}
			_, _ = w.Write([]byte(`{
  "name": "libx",
  "description": "a library",
  "web_url": "https://gitlab.com/acme/libx",
  "http_url_to_repo": "https://gitlab.com/acme/libx.git",
  "default_branch": "main",
  "license": {"key":"mit","name":"MIT License","nickname":null}
}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:gitlab/acme/libx@v1.2.3")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Name != "libx" {
		t.Fatalf("name: %q", comp.Name)
	}
	if *comp.Version != "v1.2.3" {
		t.Fatalf("version: %q", *comp.Version)
	}
	if *comp.Purl != "pkg:gitlab/acme/libx@v1.2.3" {
		t.Fatalf("purl: %q", *comp.Purl)
	}
	if comp.License == nil || comp.License.Expression != "MIT" {
		t.Fatalf("license: %+v", comp.License)
	}
	types := map[string]string{}
	for _, r := range comp.ExternalReferences {
		types[r.Type] = r.URL
	}
	if types["vcs"] != "https://gitlab.com/acme/libx" {
		t.Fatalf("vcs: %q", types["vcs"])
	}
	if types["issue-tracker"] != "https://gitlab.com/acme/libx/-/issues" {
		t.Fatalf("issue-tracker: %q", types["issue-tracker"])
	}
	if types["distribution"] != "https://gitlab.com/acme/libx.git" {
		t.Fatalf("distribution: %q", types["distribution"])
	}
}

func TestGitLab_FetchNestedNamespace(t *testing.T) {
	var gotURI string
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			gotURI = r.RequestURI
			_, _ = w.Write([]byte(`{"name":"proj","web_url":"https://gitlab.com/g/s/proj","default_branch":"main"}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/g/s/proj@v1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(gotURI, "g%2Fs%2Fproj") {
		t.Fatalf("expected encoded nested path in request, got %q", gotURI)
	}
}

func TestGitLab_FetchHappyURL(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"libx","web_url":"https://gitlab.com/acme/libx","default_branch":"main"}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c,
		"https://gitlab.com/acme/libx/-/tree/v2.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "v2.0" {
		t.Fatalf("version from URL form: %q", *comp.Version)
	}
}

func TestGitLab_Fetch404(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
		nil)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/no/such@1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "typo") {
		t.Fatalf("error should suggest typo: %v", err)
	}
}

func TestGitLab_FetchRateLimited(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		}, nil)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/acme/x@1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("error should mention GITLAB_TOKEN: %v", err)
	}
}

func TestGitLab_Fetch429(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}, nil)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/acme/x@1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on 429, got %v", err)
	}
}

func TestGitLab_FetchMissingLicense(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","web_url":"https://gitlab.com/o/x","default_branch":"main","license":null}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("license should be nil when null, got %+v", comp.License)
	}
}

func TestGitLab_FetchUnknownLicenseKey(t *testing.T) {
	warn := &bytes.Buffer{}
	diag.SetSink(warn)
	diag.Reset()
	t.Cleanup(func() { diag.SetSink(nil); diag.Reset() })

	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","web_url":"https://gitlab.com/o/x","default_branch":"main","license":{"key":"some-weird-key"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("unknown license key should be dropped, got %+v", comp.License)
	}
	if !strings.Contains(warn.String(), "not a known SPDX expression") {
		t.Fatalf("expected license warning, stderr: %q", warn.String())
	}
}

func TestGitLab_FetchDefaultBranchFallback(t *testing.T) {
	warn := &bytes.Buffer{}
	diag.SetSink(warn)
	diag.Reset()
	t.Cleanup(func() { diag.SetSink(nil); diag.Reset() })

	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","web_url":"https://gitlab.com/o/x","default_branch":"trunk"}`))
		}, nil)

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "trunk" {
		t.Fatalf("version fallback: %q", *comp.Version)
	}
	if !strings.Contains(warn.String(), "no ref supplied") {
		t.Fatalf("expected pin warning, stderr: %q", warn.String())
	}
}

func TestGitLab_FetchTagNotFound(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","web_url":"https://gitlab.com/o/x","default_branch":"main"}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })

	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x@ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing tag, got %v", err)
	}
}

func TestGitLab_FetchSendsPrivateToken(t *testing.T) {
	var gotToken string
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) {
			gotToken = r.Header.Get("PRIVATE-TOKEN")
			_, _ = w.Write([]byte(`{"name":"x","web_url":"https://gitlab.com/o/x","default_branch":"main"}`))
		}, nil)

	t.Setenv("GITLAB_TOKEN", "glpat-testvalue")
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotToken != "glpat-testvalue" {
		t.Fatalf("PRIVATE-TOKEN header: got %q", gotToken)
	}
}

func TestGitLab_FetchTokenNotLeakedInErrors(t *testing.T) {
	_, imp := newFakeGitLab(t,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
		nil)
	t.Setenv("GITLAB_TOKEN", "glpat-secret-never-show")
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x@1")
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if strings.Contains(err.Error(), "glpat-secret-never-show") {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestGitLab_BaseURLEnvOverride(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{"name":"x","web_url":"https://self-hosted/o/x","default_branch":"main"}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("BOMTIQUE_GITLAB_BASE_URL", srv.URL)
	imp := &GitLabImporter{}
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:gitlab/o/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !hit {
		t.Fatal("env-var base URL not honoured")
	}
}

func TestGitLab_RegistersGlobally(t *testing.T) {
	imp := Default().Match("pkg:gitlab/foo/bar@1")
	if imp == nil {
		t.Fatal("GitLabImporter not registered globally")
	}
	if _, ok := imp.(*GitLabImporter); !ok {
		t.Fatalf("unexpected importer type %T", imp)
	}
}

func TestGitLab_LicenseKeyMapping(t *testing.T) {
	cases := []struct{ key, want string }{
		{"apache-2.0", "Apache-2.0"},
		{"MIT", "MIT"},
		{"bsd-3-clause", "BSD-3-Clause"},
		{"gpl-3.0", "GPL-3.0-only"},
		{"mpl-2.0", "MPL-2.0"},
		{"unknown-thing", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := mapGitLabLicenseKey(tc.key)
		if got != tc.want {
			t.Fatalf("mapGitLabLicenseKey(%q) = %q want %q", tc.key, got, tc.want)
		}
	}
}
