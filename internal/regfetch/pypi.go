// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/purl"
)

// PyPIImporter pulls package metadata from the PyPI JSON API.
// Accepts:
//
//   - https://pypi.org/project/<name>[/<version>]
//   - pypi:<name>[@<version>]
//   - pkg:pypi/<name>[@<version>]
type PyPIImporter struct {
	// BaseURL overrides the default https://pypi.org for tests.
	// BOMTIQUE_PYPI_BASE_URL is consulted when empty.
	BaseURL string
}

// Name returns the importer's short label.
func (p *PyPIImporter) Name() string { return "pypi" }

// Matches returns true for any of the three accepted input shapes.
func (p *PyPIImporter) Matches(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "pkg:pypi/") {
		return true
	}
	if strings.HasPrefix(ref, "pypi:") {
		return true
	}
	if strings.HasPrefix(ref, "https://pypi.org/project/") {
		return true
	}
	if strings.HasPrefix(ref, "http://pypi.org/project/") {
		return true
	}
	return false
}

// pypiInfo mirrors the `info` block PyPI returns. It's deliberately
// trimmed — we only grab what the importer maps onto the Component
// shape.
type pypiInfo struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Summary         string            `json:"summary"`
	HomePage        string            `json:"home_page"`
	Author          string            `json:"author"`
	AuthorEmail     string            `json:"author_email"`
	Maintainer      string            `json:"maintainer"`
	MaintainerEmail string            `json:"maintainer_email"`
	License         string            `json:"license"`
	LicenseExpr     string            `json:"license_expression"`
	ProjectURLs     map[string]string `json:"project_urls"`
	Classifiers     []string          `json:"classifiers"`
}

// pypiDistribution is one entry of PyPI's `urls` array — a single
// uploaded distribution (sdist, wheel, etc.).
type pypiDistribution struct {
	PackageType string            `json:"packagetype"`
	Digests     map[string]string `json:"digests"`
}

// pypiResponse captures the top-level JSON shape of the /pypi/<name>
// and /pypi/<name>/<version> endpoints.
type pypiResponse struct {
	Info pypiInfo           `json:"info"`
	URLs []pypiDistribution `json:"urls"`
}

// Fetch resolves the ref against the PyPI JSON API and returns a
// manifest.Component.
func (p *PyPIImporter) Fetch(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
	rawName, version, err := parsePyPIRef(ref)
	if err != nil {
		return nil, err
	}
	name := normalisePEP503(rawName)

	base := strings.TrimRight(p.baseURL(), "/")
	var reqURL string
	if version == "" {
		reqURL = fmt.Sprintf("%s/pypi/%s/json", base, url.PathEscape(name))
	} else {
		reqURL = fmt.Sprintf("%s/pypi/%s/%s/json", base, url.PathEscape(name), url.PathEscape(version))
	}

	res, err := c.Get(ctx, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if res.Status == 404 {
		if version == "" {
			return nil, fmt.Errorf("%w: PyPI package %q not found", ErrNotFound, name)
		}
		return nil, fmt.Errorf("%w: PyPI package %q has no published version %q", ErrNotFound, name, version)
	}
	if res.Status == 429 {
		return nil, fmt.Errorf("%w: PyPI returned 429 Too Many Requests; retry later", ErrRateLimited)
	}
	if res.Status >= 400 {
		return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, reqURL, res.Status)
	}

	var body pypiResponse
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, fmt.Errorf("%w: decode PyPI body: %w", ErrNetwork, err)
	}

	resolvedVersion := strings.TrimSpace(body.Info.Version)
	if resolvedVersion == "" {
		resolvedVersion = version
	}
	if resolvedVersion == "" {
		return nil, fmt.Errorf("%w: PyPI response for %q carries no info.version", ErrNetwork, name)
	}

	component := &manifest.Component{
		Name:    name,
		Version: strPtrLocal(resolvedVersion),
		Purl:    strPtrLocal(fmt.Sprintf("pkg:pypi/%s@%s", name, resolvedVersion)),
	}
	if s := strings.TrimSpace(body.Info.Summary); s != "" {
		component.Description = strPtrLocal(s)
	}

	// License priority: info.license_expression → info.license (if
	// it looks like an SPDX expression) → classifier lookup.
	if spdx := pickPyPILicense(body.Info); spdx != "" {
		component.License = &manifest.License{Expression: spdx}
	} else if licenseNoteWorthy(body.Info) {
		diag.Warn("pypi: license for %s is not a recognised SPDX expression; dropping license field (pass --license explicitly)", name)
	}

	if sup := pickPyPISupplier(body.Info); sup != nil {
		component.Supplier = sup
	}

	if body.Info.HomePage != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "website", URL: body.Info.HomePage})
	}
	if src := firstNonEmpty(body.Info.ProjectURLs, "Source", "Source Code", "Repository", "Code"); src != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "vcs", URL: src})
	}
	if bugs := firstNonEmpty(body.Info.ProjectURLs, "Bug Tracker", "Issues", "Bug Reports", "Tracker"); bugs != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "issue-tracker", URL: bugs})
	}
	if docs := firstNonEmpty(body.Info.ProjectURLs, "Documentation", "Docs"); docs != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "documentation", URL: docs})
	}

	// Hashes — only when a specific distribution was returned. For
	// the per-version endpoint `urls` is populated; for the top-level
	// endpoint it corresponds to the latest version's distributions.
	if h := pickPyPIHash(body.URLs); h != nil {
		component.Hashes = append(component.Hashes, *h)
	}
	return component, nil
}

