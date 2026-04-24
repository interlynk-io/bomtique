// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
)

// newFakeNpm stands up an httptest.Server that serves one canned
// body for every request. Test callers build an NpmImporter pointed
// at the server.
func newFakeNpm(t *testing.T, body string, status int) (*httptest.Server, *NpmImporter) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &NpmImporter{BaseURL: srv.URL}
}

func TestNpm_Matches(t *testing.T) {
	imp := &NpmImporter{}
	cases := []struct {
		ref  string
		want bool
	}{
		{"pkg:npm/express@4.17.1", true},
		{"pkg:npm/%40types/node@1.2.3", true},
		{"npm:express@4.17.1", true},
		{"npm:@types/node@1.2.3", true},
		{"https://www.npmjs.com/package/express", true},
		{"https://www.npmjs.com/package/@types/node/v/1.2.3", true},
		{"pkg:github/foo/bar", false},
		{"random", false},
	}
	for _, tc := range cases {
		if got := imp.Matches(tc.ref); got != tc.want {
			t.Fatalf("Matches(%q) = %v want %v", tc.ref, got, tc.want)
		}
	}
}

func TestNpm_ParseRef(t *testing.T) {
	cases := []struct {
		raw     string
		name    string
		version string
	}{
		{"pkg:npm/express@4.17.1", "express", "4.17.1"},
		{"pkg:npm/express", "express", ""},
		{"pkg:npm/%40scope/pkg@1.2.3", "@scope/pkg", "1.2.3"},
		{"npm:express@4.17.1", "express", "4.17.1"},
		{"npm:express", "express", ""},
		{"npm:@scope/pkg", "@scope/pkg", ""},
		{"npm:@scope/pkg@1.0.0", "@scope/pkg", "1.0.0"},
		{"https://www.npmjs.com/package/express", "express", ""},
		{"https://www.npmjs.com/package/express/v/4.17.1", "express", "4.17.1"},
		{"https://www.npmjs.com/package/@types/node", "@types/node", ""},
		{"https://www.npmjs.com/package/@types/node/v/20.0.0", "@types/node", "20.0.0"},
	}
	for _, tc := range cases {
		name, ver, err := parseNpmRef(tc.raw)
		if err != nil {
			t.Fatalf("parseNpmRef(%q): %v", tc.raw, err)
		}
		if name != tc.name || ver != tc.version {
			t.Fatalf("parseNpmRef(%q) = (%q,%q) want (%q,%q)", tc.raw, name, ver, tc.name, tc.version)
		}
	}
}

func TestNpm_FetchHappyUnscoped(t *testing.T) {
	// Version-pinned fetch hits the per-version endpoint, whose body
	// is a flat doc (no dist-tags / versions wrapper).
	body := `{
  "name": "libx",
  "version": "1.2.3",
  "description": "a library",
  "homepage": "https://example.com/libx",
  "license": "Apache-2.0",
  "author": {"name":"Acme","email":"dev@acme.io","url":"https://acme.io"},
  "repository": {"type":"git","url":"git+https://github.com/acme/libx.git"},
  "bugs": {"url":"https://github.com/acme/libx/issues"},
  "dist": {"integrity": "sha512-` + sriFor([]byte("libx-1.2.3")) + `"}
}`
	_, imp := newFakeNpm(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:npm/libx@1.2.3")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Name != "libx" {
		t.Fatalf("name: %q", comp.Name)
	}
	if *comp.Version != "1.2.3" {
		t.Fatalf("version: %q", *comp.Version)
	}
	if *comp.Purl != "pkg:npm/libx@1.2.3" {
		t.Fatalf("purl: %q", *comp.Purl)
	}
	if comp.License == nil || comp.License.Expression != "Apache-2.0" {
		t.Fatalf("license: %+v", comp.License)
	}
	if comp.Supplier == nil || comp.Supplier.Name != "Acme" {
		t.Fatalf("supplier: %+v", comp.Supplier)
	}
	if *comp.Supplier.Email != "dev@acme.io" {
		t.Fatalf("supplier.email: %+v", comp.Supplier.Email)
	}

	types := map[string]string{}
	for _, r := range comp.ExternalReferences {
		types[r.Type] = r.URL
	}
	if types["website"] != "https://example.com/libx" {
		t.Fatalf("website: %q", types["website"])
	}
	if types["vcs"] != "https://github.com/acme/libx" {
		t.Fatalf("vcs (cleanup git+ and .git): %q", types["vcs"])
	}
	if types["issue-tracker"] != "https://github.com/acme/libx/issues" {
		t.Fatalf("issue-tracker: %q", types["issue-tracker"])
	}

	if len(comp.Hashes) != 1 {
		t.Fatalf("hashes: %+v", comp.Hashes)
	}
	if comp.Hashes[0].Algorithm != "SHA-512" {
		t.Fatalf("hash algorithm: %q", comp.Hashes[0].Algorithm)
	}
	if comp.Hashes[0].Value == nil || *comp.Hashes[0].Value == "" {
		t.Fatal("hash value empty")
	}
}

