// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// NpmImporter pulls package metadata from the npm registry. Accepts:
//
//   - https://www.npmjs.com/package/<name>[/v/<version>]
//   - npm:<name>[@<version>]
//   - pkg:npm/<name>[@<version>]
//
// Scoped names (`@scope/name`) are supported in every form. The
// slash between scope and name is URL-encoded as %2F before the
// request hits registry.npmjs.org.
type NpmImporter struct {
	// BaseURL overrides the default https://registry.npmjs.org for
	// tests. BOMTIQUE_NPM_BASE_URL is consulted when empty.
	BaseURL string
}

// Name returns the importer's short label.
func (n *NpmImporter) Name() string { return "npm" }

// Matches returns true for any of the three accepted input shapes.
func (n *NpmImporter) Matches(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "pkg:npm/") {
		return true
	}
	if strings.HasPrefix(ref, "npm:") {
		return true
	}
	if strings.HasPrefix(ref, "https://www.npmjs.com/package/") {
		return true
	}
	if strings.HasPrefix(ref, "http://www.npmjs.com/package/") {
		return true
	}
	return false
}

// npmVersion is the per-version payload shape — used by both the
// per-version endpoint and individual entries inside the abbreviated
// package doc's `versions` map.
type npmVersion struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Homepage    string          `json:"homepage"`
	License     json.RawMessage `json:"license"`
	Author      json.RawMessage `json:"author"`
	Repository  json.RawMessage `json:"repository"`
	Bugs        json.RawMessage `json:"bugs"`
	Dist        struct {
		Integrity string `json:"integrity"`
		Shasum    string `json:"shasum"`
	} `json:"dist"`
}

// Fetch resolves the ref against the npm registry and returns a
// manifest.Component. The registry has two endpoints we rely on:
//
//   - GET /<name>/<version> returns a compact per-version document
//     (fastest path, small response).
//   - GET /<name> with Accept: application/vnd.npm.install-v1+json
//     returns an abbreviated package document (dist-tags + trimmed
//     versions). We only use it when no version is pinned so we can
//     read dist-tags.latest.
//
// This two-endpoint split keeps us well under the 1 MiB response cap
// even for packages with hundreds of versions (@types/node, etc.).
func (n *NpmImporter) Fetch(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
	name, version, err := parseNpmRef(ref)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(n.baseURL(), "/")
	pathName := strings.ReplaceAll(name, "/", "%2F")

	var v npmVersion
	if version == "" {
		// Abbreviated metadata → pluck latest, then look up.
		url := fmt.Sprintf("%s/%s", base, pathName)
		res, err := c.Get(ctx, url, map[string]string{
			"Accept": "application/vnd.npm.install-v1+json",
		})
		if err != nil {
			return nil, err
		}
		if res.Status == 404 {
			return nil, fmt.Errorf("%w: npm package %q", ErrNotFound, name)
		}
		if res.Status >= 400 {
			return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, url, res.Status)
		}
		var abbrev struct {
			DistTags map[string]string     `json:"dist-tags"`
			Versions map[string]npmVersion `json:"versions"`
		}
		if err := json.Unmarshal(res.Body, &abbrev); err != nil {
			return nil, fmt.Errorf("%w: decode npm abbreviated body: %w", ErrNetwork, err)
		}
		version = strings.TrimSpace(abbrev.DistTags["latest"])
		if version == "" {
			return nil, fmt.Errorf("%w: npm package %q has no dist-tags.latest", ErrNotFound, name)
		}
		vv, ok := abbrev.Versions[version]
		if !ok {
			return nil, fmt.Errorf("%w: npm package %q lists latest=%q but the version is missing", ErrNotFound, name, version)
		}
		v = vv
	} else {
		url := fmt.Sprintf("%s/%s/%s", base, pathName, version)
		res, err := c.Get(ctx, url, nil)
		if err != nil {
			return nil, err
		}
		if res.Status == 404 {
			return nil, fmt.Errorf("%w: npm package %q has no published version %q", ErrNotFound, name, version)
		}
		if res.Status >= 400 {
			return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, url, res.Status)
		}
		if err := json.Unmarshal(res.Body, &v); err != nil {
			return nil, fmt.Errorf("%w: decode npm per-version body: %w", ErrNetwork, err)
		}
	}

	component := &manifest.Component{
		Name:    name,
		Version: strPtrLocal(version),
		Purl:    strPtrLocal(npmPurl(name, version)),
	}
	if v.Description != "" {
		component.Description = strPtrLocal(v.Description)
	}
	if spdx := parseNpmLicense(v.License); spdx != "" {
		component.License = &manifest.License{Expression: spdx}
	} else if len(v.License) > 0 && string(v.License) != "null" {
		diag.Warn("npm: license for %s is not a plain SPDX expression; skipping (pass --license)", name)
	}

	if sup := parseNpmAuthor(v.Author); sup != nil {
		component.Supplier = sup
	}

	if v.Homepage != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "website", URL: v.Homepage})
	}
	if repoURL := parseNpmRepositoryURL(v.Repository); repoURL != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "vcs", URL: repoURL})
	}
	if bugsURL := parseNpmBugsURL(v.Bugs); bugsURL != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "issue-tracker", URL: bugsURL})
	}

	if h, err := decodeSRI(v.Dist.Integrity); err == nil && h != nil {
		component.Hashes = append(component.Hashes, *h)
	} else if err != nil {
		diag.Warn("npm: integrity for %s@%s could not be decoded: %v", name, version, err)
	}

	return component, nil
}