// baseURL returns the effective PyPI base URL. Precedence:
// explicit BaseURL → BOMTIQUE_PYPI_BASE_URL env → pypi.org.
func (p *PyPIImporter) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	if env := strings.TrimSpace(os.Getenv("BOMTIQUE_PYPI_BASE_URL")); env != "" {
		return env
	}
	return "https://pypi.org"
}

// parsePyPIRef handles the three accepted input forms. Name is
// returned as-supplied (case-preserved); the caller normalises via
// normalisePEP503.
func parsePyPIRef(raw string) (name, version string, err error) {
	raw = strings.TrimSpace(raw)

	if strings.HasPrefix(raw, "pkg:pypi/") {
		p, perr := purl.Parse(raw)
		if perr != nil {
			err = fmt.Errorf("%w: bad pkg:pypi ref %q: %w", ErrUnsupportedRef, raw, perr)
			return
		}
		name = p.Name
		version = p.Version
		return
	}

	if strings.HasPrefix(raw, "pypi:") {
		body := strings.TrimPrefix(raw, "pypi:")
		body = strings.TrimSpace(body)
		if body == "" {
			err = fmt.Errorf("%w: pypi: ref missing name: %q", ErrUnsupportedRef, raw)
			return
		}
		if at := strings.LastIndexByte(body, '@'); at > 0 && at < len(body)-1 {
			name = body[:at]
			version = body[at+1:]
			return
		}
		name = body
		return
	}

	for _, prefix := range []string{"https://pypi.org/project/", "http://pypi.org/project/"} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		rest := strings.Trim(strings.TrimPrefix(raw, prefix), "/")
		if rest == "" {
			err = fmt.Errorf("%w: PyPI URL missing package name: %q", ErrUnsupportedRef, raw)
			return
		}
		parts := strings.Split(rest, "/")
		name = parts[0]
		if len(parts) >= 2 {
			version = parts[1]
		}
		return
	}
	err = fmt.Errorf("%w: %q is not a PyPI ref", ErrUnsupportedRef, raw)
	return
}

// pep503DashRun is the PEP 503 separator run (-, _, .) that collapses
// to a single `-`.
var pep503DashRun = regexp.MustCompile(`[-_.]+`)

