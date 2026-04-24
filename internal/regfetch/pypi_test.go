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

func newFakePyPI(t *testing.T, body string, status int) (*httptest.Server, *PyPIImporter) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &PyPIImporter{BaseURL: srv.URL}
}

func TestPyPI_Matches(t *testing.T) {
	imp := &PyPIImporter{}
	cases := []struct {
		ref  string
		want bool
	}{
		{"pkg:pypi/requests@2.31.0", true},
		{"pypi:requests@2.31.0", true},
		{"pypi:requests", true},
		{"https://pypi.org/project/requests", true},
		{"https://pypi.org/project/requests/2.31.0", true},
		{"pkg:npm/foo@1", false},
		{"random", false},
	}
	for _, tc := range cases {
		if got := imp.Matches(tc.ref); got != tc.want {
			t.Fatalf("Matches(%q) = %v want %v", tc.ref, got, tc.want)
		}
	}
}

func TestPyPI_ParseRef(t *testing.T) {
	cases := []struct {
		raw     string
		name    string
		version string
	}{
		{"pkg:pypi/requests@2.31.0", "requests", "2.31.0"},
		{"pkg:pypi/requests", "requests", ""},
		{"pypi:requests@2.31.0", "requests", "2.31.0"},
		{"pypi:requests", "requests", ""},
		{"https://pypi.org/project/requests", "requests", ""},
		{"https://pypi.org/project/requests/2.31.0", "requests", "2.31.0"},
		{"https://pypi.org/project/requests/2.31.0/", "requests", "2.31.0"},
	}
	for _, tc := range cases {
		n, v, err := parsePyPIRef(tc.raw)
		if err != nil {
			t.Fatalf("parsePyPIRef(%q): %v", tc.raw, err)
		}
		if n != tc.name || v != tc.version {
			t.Fatalf("parsePyPIRef(%q) = (%q,%q) want (%q,%q)",
				tc.raw, n, v, tc.name, tc.version)
		}
	}
}

