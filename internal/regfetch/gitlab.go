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

// GitLabImporter pulls component metadata from a GitLab instance
// (gitlab.com by default; self-hosted hosts via BaseURL or
// BOMTIQUE_GITLAB_BASE_URL).
//
// Accepted inputs:
//   - https://<host>/<group>[/<subgroup>...]/<project>[/-/tree/<ref>|/-/tags/<ref>]
//   - pkg:gitlab/<group>[/<subgroup>...]/<project>[@<ref>]
//
// Network use is one GET against /api/v4/projects/<url-encoded-path>
// plus one optional GET against /repository/tags/<tag>.
type GitLabImporter struct {
	// BaseURL overrides the default https://gitlab.com for tests or
	// self-hosted instances. BOMTIQUE_GITLAB_BASE_URL is consulted
	// when BaseURL is empty.
	BaseURL string
}

// Name is the importer's short label.
func (g *GitLabImporter) Name() string { return "gitlab" }

// Matches returns true for gitlab URLs or pkg:gitlab purls. Any host
// other than gitlab.com is accepted too when BaseURL or the env var
// points at it — the Matches check itself can't know about the
// configured host, so the URL form only matches gitlab.com by
// default. Self-hosted users pass refs as pkg:gitlab to avoid
// ambiguity.
func (g *GitLabImporter) Matches(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "pkg:gitlab/") {
		return true
	}
	if strings.HasPrefix(ref, "https://gitlab.com/") {
		return true
	}
	if strings.HasPrefix(ref, "http://gitlab.com/") {
		return true
	}
	return false
}

// Fetch resolves the ref against the GitLab API and returns a
// manifest.Component.
func (g *GitLabImporter) Fetch(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
	projectPath, tagRef, err := parseGitLabRef(ref)
	if err != nil {
		return nil, err
	}

	base := g.baseURL()
	encodedPath := url.QueryEscape(projectPath)
	projectURL := fmt.Sprintf("%s/api/v4/projects/%s",
		strings.TrimRight(base, "/"), encodedPath)

	headers := map[string]string{}
	if token := strings.TrimSpace(os.Getenv("GITLAB_TOKEN")); token != "" {
		headers["PRIVATE-TOKEN"] = token
	}

	res, err := c.Get(ctx, projectURL, headers)
	if err != nil {
		return nil, err
	}
	if res.Status == 404 {
		return nil, fmt.Errorf("%w: GitLab project %q (did you typo the namespace/project path?)", ErrNotFound, projectPath)
	}
	if res.Status == 403 && isGitLabRateLimited(res) {
		return nil, fmt.Errorf("%w: GitLab API rate limit hit; set GITLAB_TOKEN to raise the cap", ErrRateLimited)
	}
	if res.Status == 429 {
		return nil, fmt.Errorf("%w: GitLab returned 429 Too Many Requests; set GITLAB_TOKEN or retry later", ErrRateLimited)
	}
	if res.Status >= 400 {
		return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, projectURL, res.Status)
	}

	var meta struct {
		Name          string `json:"name"`
		Path          string `json:"path"`
		PathWithNS    string `json:"path_with_namespace"`
		Description   string `json:"description"`
		Homepage      string `json:"homepage"`
		WebURL        string `json:"web_url"`
		HTTPURLToRepo string `json:"http_url_to_repo"`
		DefaultBranch string `json:"default_branch"`
		License       *struct {
			Key      string `json:"key"`
			Name     string `json:"name"`
			Nickname string `json:"nickname"`
		} `json:"license"`
	}
	if err := json.Unmarshal(res.Body, &meta); err != nil {
		return nil, fmt.Errorf("%w: decode GitLab project body: %w", ErrNetwork, err)
	}

	// Resolve version: explicit tag → confirm exists; else default branch.
	version := tagRef
	if version == "" {
		version = meta.DefaultBranch
		if version == "" {
			version = "HEAD"
		}
		diag.Warn("gitlab: no ref supplied for %s; using default branch %q — pin via @<ref> for reproducibility", projectPath, version)
	} else {
		tagURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/tags/%s",
			strings.TrimRight(base, "/"), encodedPath, url.PathEscape(version))
		tagRes, err := c.Get(ctx, tagURL, headers)
		if err != nil {
			return nil, err
		}
		switch {
		case tagRes.Status == 404:
			return nil, fmt.Errorf("%w: GitLab tag %q not found on %s", ErrNotFound, version, projectPath)
		case tagRes.Status == 403 && isGitLabRateLimited(tagRes):
			return nil, fmt.Errorf("%w: GitLab API rate limit hit confirming tag %q", ErrRateLimited, version)
		case tagRes.Status == 429:
			return nil, fmt.Errorf("%w: GitLab 429 confirming tag %q", ErrRateLimited, version)
		case tagRes.Status >= 400:
			return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, tagURL, tagRes.Status)
		}
	}

	// Use the API's reported name when available; fall back to the
	// last path segment so we never emit an empty Component.Name.
	projectName := strings.TrimSpace(meta.Name)
	if projectName == "" {
		projectName = strings.TrimSpace(meta.Path)
	}
	if projectName == "" {
		segments := strings.Split(projectPath, "/")
		projectName = segments[len(segments)-1]
	}

	component := &manifest.Component{
		Name:    projectName,
		Version: strPtrLocal(version),
		Purl:    strPtrLocal(fmt.Sprintf("pkg:gitlab/%s@%s", projectPath, version)),
	}
	if meta.Description != "" {
		component.Description = strPtrLocal(meta.Description)
	}
	if meta.License != nil {
		if spdx := mapGitLabLicenseKey(meta.License.Key); spdx != "" {
			component.License = &manifest.License{Expression: spdx}
		} else if strings.TrimSpace(meta.License.Key) != "" {
			diag.Warn("gitlab: license key %q from %s is not a known SPDX expression; dropping license field (pass --license explicitly)", meta.License.Key, projectPath)
		}
	}

	if meta.Homepage != "" && meta.Homepage != meta.WebURL {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "website", URL: meta.Homepage})
	}
	if meta.WebURL != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "vcs", URL: meta.WebURL})
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "issue-tracker", URL: meta.WebURL + "/-/issues"})
	}
	if meta.HTTPURLToRepo != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "distribution", URL: meta.HTTPURLToRepo})
	}
	return component, nil
}