// normalisePEP503 lowercases the name and collapses runs of
// `-`, `_`, `.` to a single `-` (PEP 503 §2).
func normalisePEP503(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return ""
	}
	return pep503DashRun.ReplaceAllString(n, "-")
}

// pickPyPISupplier chooses an author (preferred) or maintainer block
// and returns it as a Supplier. Returns nil when no name is given.
func pickPyPISupplier(info pypiInfo) *manifest.Supplier {
	if name := strings.TrimSpace(info.Author); name != "" {
		s := &manifest.Supplier{Name: name}
		if e := strings.TrimSpace(info.AuthorEmail); e != "" {
			s.Email = &e
		}
		return s
	}
	if name := strings.TrimSpace(info.Maintainer); name != "" {
		s := &manifest.Supplier{Name: name}
		if e := strings.TrimSpace(info.MaintainerEmail); e != "" {
			s.Email = &e
		}
		return s
	}
	return nil
}

// firstNonEmpty returns the first non-empty value among `keys` in
// `m`. Used to reconcile the varied casing / naming of PyPI's
// project_urls.
func firstNonEmpty(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if vv := strings.TrimSpace(v); vv != "" {
				return vv
			}
		}
	}
	// Fall back to a case-insensitive scan so "source" vs "Source"
	// vs "Source code" all match.
	for _, k := range keys {
		target := strings.ToLower(k)
		for key, v := range m {
			if strings.EqualFold(key, target) || strings.Contains(strings.ToLower(key), target) {
				if vv := strings.TrimSpace(v); vv != "" {
					return vv
				}
			}
		}
	}
	return ""
}

// pickPyPIHash picks the strongest available SHA-256 digest from the
// uploaded distributions. sdist wins over wheels (more authoritative
// for source-level attestation); we fall through to the first wheel
// when no sdist is present.
func pickPyPIHash(dists []pypiDistribution) *manifest.Hash {
	var sdistDigest, wheelDigest string
	for _, d := range dists {
		sum := strings.TrimSpace(d.Digests["sha256"])
		if sum == "" {
			continue
		}
		switch d.PackageType {
		case "sdist":
			if sdistDigest == "" {
				sdistDigest = sum
			}
		case "bdist_wheel":
			if wheelDigest == "" {
				wheelDigest = sum
			}
		}
	}
	digest := sdistDigest
	if digest == "" {
		digest = wheelDigest
	}
	if digest == "" {
		return nil
	}
	// §8.1: lowercase hex. PyPI already returns lowercase; belt
	// and braces.
	digest = strings.ToLower(digest)
	return &manifest.Hash{Algorithm: "SHA-256", Value: &digest}
}

// pickPyPILicense resolves the most-likely SPDX expression from:
//
//  1. info.license_expression (preferred — PEP 639 gives the
//     producer an explicit SPDX slot).
//  2. info.license mapped through the free-text table
//     (catches "Apache 2.0" → "Apache-2.0" shapes).
//  3. info.license when it already looks like a bare SPDX ID
//     (no spaces, SPDX-charset only).
//  4. classifier mapping as a last resort.
func pickPyPILicense(info pypiInfo) string {
	if expr := strings.TrimSpace(info.LicenseExpr); expr != "" {
		return expr
	}
	if lic := strings.TrimSpace(info.License); lic != "" {
		if mapped := mapPyPILicenseText(lic); mapped != "" {
			return mapped
		}
		if looksLikeSPDXID(lic) {
			return lic
		}
	}
	for _, c := range info.Classifiers {
		if mapped := mapPyPIClassifier(c); mapped != "" {
			return mapped
		}
	}
	return ""
}

// looksLikeSPDXID accepts strings that plausibly are a single SPDX
// identifier (e.g. "Apache-2.0", "MPL-2.0", "ISC"). Strings with
// spaces are rejected — real SPDX expressions with operators go
// through info.license_expression, not info.license.
func looksLikeSPDXID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.' || r == '+':
		default:
			return false
		}
	}
	// Must start with a letter to avoid degenerate strings like
	// "-1.0" or pure punctuation.
	first := s[0]
	isLetter := (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
	return isLetter
}