func TestPyPI_NormalisePEP503(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Flask_SQLAlchemy", "flask-sqlalchemy"},
		{"Flask.SQL_Alchemy", "flask-sql-alchemy"},
		{"FOO", "foo"},
		{"foo__bar--baz..qux", "foo-bar-baz-qux"},
		{"already-kebab", "already-kebab"},
		{"", ""},
		{"  spaced  ", "spaced"},
	}
	for _, tc := range cases {
		got := normalisePEP503(tc.in)
		if got != tc.want {
			t.Fatalf("normalisePEP503(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestPyPI_FetchHappyPinned(t *testing.T) {
	body := `{
  "info": {
    "name": "requests",
    "version": "2.31.0",
    "summary": "Python HTTP for Humans.",
    "home_page": "https://requests.readthedocs.io",
    "author": "Kenneth Reitz",
    "author_email": "me@kennethreitz.org",
    "license": "Apache 2.0",
    "project_urls": {
      "Source": "https://github.com/psf/requests",
      "Documentation": "https://requests.readthedocs.io",
      "Bug Tracker": "https://github.com/psf/requests/issues"
    },
    "classifiers": ["License :: OSI Approved :: Apache Software License"]
  },
  "urls": [
    {"packagetype": "sdist", "digests": {"sha256": "aaaa"}},
    {"packagetype": "bdist_wheel", "digests": {"sha256": "bbbb"}}
  ]
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/Requests@2.31.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Name != "requests" {
		t.Fatalf("name (PEP 503 normalised): %q", comp.Name)
	}
	if *comp.Version != "2.31.0" {
		t.Fatalf("version: %q", *comp.Version)
	}
	if *comp.Purl != "pkg:pypi/requests@2.31.0" {
		t.Fatalf("purl: %q", *comp.Purl)
	}
	if comp.License == nil || comp.License.Expression != "Apache-2.0" {
		t.Fatalf("license: %+v", comp.License)
	}
	if comp.Supplier == nil || comp.Supplier.Name != "Kenneth Reitz" {
		t.Fatalf("supplier: %+v", comp.Supplier)
	}
	if *comp.Supplier.Email != "me@kennethreitz.org" {
		t.Fatalf("supplier.email: %v", comp.Supplier.Email)
	}
	if comp.Description == nil || *comp.Description != "Python HTTP for Humans." {
		t.Fatalf("description: %v", comp.Description)
	}

	types := map[string]string{}
	for _, r := range comp.ExternalReferences {
		types[r.Type] = r.URL
	}
	if types["website"] != "https://requests.readthedocs.io" {
		t.Fatalf("website: %q", types["website"])
	}
	if types["vcs"] != "https://github.com/psf/requests" {
		t.Fatalf("vcs: %q", types["vcs"])
	}
	if types["issue-tracker"] != "https://github.com/psf/requests/issues" {
		t.Fatalf("issue-tracker: %q", types["issue-tracker"])
	}
	if types["documentation"] != "https://requests.readthedocs.io" {
		t.Fatalf("documentation: %q", types["documentation"])
	}

	if len(comp.Hashes) != 1 {
		t.Fatalf("hashes: %+v", comp.Hashes)
	}
	if comp.Hashes[0].Algorithm != "SHA-256" {
		t.Fatalf("hash algorithm: %q", comp.Hashes[0].Algorithm)
	}
	// sdist wins over wheel.
	if *comp.Hashes[0].Value != "aaaa" {
		t.Fatalf("hash value: %q (expected sdist 'aaaa')", *comp.Hashes[0].Value)
	}
}

func TestPyPI_FetchLatestTagFallback(t *testing.T) {
	body := `{
  "info": {
    "name": "requests",
    "version": "2.31.0",
    "summary": "latest",
    "license": "MIT"
  },
  "urls": []
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/requests")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "2.31.0" {
		t.Fatalf("expected info.version as latest fallback, got %q", *comp.Version)
	}
	if comp.License == nil || comp.License.Expression != "MIT" {
		t.Fatalf("license: %+v", comp.License)
	}
}

func TestPyPI_Fetch404(t *testing.T) {
	_, imp := newFakePyPI(t, `{"message":"not found"}`, http.StatusNotFound)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:pypi/nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPyPI_FetchLicenseFromClassifier(t *testing.T) {
	// info.license is an unrecognised free-text string; classifier
	// should drive the result.
	body := `{
  "info": {
    "name": "foo",
    "version": "1.0",
    "license": "see the LICENSE file",
    "classifiers": ["License :: OSI Approved :: MIT License"]
  },
  "urls": []
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/foo@1.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License == nil || comp.License.Expression != "MIT" {
		t.Fatalf("license should come from classifier: %+v", comp.License)
	}
}

func TestPyPI_FetchLicenseUnknownWarns(t *testing.T) {
	warn := &bytes.Buffer{}
	diag.SetSink(warn)
	diag.Reset()
	t.Cleanup(func() { diag.SetSink(nil); diag.Reset() })

	body := `{
  "info": {
    "name": "foo",
    "version": "1.0",
    "license": "Custom proprietary garbage"
  },
  "urls": []
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/foo@1.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("license should be dropped, got %+v", comp.License)
	}
	if !strings.Contains(warn.String(), "not a recognised SPDX expression") {
		t.Fatalf("expected warning, stderr: %q", warn.String())
	}
}

func TestPyPI_FetchLicenseFromLicenseExpression(t *testing.T) {
	// PyPI's newer metadata includes `license_expression` (an SPDX
	// string). It should win over info.license.
	body := `{
  "info": {
    "name": "foo",
    "version": "1.0",
    "license": "ignored",
    "license_expression": "Apache-2.0 OR MIT"
  },
  "urls": []
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/foo@1.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License == nil || comp.License.Expression != "Apache-2.0 OR MIT" {
		t.Fatalf("license_expression should win: %+v", comp.License)
	}
}

func TestPyPI_FetchWheelOnlyDigest(t *testing.T) {
	body := `{
  "info": {"name":"x","version":"1","license":"MIT"},
  "urls": [{"packagetype":"bdist_wheel","digests":{"sha256":"deadbeef"}}]
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(comp.Hashes) != 1 || *comp.Hashes[0].Value != "deadbeef" {
		t.Fatalf("wheel digest fallback failed: %+v", comp.Hashes)
	}
}

func TestPyPI_FetchNoDigestNoHash(t *testing.T) {
	body := `{"info":{"name":"x","version":"1"},"urls":[]}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(comp.Hashes) != 0 {
		t.Fatalf("no URLs → no hashes, got %+v", comp.Hashes)
	}
}

func TestPyPI_FetchMaintainerFallback(t *testing.T) {
	body := `{
  "info": {
    "name":"x","version":"1",
    "maintainer":"Bob","maintainer_email":"bob@example.com"
  },
  "urls": []
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Supplier == nil || comp.Supplier.Name != "Bob" {
		t.Fatalf("expected maintainer fallback, got %+v", comp.Supplier)
	}
}

func TestPyPI_FetchProjectURLsCaseInsensitive(t *testing.T) {
	body := `{
  "info": {
    "name":"x","version":"1",
    "project_urls": {"source": "https://example.com/src"}
  },
  "urls": []
}`
	_, imp := newFakePyPI(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:pypi/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var vcs string
	for _, r := range comp.ExternalReferences {
		if r.Type == "vcs" {
			vcs = r.URL
		}
	}
	if vcs != "https://example.com/src" {
		t.Fatalf("lowercase 'source' not matched: %q", vcs)
	}
}

func TestPyPI_BaseURLEnvOverride(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{"info":{"name":"x","version":"1"},"urls":[]}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("BOMTIQUE_PYPI_BASE_URL", srv.URL)
	imp := &PyPIImporter{}
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:pypi/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !hit {
		t.Fatal("env-var base URL not honoured")
	}
}

func TestPyPI_RegistersGlobally(t *testing.T) {
	imp := Default().Match("pkg:pypi/foo")
	if imp == nil {
		t.Fatal("PyPIImporter not registered globally")
	}
	if _, ok := imp.(*PyPIImporter); !ok {
		t.Fatalf("unexpected importer type %T", imp)
	}
}

func TestPyPI_MapLicenseText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"MIT", "MIT"},
		{"MIT License", "MIT"},
		{"Apache 2.0", "Apache-2.0"},
		{"Apache Software License", "Apache-2.0"},
		{"BSD", "BSD-3-Clause"},
		{"Simplified BSD", "BSD-2-Clause"},
		{"random garbage", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := mapPyPILicenseText(tc.in)
		if got != tc.want {
			t.Fatalf("mapPyPILicenseText(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestPyPI_FetchRateLimited(t *testing.T) {
	_, imp := newFakePyPI(t, `{}`, http.StatusTooManyRequests)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:pypi/x@1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on 429, got %v", err)
	}
}