func TestNpm_FetchLatestTagFallback(t *testing.T) {
	// Unpinned → abbreviated endpoint → must wrap the version under
	// dist-tags and versions.
	body := `{
  "name":"x",
  "dist-tags":{"latest":"9.9.9"},
  "versions":{"9.9.9":{"name":"x","version":"9.9.9"}}
}`
	_, imp := newFakeNpm(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:npm/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "9.9.9" {
		t.Fatalf("expected dist-tags.latest fallback, got %q", *comp.Version)
	}
}

func TestNpm_FetchScopedURLEncoded(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		_, _ = w.Write([]byte(`{"name":"@types/node","version":"20.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	imp := &NpmImporter{BaseURL: srv.URL}
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:npm/%40types/node@20.0.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(gotURI, "@types%2Fnode/20.0.0") {
		t.Fatalf("expected @types%%2Fnode/20.0.0 in URI, got %q", gotURI)
	}
}

func TestNpm_Fetch404(t *testing.T) {
	_, imp := newFakeNpm(t, `{"error":"not found"}`, http.StatusNotFound)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:npm/nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNpm_FetchMissingVersion(t *testing.T) {
	// Version pinned → hits /<name>/<version> which returns 404 for
	// unpublished versions.
	_, imp := newFakeNpm(t, `{"error":"not found"}`, http.StatusNotFound)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:npm/x@ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unpublished version, got %v", err)
	}
}

func TestNpm_FetchDeprecatedLicenseObject(t *testing.T) {
	// Old packages carry `license: { type, url }`. We best-effort
	// pull the SPDX from `type`.
	body := `{
  "name":"x",
  "version":"1.0",
  "license": {"type":"MIT","url":"https://example.com/l"}
}`
	_, imp := newFakeNpm(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:npm/x@1.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License == nil || comp.License.Expression != "MIT" {
		t.Fatalf("license not lifted from deprecated object form: %+v", comp.License)
	}
}

func TestNpm_FetchNonSPDXLicenseWarnings(t *testing.T) {
	warn := &bytes.Buffer{}
	diag.SetSink(warn)
	diag.Reset()
	t.Cleanup(func() { diag.SetSink(nil); diag.Reset() })

	body := `{
  "name":"x",
  "version":"1.0",
  "license":"SEE LICENSE IN LICENSE.txt"
}`
	_, imp := newFakeNpm(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:npm/x@1.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("license should be nil for non-SPDX string, got %+v", comp.License)
	}
	if !strings.Contains(warn.String(), "not a plain SPDX expression") {
		t.Fatalf("expected warning, stderr: %q", warn.String())
	}
}

func TestNpm_FetchAuthorString(t *testing.T) {
	body := `{
  "name":"x",
  "version":"1",
  "author":"Alice <alice@example.com> (https://alice.example)"
}`
	_, imp := newFakeNpm(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:npm/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Supplier == nil || comp.Supplier.Name != "Alice" {
		t.Fatalf("supplier.name: %+v", comp.Supplier)
	}
	if comp.Supplier.Email == nil || *comp.Supplier.Email != "alice@example.com" {
		t.Fatalf("supplier.email: %+v", comp.Supplier.Email)
	}
	if comp.Supplier.URL == nil || *comp.Supplier.URL != "https://alice.example" {
		t.Fatalf("supplier.url: %+v", comp.Supplier.URL)
	}
}

func TestNpm_FetchRepositoryString(t *testing.T) {
	// Some packages carry `repository: "git+https://..."` (string
	// form). We strip the git+ and .git sugar.
	body := `{
  "name":"x",
  "version":"1",
  "repository":"git+https://github.com/o/r.git"
}`
	_, imp := newFakeNpm(t, body, 0)
	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:npm/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var vcs string
	for _, r := range comp.ExternalReferences {
		if r.Type == "vcs" {
			vcs = r.URL
		}
	}
	if vcs != "https://github.com/o/r" {
		t.Fatalf("vcs: %q want https://github.com/o/r", vcs)
	}
}

func TestNpm_DecodeSRI(t *testing.T) {
	payload := []byte("hello world")
	sri := "sha512-" + sriFor(payload)
	h, err := decodeSRI(sri)
	if err != nil {
		t.Fatalf("decodeSRI: %v", err)
	}
	if h.Algorithm != "SHA-512" {
		t.Fatalf("alg: %q", h.Algorithm)
	}
	sum := sha512.Sum512(payload)
	want := hex.EncodeToString(sum[:])
	if *h.Value != want {
		t.Fatalf("value: got %q want %q", *h.Value, want)
	}
}

func TestNpm_DecodeSRIPicksStrongest(t *testing.T) {
	// SRI may carry multiple tokens; we should pick the strongest.
	payload := []byte("x")
	sum := sha512.Sum512(payload)
	sri := "sha256-abcd sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	h, err := decodeSRI(sri)
	if err != nil {
		t.Fatalf("decodeSRI: %v", err)
	}
	if h.Algorithm != "SHA-512" {
		t.Fatalf("should pick SHA-512 over SHA-256, got %q", h.Algorithm)
	}
}

func TestNpm_DecodeSRIRejectsUnknownAlg(t *testing.T) {
	_, err := decodeSRI("md5-AAAAAAAAAAAAAAAAAAAAAA==")
	if err == nil {
		t.Fatal("expected error for md5 SRI")
	}
}

func TestNpm_BaseURLEnvOverride(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{"name":"x","version":"1"}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("BOMTIQUE_NPM_BASE_URL", srv.URL)
	imp := &NpmImporter{}
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:npm/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !hit {
		t.Fatal("env-var base URL not honoured")
	}
}

func TestNpm_RegistersGlobally(t *testing.T) {
	imp := Default().Match("pkg:npm/foo@1")
	if imp == nil {
		t.Fatal("NpmImporter not registered globally")
	}
	if _, ok := imp.(*NpmImporter); !ok {
		t.Fatalf("unexpected importer type %T", imp)
	}
}

// sriFor helps tests build a valid sha512 SRI base64 payload for
// given content.
func sriFor(payload []byte) string {
	sum := sha512.Sum512(payload)
	return base64.StdEncoding.EncodeToString(sum[:])
}
