// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// fakeImporter is a reusable test importer that matches refs with a
// chosen prefix and returns a canned Component. Fetch behaviour can
// be swapped via the FetchFn hook.
type fakeImporter struct {
	name    string
	prefix  string
	FetchFn func(ctx context.Context, c *Client, ref string) (*manifest.Component, error)
}

func (f *fakeImporter) Name() string            { return f.name }
func (f *fakeImporter) Matches(ref string) bool { return strings.HasPrefix(ref, f.prefix) }
func (f *fakeImporter) Fetch(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
	if f.FetchFn != nil {
		return f.FetchFn(ctx, c, ref)
	}
	v := strings.TrimPrefix(ref, f.prefix)
	return &manifest.Component{Name: "fake", Version: &v}, nil
}

func TestRegistry_FetchDispatches(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeImporter{name: "one", prefix: "one:"})
	r.Register(&fakeImporter{name: "two", prefix: "two:"})

	c, err := r.Fetch(context.Background(), NewClient(), "two:1.2.3")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if c.Name != "fake" || *c.Version != "1.2.3" {
		t.Fatalf("unexpected component: %+v", c)
	}
}

func TestRegistry_FetchFirstMatchWins(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeImporter{name: "first", prefix: "pkg:",
		FetchFn: func(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
			return &manifest.Component{Name: "first-wins", Version: strPtr("1")}, nil
		}})
	r.Register(&fakeImporter{name: "second", prefix: "pkg:",
		FetchFn: func(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
			return &manifest.Component{Name: "second-never", Version: strPtr("1")}, nil
		}})

	c, err := r.Fetch(context.Background(), NewClient(), "pkg:anything")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "first-wins" {
		t.Fatalf("first-match-wins broken: got %q", c.Name)
	}
}

func TestRegistry_FetchUnsupported(t *testing.T) {
	r := NewRegistry()
	_, err := r.Fetch(context.Background(), NewClient(), "something-weird")
	if !errors.Is(err, ErrUnsupportedRef) {
		t.Fatalf("expected ErrUnsupportedRef, got %v", err)
	}
}

func TestRegistry_Match(t *testing.T) {
	r := NewRegistry()
	imp := &fakeImporter{name: "one", prefix: "one:"}
	r.Register(imp)
	if got := r.Match("one:x"); got == nil || got.Name() != "one" {
		t.Fatalf("match returned %v", got)
	}
	if got := r.Match("other:x"); got != nil {
		t.Fatalf("match should be nil: %v", got)
	}
}

func TestClient_GetHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept header: got %q", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "bomtique/") {
			t.Fatalf("User-Agent: got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient()
	res, err := c.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("status: %d", res.Status)
	}
	if string(res.Body) != `{"ok":true}` {
		t.Fatalf("body: %q", res.Body)
	}
}

func TestClient_GetAppliesExtraHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token123" {
			t.Fatalf("Authorization header: got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	_, err := c.Get(context.Background(), srv.URL, map[string]string{
		"Authorization": "Bearer token123",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestClient_GetReturnsStatusCodeVerbatim(t *testing.T) {
	// The client does NOT interpret 404 / 429 / 5xx — the caller
	// does. We confirm the status round-trips cleanly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"gone"}`))
	}))
	defer srv.Close()
	c := NewClient()
	res, err := c.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("Get (non-2xx should not error): %v", err)
	}
	if res.Status != 404 {
		t.Fatalf("status: got %d want 404", res.Status)
	}
}

func TestClient_GetCapsBody(t *testing.T) {
	// Serve 2 MiB of padding — well above the 1 MiB default cap.
	big := make([]byte, 2<<20)
	for i := range big {
		big[i] = 'x'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(big)
	}))
	defer srv.Close()
	c := NewClient()
	_, err := c.Get(context.Background(), srv.URL, nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge, got %v", err)
	}
}

func TestClient_GetRejectsTransportError(t *testing.T) {
	// A URL that fails DNS quickly — use an invalid scheme instead
	// of a hostname so we don't rely on a real DNS path.
	c := NewClient()
	_, err := c.Get(context.Background(), "http://127.0.0.1:1/nowhere", nil)
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("expected ErrNetwork, got %v", err)
	}
}

func TestClient_SetUserAgentVersion(t *testing.T) {
	c := NewClient()
	c.SetUserAgentVersion("9.9.9")
	if !strings.Contains(c.UserAgent, "bomtique/9.9.9") {
		t.Fatalf("UA not updated: %q", c.UserAgent)
	}
	// Empty version is a no-op (keeps the default tag).
	orig := c.UserAgent
	c.SetUserAgentVersion("")
	if c.UserAgent != orig {
		t.Fatalf("UA should be unchanged on empty version: %q", c.UserAgent)
	}
}

// Integration smoke: register a fake importer, go through
// r.Fetch -> importer.Fetch -> Client.Get against an httptest.Server.
func TestRegistry_EndToEndFakeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"remote","version":"1.2.3"}`))
	}))
	defer srv.Close()

	imp := &fakeImporter{
		name:   "test",
		prefix: "test:",
		FetchFn: func(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
			res, err := c.Get(ctx, srv.URL, nil)
			if err != nil {
				return nil, err
			}
			if res.Status != 200 {
				return nil, fmt.Errorf("status %d", res.Status)
			}
			// Body is {"name":"remote","version":"1.2.3"} — trivial parse.
			return &manifest.Component{Name: "remote", Version: strPtr("1.2.3")}, nil
		},
	}
	r := NewRegistry()
	r.Register(imp)

	c, err := r.Fetch(context.Background(), NewClient(), "test:whatever")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if c.Name != "remote" || *c.Version != "1.2.3" {
		t.Fatalf("end-to-end fetch: %+v", c)
	}
}

func TestRegistry_RegisterNilIsNoop(t *testing.T) {
	r := NewRegistry()
	r.Register(nil)
	if got := r.Importers(); len(got) != 0 {
		t.Fatalf("nil importer was recorded: %v", got)
	}
}

func strPtr(s string) *string { return &s }