// licenseNoteWorthy returns true when the PyPI response carries
// license data we couldn't resolve — so we emit a warning — and
// false when it's null / missing and the user wouldn't care.
func licenseNoteWorthy(info pypiInfo) bool {
	if strings.TrimSpace(info.LicenseExpr) != "" {
		return true
	}
	if strings.TrimSpace(info.License) != "" {
		return true
	}
	for _, c := range info.Classifiers {
		if strings.HasPrefix(strings.ToLower(c), "license ::") {
			return true
		}
	}
	return false
}

// mapPyPILicenseText maps free-form PyPI license strings to SPDX IDs.
// Unknown strings return "".
func mapPyPILicenseText(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch l {
	case "mit", "mit license", "the mit license", "the mit license (mit)":
		return "MIT"
	case "apache 2.0", "apache-2.0", "apache license 2.0", "apache license, version 2.0", "apache software license":
		return "Apache-2.0"
	case "bsd", "bsd license", "new bsd license", "bsd 3-clause", "bsd-3-clause", "3-clause bsd", "3-clause bsd license":
		return "BSD-3-Clause"
	case "bsd 2-clause", "bsd-2-clause", "2-clause bsd", "simplified bsd", "simplified bsd license":
		return "BSD-2-Clause"
	case "isc", "isc license":
		return "ISC"
	case "mpl 2.0", "mpl-2.0", "mozilla public license 2.0 (mpl 2.0)", "mozilla public license 2.0":
		return "MPL-2.0"
	case "lgpl", "lgpl-3.0", "lgpl v3", "gnu lesser general public license v3":
		return "LGPL-3.0-only"
	case "gpl", "gpl-3.0", "gpl v3", "gnu general public license v3", "gnu gpl v3":
		return "GPL-3.0-only"
	case "gpl-2.0", "gpl v2", "gnu general public license v2":
		return "GPL-2.0-only"
	case "unlicense", "the unlicense":
		return "Unlicense"
	case "cc0", "cc0 1.0", "cc0-1.0":
		return "CC0-1.0"
	case "python software foundation license", "psf", "psf-2.0", "psfl":
		return "Python-2.0"
	}
	return ""
}

// mapPyPIClassifier maps the `License :: OSI Approved :: ...`
// classifiers to SPDX IDs.
func mapPyPIClassifier(classifier string) string {
	c := strings.ToLower(strings.TrimSpace(classifier))
	if !strings.HasPrefix(c, "license ::") {
		return ""
	}
	switch c {
	case "license :: osi approved :: mit license":
		return "MIT"
	case "license :: osi approved :: apache software license":
		return "Apache-2.0"
	case "license :: osi approved :: bsd license":
		return "BSD-3-Clause"
	case "license :: osi approved :: isc license (iscl)":
		return "ISC"
	case "license :: osi approved :: mozilla public license 2.0 (mpl 2.0)":
		return "MPL-2.0"
	case "license :: osi approved :: gnu general public license v2 (gplv2)":
		return "GPL-2.0-only"
	case "license :: osi approved :: gnu general public license v3 (gplv3)":
		return "GPL-3.0-only"
	case "license :: osi approved :: gnu lesser general public license v2 (lgplv2)":
		return "LGPL-2.1-only"
	case "license :: osi approved :: gnu lesser general public license v3 (lgplv3)":
		return "LGPL-3.0-only"
	case "license :: osi approved :: python software foundation license":
		return "Python-2.0"
	case "license :: cc0 1.0 universal (cc0 1.0) public domain dedication":
		return "CC0-1.0"
	case "license :: public domain":
		return "CC0-1.0"
	}
	return ""
}

func init() {
	Register(&PyPIImporter{})
}