// baseURL returns the effective npm registry base. Precedence:
// explicit BaseURL → BOMTIQUE_NPM_BASE_URL env → registry.npmjs.org.
func (n *NpmImporter) baseURL() string {
	if n.BaseURL != "" {
		return n.BaseURL
	}
	if env := strings.TrimSpace(os.Getenv("BOMTIQUE_NPM_BASE_URL")); env != "" {
		return env
	}
	return "https://registry.npmjs.org"
}

// parseNpmRef extracts (name, version) from any of the three accepted
// input shapes. Name preserves its @scope/... form; version may be
// empty.
func parseNpmRef(raw string) (name, version string, err error) {
	raw = strings.TrimSpace(raw)

	if strings.HasPrefix(raw, "pkg:npm/") {
		p, perr := purl.Parse(raw)
		if perr != nil {
			err = fmt.Errorf("%w: bad pkg:npm ref %q: %w", ErrUnsupportedRef, raw, perr)
			return
		}
		// Scoped: namespace carries @scope; name carries the package
		// name proper. Unscoped: namespace empty, name carries the
		// whole identifier.
		if p.Namespace != "" {
			name = p.Namespace + "/" + p.Name
		} else {
			name = p.Name
		}
		version = p.Version
		return
	}

	if strings.HasPrefix(raw, "npm:") {
		body := strings.TrimPrefix(raw, "npm:")
		name, version = splitNpmNameVersion(body)
		return
	}

	for _, prefix := range []string{"https://www.npmjs.com/package/", "http://www.npmjs.com/package/"} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		rest := strings.Trim(strings.TrimPrefix(raw, prefix), "/")
		if rest == "" {
			err = fmt.Errorf("%w: npm URL missing package name: %q", ErrUnsupportedRef, raw)
			return
		}
		// /v/<version> suffix.
		if idx := strings.Index(rest, "/v/"); idx >= 0 {
			name = rest[:idx]
			version = strings.TrimPrefix(rest[idx:], "/v/")
			return
		}
		name = rest
		return
	}
	err = fmt.Errorf("%w: %q is not an npm ref", ErrUnsupportedRef, raw)
	return
}

// splitNpmNameVersion handles the `npm:<body>` form. `body` is either
// `name`, `@scope/name`, `name@version`, or `@scope/name@version`.
// The scoped leading `@` is NOT a version separator.
func splitNpmNameVersion(body string) (name, version string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ""
	}
	// Find the LAST `@` that is NOT the leading scope marker.
	searchFrom := 0
	if strings.HasPrefix(body, "@") {
		searchFrom = 1
	}
	idx := strings.LastIndexByte(body[searchFrom:], '@')
	if idx < 0 {
		return body, ""
	}
	idx += searchFrom
	return body[:idx], body[idx+1:]
}

// npmPurl produces a canonical pkg:npm purl, matching the scoped-
// name convention of the purl-spec: scope becomes namespace, name
// becomes the package name.
func npmPurl(name, version string) string {
	if strings.HasPrefix(name, "@") {
		// @scope/name → pkg:npm/%40scope/name — actually canonical
		// form is pkg:npm/@scope/name (purl-spec special-cases npm's
		// scoped names; the purl parser accepts the unescaped @).
		return fmt.Sprintf("pkg:npm/%s@%s", name, version)
	}
	return fmt.Sprintf("pkg:npm/%s@%s", name, version)
}

// parseNpmLicense decodes npm's `license` field. Modern packages
// carry a plain SPDX expression string; legacy packages may carry an
// `{ type, url }` object or a deprecated `licenses: [...]` array.
// Returns the best-effort SPDX expression string, or "" when none
// can be extracted.
func parseNpmLicense(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if isSPDXExpressionShape(s) {
				return s
			}
		}
		return ""
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil {
			if isSPDXExpressionShape(obj.Type) {
				return obj.Type
			}
		}
	}
	return ""
}

// spdxCharset is a lightweight check for strings that look like SPDX
// expressions — alphanumerics plus the punctuation the grammar uses.
// Real validation lives in internal/manifest/validate.
var spdxCharset = regexp.MustCompile(`^[A-Za-z0-9.\-+\s()]+$`)

