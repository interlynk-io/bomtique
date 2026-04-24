// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeCargo stands up an httptest.Server that routes
// /api/v1/crates/<name>/<version> to tagHandler and the bare
// /api/v1/crates/<name> to crateHandler. Either handler may be nil,
// in which case a 200 with an empty-ish body is returned.
func newFakeCargo(t *testing.T, crateHandler, versionHandler http.HandlerFunc) (*httptest.Server, *CargoImporter) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/crates/", func(w http.ResponseWriter, r *http.Request) {
		// /api/v1/crates/<name>/<version> has three path segments after
		// the prefix; /api/v1/crates/<name> has two.
		rel := strings.TrimPrefix(r.URL.Path, "/api/v1/crates/")
		rel = strings.Trim(rel, "/")
		parts := strings.Split(rel, "/")
		if len(parts) >= 2 {
			if versionHandler != nil {
				versionHandler(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if crateHandler != nil {
			crateHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return srv, &CargoImporter{BaseURL: srv.URL}
}

func TestCargo_Matches(t *testing.T) {
	imp := &CargoImporter{}
	cases := []struct {
		ref  string
		want bool
	}{
		{"pkg:cargo/serde@1.0", true},
		{"pkg:cargo/serde", true},
		{"https://crates.io/crates/serde", true},
		{"https://crates.io/crates/serde/1.0", true},
		{"pkg:npm/foo@1", false},
		{"random", false},
	}
	for _, tc := range cases {
		if got := imp.Matches(tc.ref); got != tc.want {
			t.Fatalf("Matches(%q) = %v want %v", tc.ref, got, tc.want)
		}
	}
}

func TestCargo_ParseRef(t *testing.T) {
	cases := []struct {
		raw     string
		name    string
		version string
	}{
		{"pkg:cargo/serde@1.0.0", "serde", "1.0.0"},
		{"pkg:cargo/serde", "serde", ""},
		{"https://crates.io/crates/serde", "serde", ""},
		{"https://crates.io/crates/serde/1.0.0", "serde", "1.0.0"},
		{"https://crates.io/crates/serde/1.0.0/", "serde", "1.0.0"},
	}
	for _, tc := range cases {
		n, v, err := parseCargoRef(tc.raw)
		if err != nil {
			t.Fatalf("parseCargoRef(%q): %v", tc.raw, err)
		}
		if n != tc.name || v != tc.version {
			t.Fatalf("parseCargoRef(%q) = (%q,%q) want (%q,%q)", tc.raw, n, v, tc.name, tc.version)
		}
	}
}

func TestCargo_FetchHappyPinned(t *testing.T) {
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/crates/serde" {
				t.Fatalf("crate path: %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "crate": {
    "id": "serde",
    "name": "serde",
    "description": "A generic serialization/deserialization framework",
    "homepage": "https://serde.rs",
    "documentation": "https://docs.rs/serde",
    "repository": "https://github.com/serde-rs/serde",
    "newest_version": "1.0.193"
  }
}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/crates/serde/1.0.193" {
				t.Fatalf("version path: %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "version": {
    "num": "1.0.193",
    "license": "MIT OR Apache-2.0",
    "checksum": "25DD9975E684D4078D48E3cafebabe..."
  }
}`))
		})

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:cargo/serde@1.0.193")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.Name != "serde" {
		t.Fatalf("name: %q", comp.Name)
	}
	if *comp.Version != "1.0.193" {
		t.Fatalf("version: %q", *comp.Version)
	}
	if *comp.Purl != "pkg:cargo/serde@1.0.193" {
		t.Fatalf("purl: %q", *comp.Purl)
	}
	if comp.License == nil || comp.License.Expression != "MIT OR Apache-2.0" {
		t.Fatalf("license: %+v", comp.License)
	}
	if comp.Description == nil || !strings.Contains(*comp.Description, "serialization") {
		t.Fatalf("description: %v", comp.Description)
	}

	types := map[string]string{}
	for _, r := range comp.ExternalReferences {
		types[r.Type] = r.URL
	}
	if types["website"] != "https://serde.rs" {
		t.Fatalf("website: %q", types["website"])
	}
	if types["vcs"] != "https://github.com/serde-rs/serde" {
		t.Fatalf("vcs: %q", types["vcs"])
	}
	if types["documentation"] != "https://docs.rs/serde" {
		t.Fatalf("documentation: %q", types["documentation"])
	}
	if len(comp.Hashes) != 1 {
		t.Fatalf("hashes: %+v", comp.Hashes)
	}
	if comp.Hashes[0].Algorithm != "SHA-256" {
		t.Fatalf("hash algorithm: %q", comp.Hashes[0].Algorithm)
	}
	// Checksum should be lowercased.
	if *comp.Hashes[0].Value != "25dd9975e684d4078d48e3cafebabe..." {
		t.Fatalf("hash value not lowercased: %q", *comp.Hashes[0].Value)
	}
}

func TestCargo_FetchLatestFallback(t *testing.T) {
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"crate":{"name":"x","newest_version":"9.9.9","description":"d"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/9.9.9") {
				t.Fatalf("expected version endpoint to use newest_version, got %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"version":{"num":"9.9.9","license":"MIT","checksum":"abc"}}`))
		})

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:cargo/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "9.9.9" {
		t.Fatalf("version: %q", *comp.Version)
	}
}

