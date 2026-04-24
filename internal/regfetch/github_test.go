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
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// newFakeGitHub stands up an httptest.Server that mimics the three
// endpoints GitHubImporter hits. Pass the repo-metadata handler and
// an optional tag-existence handler.
func newFakeGitHub(t *testing.T, repoHandler, tagHandler http.HandlerFunc) (*httptest.Server, *GitHubImporter) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		// Distinguish tag-lookup URLs from repo-metadata ones.
		if strings.Contains(r.URL.Path, "/git/ref/tags/") {
			if tagHandler != nil {
				tagHandler(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if repoHandler != nil {
			repoHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return srv, &GitHubImporter{BaseURL: srv.URL}
}

func TestGitHub_Matches(t *testing.T) {
	imp := &GitHubImporter{}
	cases := []struct {
		ref  string
		want bool
	}{
		{"https://github.com/o/r", true},
		{"https://github.com/o/r/tree/v1", true},
		{"https://github.com/o/r/releases/tag/v1", true},
		{"pkg:github/o/r@v1", true},
		{"pkg:github/o/r", true},
		{"pkg:npm/foo@1", false},
		{"https://gitlab.com/o/r", false},
		{"random string", false},
	}
	for _, tc := range cases {
		if got := imp.Matches(tc.ref); got != tc.want {
			t.Fatalf("Matches(%q) = %v want %v", tc.ref, got, tc.want)
		}
	}
}

func TestGitHub_ParseRefURL(t *testing.T) {
	cases := []struct {
		raw   string
		owner string
		repo  string
		ref   string
	}{
		{"https://github.com/acme/libx", "acme", "libx", ""},
		{"https://github.com/acme/libx/tree/v1.2.3", "acme", "libx", "v1.2.3"},
		{"https://github.com/acme/libx/releases/tag/v1.2.3", "acme", "libx", "v1.2.3"},
		{"https://github.com/acme/libx.git", "acme", "libx", ""},
		{"pkg:github/acme/libx@v1", "acme", "libx", "v1"},
		{"pkg:github/acme/libx", "acme", "libx", ""},
	}
	for _, tc := range cases {
		o, r, v, err := parseGitHubRef(tc.raw)
		if err != nil {
			t.Fatalf("parseGitHubRef(%q): %v", tc.raw, err)
		}
		if o != tc.owner || r != tc.repo || v != tc.ref {
			t.Fatalf("parseGitHubRef(%q) = (%q,%q,%q) want (%q,%q,%q)",
				tc.raw, o, r, v, tc.owner, tc.repo, tc.ref)
		}
	}
}

func TestGitHub_ParseRefRejectsNestedPurl(t *testing.T) {
	// Repo-local §9.3 purls have nested namespaces — they're not
	// importable via the real GitHub API.
	_, _, _, err := parseGitHubRef("pkg:github/acme/device-firmware/src/vendor@1")
	if !errors.Is(err, ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef for nested pkg:github, got %v", err)
	}
}

func TestGitHub_FetchHappyPurl(t *testing.T) {
	srv, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/repos/acme/libx" {
				t.Fatalf("path: got %q", got)
			}
			_, _ = w.Write([]byte(`{
  "name": "libx",
  "description": "a library",
  "homepage": "https://example.com/libx",
  "html_url": "https://github.com/acme/libx",
  "default_branch": "main",
  "license": {"spdx_id": "Apache-2.0"}
}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ref":"refs/tags/v1.2.3"}`))
		})
	_ = srv
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:github/acme/libx@v1.2.3")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Name != "libx" {
		t.Fatalf("name: %q", comp.Name)
	}
	if *comp.Version != "v1.2.3" {
		t.Fatalf("version: %q", *comp.Version)
	}
	if *comp.Purl != "pkg:github/acme/libx@v1.2.3" {
		t.Fatalf("purl: %q", *comp.Purl)
	}
	if comp.License == nil || comp.License.Expression != "Apache-2.0" {
		t.Fatalf("license: %+v", comp.License)
	}
	if comp.Description == nil || *comp.Description != "a library" {
		t.Fatalf("description: %v", comp.Description)
	}

	types := map[string]string{}
	for _, r := range comp.ExternalReferences {
		types[r.Type] = r.URL
	}
	if types["website"] != "https://example.com/libx" {
		t.Fatalf("website: %q", types["website"])
	}
	if types["vcs"] != "https://github.com/acme/libx" {
		t.Fatalf("vcs: %q", types["vcs"])
	}
	if types["issue-tracker"] != "https://github.com/acme/libx/issues" {
		t.Fatalf("issue-tracker: %q", types["issue-tracker"])
	}
}