func isSPDXExpressionShape(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if !spdxCharset.MatchString(s) {
		return false
	}
	// Reject obviously non-SPDX strings: sentence-like text with
	// lower-case operators or ending in a period.
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".") {
		return false
	}
	// npm commonly uses "SEE LICENSE IN <file>" — not an SPDX
	// expression.
	if strings.HasPrefix(lower, "see license") {
		return false
	}
	return true
}

// parseNpmAuthor decodes the npm `author` field. It may be a string
// of the form `Name <email> (url)` OR an object
// `{ name, email?, url? }`. Either form lowers to a manifest.Supplier.
func parseNpmAuthor(raw json.RawMessage) *manifest.Supplier {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil
		}
		return parseAuthorString(s)
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			URL   string `json:"url"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		name := strings.TrimSpace(obj.Name)
		if name == "" {
			return nil
		}
		sup := &manifest.Supplier{Name: name}
		if e := strings.TrimSpace(obj.Email); e != "" {
			sup.Email = &e
		}
		if u := strings.TrimSpace(obj.URL); u != "" {
			sup.URL = &u
		}
		return sup
	}
	return nil
}

var authorStringRE = regexp.MustCompile(`^\s*([^<(]+?)\s*(?:<([^>]+)>)?\s*(?:\(([^)]+)\))?\s*$`)

// parseAuthorString handles npm's classic
// "Name <email> (url)" shorthand.
func parseAuthorString(s string) *manifest.Supplier {
	m := authorStringRE.FindStringSubmatch(s)
	if m == nil {
		name := strings.TrimSpace(s)
		if name == "" {
			return nil
		}
		return &manifest.Supplier{Name: name}
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return nil
	}
	sup := &manifest.Supplier{Name: name}
	if e := strings.TrimSpace(m[2]); e != "" {
		sup.Email = &e
	}
	if u := strings.TrimSpace(m[3]); u != "" {
		sup.URL = &u
	}
	return sup
}

// parseNpmRepositoryURL handles both the string and object form of
// npm's `repository` field. Strips a leading "git+" and a trailing
// ".git" so the resulting URL is browser-friendly.
func parseNpmRepositoryURL(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	trimmed := strings.TrimSpace(string(raw))
	var u string
	switch {
	case strings.HasPrefix(trimmed, `"`):
		_ = json.Unmarshal(raw, &u)
	case strings.HasPrefix(trimmed, "{"):
		var obj struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil {
			u = obj.URL
		}
	}
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "git+")
	u = strings.TrimSuffix(u, ".git")
	return u
}

// parseNpmBugsURL decodes npm's `bugs` field (string or
// `{ url, email? }` object).
func parseNpmBugsURL(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case strings.HasPrefix(trimmed, `"`):
		var u string
		if err := json.Unmarshal(raw, &u); err == nil {
			return strings.TrimSpace(u)
		}
	case strings.HasPrefix(trimmed, "{"):
		var obj struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil {
			return strings.TrimSpace(obj.URL)
		}
	}
	return ""
}

// decodeSRI decodes an npm Subresource Integrity string
// (e.g. "sha512-<base64>") into a literal-form §8.1 Hash entry.
// Returns nil, nil when the integrity is empty. Only SHA-256 /
// SHA-384 / SHA-512 are accepted — §8.1 forbids weaker algorithms.
func decodeSRI(sri string) (*manifest.Hash, error) {
	sri = strings.TrimSpace(sri)
	if sri == "" {
		return nil, nil
	}
	// SRI may carry multiple space-separated tokens of decreasing
	// strength; pick the strongest.
	tokens := strings.Fields(sri)
	var bestAlg, bestB64 string
	rank := map[string]int{"sha256": 1, "sha384": 2, "sha512": 3}
	bestRank := 0
	for _, tok := range tokens {
		dash := strings.IndexByte(tok, '-')
		if dash <= 0 {
			continue
		}
		alg := strings.ToLower(tok[:dash])
		b64 := tok[dash+1:]
		r, ok := rank[alg]
		if !ok {
			continue
		}
		if r > bestRank {
			bestRank = r
			bestAlg = alg
			bestB64 = b64
		}
	}
	if bestAlg == "" {
		return nil, fmt.Errorf("SRI %q carries no SHA-256/384/512 token", sri)
	}
	raw, err := base64.StdEncoding.DecodeString(bestB64)
	if err != nil {
		return nil, fmt.Errorf("SRI %q base64: %w", sri, err)
	}
	value := hex.EncodeToString(raw)
	algName := map[string]string{
		"sha256": "SHA-256",
		"sha384": "SHA-384",
		"sha512": "SHA-512",
	}[bestAlg]
	return &manifest.Hash{Algorithm: algName, Value: &value}, nil
}

func init() {
	Register(&NpmImporter{})
}