func TestCargo_FetchMaxVersionFallback(t *testing.T) {
	// Some crates.io responses use `max_version` instead of
	// `newest_version`. We fall back to it.
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"crate":{"name":"x","max_version":"3.2.1"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"version":{"num":"3.2.1","license":"MIT","checksum":"abc"}}`))
		})

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:cargo/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if *comp.Version != "3.2.1" {
		t.Fatalf("version: %q", *comp.Version)
	}
}

func TestCargo_FetchCrateNotFound(t *testing.T) {
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, nil)

	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:cargo/noexist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCargo_FetchVersionNotFound(t *testing.T) {
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"crate":{"name":"x","newest_version":"1.0"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:cargo/x@ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCargo_FetchUASatisfiesToS(t *testing.T) {
	var gotUA string
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"crate":{"name":"x","newest_version":"1.0"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"version":{"num":"1.0","license":"MIT","checksum":"abc"}}`))
		})

	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:cargo/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// crates.io ToS: UA must identify the tool AND include a contact
	// URL.
	if !strings.HasPrefix(gotUA, "bomtique/") {
		t.Fatalf("UA must identify bomtique: %q", gotUA)
	}
	if !strings.Contains(gotUA, "http") {
		t.Fatalf("UA must carry a contact URL: %q", gotUA)
	}
}

func TestCargo_FetchRateLimited(t *testing.T) {
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, nil)
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:cargo/x@1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestCargo_BaseURLEnvOverride(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		// Answer both crate and version endpoints with minimal bodies.
		rel := strings.TrimPrefix(r.URL.Path, "/api/v1/crates/")
		if strings.Contains(rel, "/") {
			_, _ = w.Write([]byte(`{"version":{"num":"1","license":"MIT","checksum":"abc"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"crate":{"name":"x","newest_version":"1"}}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("BOMTIQUE_CARGO_BASE_URL", srv.URL)
	imp := &CargoImporter{}
	c := NewClient()
	_, err := imp.Fetch(context.Background(), c, "pkg:cargo/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !hit {
		t.Fatal("env-var base URL not honoured")
	}
}

func TestCargo_RegistersGlobally(t *testing.T) {
	imp := Default().Match("pkg:cargo/foo@1")
	if imp == nil {
		t.Fatal("CargoImporter not registered globally")
	}
	if _, ok := imp.(*CargoImporter); !ok {
		t.Fatalf("unexpected importer type %T", imp)
	}
}

func TestCargo_FetchMissingLicenseWarns(t *testing.T) {
	_, imp := newFakeCargo(t,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"crate":{"name":"x","newest_version":"1"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			// No license field — some crates predate the requirement.
			_, _ = w.Write([]byte(`{"version":{"num":"1","license":"","checksum":"abc"}}`))
		})

	c := NewClient()
	comp, err := imp.Fetch(context.Background(), c, "pkg:cargo/x@1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if comp.License != nil {
		t.Fatalf("license should be nil when empty upstream, got %+v", comp.License)
	}
}