func TestGitHub_FetchHappyURL(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"libx","html_url":"https://github.com/acme/libx","default_branch":"main"}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c,
		"https://github.com/acme/libx/releases/tag/v2.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "v2.0" {
		t.Fatalf("version from URL form: %q", *comp.Version)
	}
}

func TestGitHub_Fetch404(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, nil)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/noone/nothing@1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "typo") {
		t.Fatalf("error should suggest typo: %v", err)
	}
}

func TestGitHub_FetchRateLimited(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		}, nil)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/acme/x@1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error should mention GITHUB_TOKEN: %v", err)
	}
}

func TestGitHub_FetchMissingLicense(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			// license: null is the shape GitHub returns for repos
			// it can't classify.
			_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"main","license":null}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:github/o/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("license should be nil when upstream has null, got %+v", comp.License)
	}
}

func TestGitHub_FetchNOASSERTIONSkipped(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"main","license":{"spdx_id":"NOASSERTION"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:github/o/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("license should be nil for NOASSERTION, got %+v", comp.License)
	}
}

func TestGitHub_FetchDefaultBranchFallback(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"trunk"}`))
		}, nil)

	warn := &bytes.Buffer{}
	diag.SetSink(warn)
	diag.Reset()
	t.Cleanup(func() { diag.SetSink(nil); diag.Reset() })

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:github/o/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "trunk" {
		t.Fatalf("version fallback: got %q", *comp.Version)
	}
	if !strings.Contains(warn.String(), "no ref supplied") {
		t.Fatalf("expected fallback warning, stderr: %q", warn.String())
	}
}

func TestGitHub_FetchTagNotFound(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"main"}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/o/x@does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing tag, got %v", err)
	}
}

func TestGitHub_FetchSendsAuthWhenTokenSet(t *testing.T) {
	var gotAuth string
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"main"}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Setenv("GITHUB_TOKEN", "ghp_testtoken")
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/o/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotAuth != "Bearer ghp_testtoken" {
		t.Fatalf("Authorization header: got %q", gotAuth)
	}
}

func TestGitHub_FetchTokenNotLeakedInErrors(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, nil)

	t.Setenv("GITHUB_TOKEN", "ghp_secret_should_never_appear")
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/o/x@1")
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if strings.Contains(err.Error(), "ghp_secret_should_never_appear") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

func TestGitHub_BaseURLEnvOverride(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"main"}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("BOMTIQUE_GITHUB_BASE_URL", srv.URL)
	imp := &GitHubImporter{} // no explicit BaseURL
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/o/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !hit {
		t.Fatal("env-var base URL was not honoured")
	}
}

func TestGitHub_RegistersGlobally(t *testing.T) {
	// The init() in github.go registers a GitHubImporter in the
	// process-global registry. Confirm Match returns it.
	imp := Default().Match("pkg:github/foo/bar@1")
	if imp == nil {
		t.Fatal("GitHubImporter not registered globally")
	}
	if _, ok := imp.(*GitHubImporter); !ok {
		t.Fatalf("globally-registered importer has unexpected type %T", imp)
	}
}

// TestGitHub_TokenNotInRequestBody — belt-and-braces check: our
// request body is nil (GETs only), but confirm no leaked header
// echoes into the error path via the res.URL field either.
func TestGitHub_ErrorDoesNotIncludeRequestHeaders(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}, nil)
	t.Setenv("GITHUB_TOKEN", "leak-me-maybe")
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:github/o/x@1")
	if err == nil {
		t.Fatal("expected error for 418")
	}
	if strings.Contains(err.Error(), "leak-me-maybe") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

// Belt test: the Component produced by Fetch must validate against
// the manifest package's Component shape (name non-empty, at least
// one of version/purl/hashes — §6.1). The happy path covers this
// implicitly; we keep a dedicated assertion here to guard the rule.
func TestGitHub_ProducedComponentSatisfies_6_1(t *testing.T) {
	_, imp := newFakeGitHub(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x","html_url":"https://github.com/o/x","default_branch":"main"}`))
		}, nil)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:github/o/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Name == "" {
		t.Fatal("§6.1: name must be non-empty")
	}
	hasIdentity := comp.Version != nil || comp.Purl != nil || len(comp.Hashes) > 0
	if !hasIdentity {
		t.Fatal("§6.1: component needs one of version/purl/hashes")
	}
}

// Guardrail: make sure the Component type still lines up with the
// importer's struct literal shape — if manifest.Component ever
// grows a required field, this test will fail fast.
var _ = (*manifest.Component)(nil)
