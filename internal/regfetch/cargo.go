// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// CargoImporter pulls crate metadata from the crates.io API.
// Accepts:
//
//   - https://crates.io/crates/<name>[/<version>]
//   - pkg:cargo/<name>[@<version>]
//
// Network usage: one GET to /api/v1/crates/<name> for crate-level
// metadata (description, repository, documentation, homepage) plus
// one GET to /api/v1/crates/<name>/<version> for per-version license
// and checksum.
//
// crates.io's ToS requires the client User-Agent to identify the
// tool and include a contact URL. The shared [Client] default
// satisfies this; importer registration would fail fast if a future
// change made the UA anonymous.
type CargoImporter struct {
	// BaseURL overrides the default https://crates.io for tests.
	// BOMTIQUE_CARGO_BASE_URL is consulted when empty.
	BaseURL string
}

// Name returns the importer's short label.
func (c *CargoImporter) Name() string { return "cargo" }

// Matches returns true for crates.io URLs or pkg:cargo purls.
func (c *CargoImporter) Matches(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "pkg:cargo/") {
		return true
	}
	if strings.HasPrefix(ref, "https://crates.io/crates/") {
		return true
	}
	if strings.HasPrefix(ref, "http://crates.io/crates/") {
		return true
	}
	return false
}

type cargoCrate struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	Documentation string `json:"documentation"`
	Repository    string `json:"repository"`
	NewestVersion string `json:"newest_version"`
	MaxVersion    string `json:"max_version"`
}

type cargoVersion struct {
	Num      string `json:"num"`
	License  string `json:"license"`
	Checksum string `json:"checksum"`
}

// Fetch resolves the ref against the crates.io API and returns a
// manifest.Component.
func (c *CargoImporter) Fetch(ctx context.Context, client *Client, ref string) (*manifest.Component, error) {
	name, version, err := parseCargoRef(ref)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(c.baseURL(), "/")

	// First GET: crate metadata.
	crateURL := fmt.Sprintf("%s/api/v1/crates/%s", base, url.PathEscape(name))
	res, err := client.Get(ctx, crateURL, nil)
	if err != nil {
		return nil, err
	}
	if res.Status == 404 {
		return nil, fmt.Errorf("%w: crate %q", ErrNotFound, name)
	}
	if res.Status == 429 {
		return nil, fmt.Errorf("%w: crates.io returned 429 Too Many Requests; retry later", ErrRateLimited)
	}
	if res.Status >= 400 {
		return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, crateURL, res.Status)
	}

	var crateBody struct {
		Crate cargoCrate `json:"crate"`
	}
	if err := json.Unmarshal(res.Body, &crateBody); err != nil {
		return nil, fmt.Errorf("%w: decode crates.io crate body: %w", ErrNetwork, err)
	}

	if version == "" {
		version = strings.TrimSpace(crateBody.Crate.NewestVersion)
		if version == "" {
			version = strings.TrimSpace(crateBody.Crate.MaxVersion)
		}
		if version == "" {
			return nil, fmt.Errorf("%w: crate %q has no newest_version", ErrNotFound, name)
		}
	}

	// Second GET: version-specific data (license + checksum).
	verURL := fmt.Sprintf("%s/api/v1/crates/%s/%s", base, url.PathEscape(name), url.PathEscape(version))
	verRes, err := client.Get(ctx, verURL, nil)
	if err != nil {
		return nil, err
	}
	if verRes.Status == 404 {
		return nil, fmt.Errorf("%w: crate %q has no published version %q", ErrNotFound, name, version)
	}
	if verRes.Status == 429 {
		return nil, fmt.Errorf("%w: crates.io 429 confirming version %q", ErrRateLimited, version)
	}
	if verRes.Status >= 400 {
		return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, verURL, verRes.Status)
	}
	var verBody struct {
		Version cargoVersion `json:"version"`
	}
	if err := json.Unmarshal(verRes.Body, &verBody); err != nil {
		return nil, fmt.Errorf("%w: decode crates.io version body: %w", ErrNetwork, err)
	}

	component := &manifest.Component{
		Name:    name,
		Version: strPtrLocal(version),
		Purl:    strPtrLocal(fmt.Sprintf("pkg:cargo/%s@%s", name, version)),
	}
	if d := strings.TrimSpace(crateBody.Crate.Description); d != "" {
		component.Description = strPtrLocal(d)
	}
	if lic := strings.TrimSpace(verBody.Version.License); lic != "" {
		// crates.io licenses are SPDX expressions by convention —
		// "MIT OR Apache-2.0" is typical. Pass through verbatim.
		component.License = &manifest.License{Expression: lic}
	} else {
		diag.Warn("cargo: crate %s@%s carries no license; pass --license explicitly", name, version)
	}

	if crateBody.Crate.Homepage != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "website", URL: crateBody.Crate.Homepage})
	}
	if crateBody.Crate.Repository != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "vcs", URL: crateBody.Crate.Repository})
	}
	if crateBody.Crate.Documentation != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "documentation", URL: crateBody.Crate.Documentation})
	}

	// SHA-256 checksum is always lowercase hex on crates.io.
	if sum := strings.ToLower(strings.TrimSpace(verBody.Version.Checksum)); sum != "" {
		component.Hashes = append(component.Hashes, manifest.Hash{
			Algorithm: "SHA-256",
			Value:     strPtrLocal(sum),
		})
	}
	return component, nil
}

// baseURL returns the effective crates.io base. Precedence:
// explicit BaseURL → BOMTIQUE_CARGO_BASE_URL env → crates.io.
func (c *CargoImporter) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	if env := strings.TrimSpace(os.Getenv("BOMTIQUE_CARGO_BASE_URL")); env != "" {
		return env
	}
	return "https://crates.io"
}

// parseCargoRef extracts (name, version) from either a URL or a
// pkg:cargo purl.
func parseCargoRef(raw string) (name, version string, err error) {
	raw = strings.TrimSpace(raw)

	if strings.HasPrefix(raw, "pkg:cargo/") {
		p, perr := purl.Parse(raw)
		if perr != nil {
			err = fmt.Errorf("%w: bad pkg:cargo ref %q: %w", ErrUnsupportedRef, raw, perr)
			return
		}
		name = p.Name
		version = p.Version
		return
	}

	for _, prefix := range []string{"https://crates.io/crates/", "http://crates.io/crates/"} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		rest := strings.Trim(strings.TrimPrefix(raw, prefix), "/")
		if rest == "" {
			err = fmt.Errorf("%w: crates.io URL missing crate name: %q", ErrUnsupportedRef, raw)
			return
		}
		parts := strings.Split(rest, "/")
		name = parts[0]
		if len(parts) >= 2 {
			version = parts[1]
		}
		return
	}
	err = fmt.Errorf("%w: %q is not a crates.io ref", ErrUnsupportedRef, raw)
	return
}

func init() {
	Register(&CargoImporter{})
}