// baseURL returns the effective GitLab API base. Precedence:
// explicit BaseURL → BOMTIQUE_GITLAB_BASE_URL env → gitlab.com.
func (g *GitLabImporter) baseURL() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	if env := strings.TrimSpace(os.Getenv("BOMTIQUE_GITLAB_BASE_URL")); env != "" {
		return env
	}
	return "https://gitlab.com"
}

// parseGitLabRef extracts the project path and optional tag from
// either a GitLab URL or a pkg:gitlab purl. The project path is
// returned unencoded (e.g. "group/subgroup/project"); Fetch URL-
// encodes it before putting it in the request path.
func parseGitLabRef(raw string) (projectPath, tagRef string, err error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "pkg:gitlab/") {
		p, perr := purl.Parse(raw)
		if perr != nil {
			err = fmt.Errorf("%w: bad pkg:gitlab ref %q: %w", ErrUnsupportedRef, raw, perr)
			return
		}
		if p.Namespace == "" {
			err = fmt.Errorf("%w: pkg:gitlab ref missing namespace: %q", ErrUnsupportedRef, raw)
			return
		}
		projectPath = p.Namespace + "/" + p.Name
		tagRef = p.Version
		return
	}
	for _, prefix := range []string{"https://gitlab.com/", "http://gitlab.com/"} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		rest := strings.TrimPrefix(raw, prefix)
		rest = strings.Trim(rest, "/")
		if rest == "" {
			err = fmt.Errorf("%w: GitLab URL missing project path: %q", ErrUnsupportedRef, raw)
			return
		}
		// Strip a trailing .git on the final segment so clone URLs
		// work.
		parts := strings.Split(rest, "/")
		// GitLab uses `/-/` as a delimiter between the project path
		// and repo operations. Everything before the `/-/` is the
		// project path; the segment after identifies the operation.
		pathParts := parts
		opParts := []string{}
		for i, p := range parts {
			if p == "-" {
				pathParts = parts[:i]
				opParts = parts[i+1:]
				break
			}
		}
		if len(pathParts) < 2 {
			err = fmt.Errorf("%w: GitLab URL needs at least <group>/<project>: %q", ErrUnsupportedRef, raw)
			return
		}
		pathParts[len(pathParts)-1] = strings.TrimSuffix(pathParts[len(pathParts)-1], ".git")
		projectPath = strings.Join(pathParts, "/")

		// Pick a ref from /tree/<x>, /tags/<x>, or /commits/<x>.
		if len(opParts) >= 2 {
			switch opParts[0] {
			case "tree", "tags", "commits", "commit":
				tagRef = strings.Join(opParts[1:], "/")
			}
		}
		return
	}
	err = fmt.Errorf("%w: %q is not a GitLab URL or pkg:gitlab purl", ErrUnsupportedRef, raw)
	return
}

// isGitLabRateLimited decides whether a 403 really means "rate
// limited" vs "permission denied". GitLab exposes RateLimit-Remaining
// headers on throttled responses.
func isGitLabRateLimited(res *Response) bool {
	if strings.TrimSpace(res.Headers.Get("RateLimit-Remaining")) == "0" {
		return true
	}
	if strings.TrimSpace(res.Headers.Get("RateLimit-Limit")) != "" &&
		strings.EqualFold(res.Headers.Get("RateLimit-Remaining"), "0") {
		return true
	}
	return false
}

// mapGitLabLicenseKey maps a lowercase GitLab license key to a valid
// SPDX expression. Unknown keys return "" and the caller drops the
// license field with a warning.
func mapGitLabLicenseKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "apache-2.0":
		return "Apache-2.0"
	case "mit":
		return "MIT"
	case "bsd-3-clause":
		return "BSD-3-Clause"
	case "bsd-2-clause":
		return "BSD-2-Clause"
	case "0bsd":
		return "0BSD"
	case "gpl-2.0":
		return "GPL-2.0-only"
	case "gpl-3.0":
		return "GPL-3.0-only"
	case "lgpl-2.1":
		return "LGPL-2.1-only"
	case "lgpl-3.0":
		return "LGPL-3.0-only"
	case "mpl-2.0":
		return "MPL-2.0"
	case "agpl-3.0":
		return "AGPL-3.0-only"
	case "unlicense":
		return "Unlicense"
	case "cc0-1.0":
		return "CC0-1.0"
	case "isc":
		return "ISC"
	case "epl-2.0":
		return "EPL-2.0"
	case "wtfpl":
		return "WTFPL"
	}
	return ""
}

func init() {
	Register(&GitLabImporter{})
}
