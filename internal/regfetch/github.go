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

// GitHubImporter pulls component metadata from api.github.com. It
// accepts two input shapes:
//
//   - URL form:
//     https://github.com/<owner>/<repo>
//     https://github.com/<owner>/<repo>/tree/<ref>
//     https://github.com/<owner>/<repo>/releases/tag/<ref>
//   - purl form: pkg:github/<owner>/<repo>[@<ref>]
//
// Network usage is a single GET against /repos/{owner}/{repo}, plus
// one optional GET against /git/ref/tags/{tag} to confirm a specific
// ref exists. No clone, no archive download.
type GitHubImporter struct {
	// BaseURL overrides the default https://api.github.com for tests.
	// BOMTIQUE_GITHUB_BASE_URL env var is consulted as a fallback when
	// BaseURL is empty.
	BaseURL string
}

// Name returns the importer's short label.
func (g *GitHubImporter) Name() string { return "github" }

// Matches returns true when the ref is a GitHub URL or pkg:github
// purl. Cheap string-prefix checks only; no network calls.
func (g *GitHubImporter) Matches(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "https://github.com/") {
		return true
	}
	if strings.HasPrefix(ref, "http://github.com/") {
		return true
	}
	if strings.HasPrefix(ref, "pkg:github/") {
		return true
	}
	return false
}

// Fetch resolves the ref against the GitHub API and returns a
// manifest.Component. Errors wrap the regfetch error sentinels.
func (g *GitHubImporter) Fetch(ctx context.Context, c *Client, ref string) (*manifest.Component, error) {
	owner, repo, tagRef, err := parseGitHubRef(ref)
	if err != nil {
		return nil, err
	}

	base := g.baseURL()
	repoURL := fmt.Sprintf("%s/repos/%s/%s",
		strings.TrimRight(base, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo))

	headers := map[string]string{}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	res, err := c.Get(ctx, repoURL, headers)
	if err != nil {
		return nil, err
	}
	if res.Status == 404 {
		return nil, fmt.Errorf("%w: GitHub repo %s/%s (did you typo owner/repo?)", ErrNotFound, owner, repo)
	}
	if res.Status == 403 && strings.TrimSpace(res.Headers.Get("X-RateLimit-Remaining")) == "0" {
		return nil, fmt.Errorf("%w: GitHub API rate limit hit; set GITHUB_TOKEN to raise the cap", ErrRateLimited)
	}
	if res.Status >= 400 {
		return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, repoURL, res.Status)
	}

	var meta struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		Homepage      string `json:"homepage"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
		License       *struct {
			SPDXID string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := json.Unmarshal(res.Body, &meta); err != nil {
		return nil, fmt.Errorf("%w: decode GitHub repo body: %w", ErrNetwork, err)
	}

	// If the caller supplied a ref, confirm it exists before using it.
	// Otherwise fall back to the default branch and warn the user to
	// pin.
	version := tagRef
	if version == "" {
		version = meta.DefaultBranch
		if version == "" {
			version = "HEAD"
		}
		diag.Warn("github: no ref supplied for %s/%s; using default branch %q — pin via @<ref> for reproducibility", owner, repo, version)
	} else {
		tagURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/tags/%s",
			strings.TrimRight(base, "/"),
			url.PathEscape(owner),
			url.PathEscape(repo),
			url.PathEscape(version))
		tagRes, err := c.Get(ctx, tagURL, headers)
		if err != nil {
			return nil, err
		}
		switch {
		case tagRes.Status == 404:
			return nil, fmt.Errorf("%w: GitHub tag %q not found on %s/%s", ErrNotFound, version, owner, repo)
		case tagRes.Status == 403 && strings.TrimSpace(tagRes.Headers.Get("X-RateLimit-Remaining")) == "0":
			return nil, fmt.Errorf("%w: GitHub API rate limit hit confirming tag %q", ErrRateLimited, version)
		case tagRes.Status >= 400:
			return nil, fmt.Errorf("%w: GET %s returned %d", ErrNetwork, tagURL, tagRes.Status)
		}
	}

	component := &manifest.Component{
		Name:    meta.Name,
		Version: strPtrLocal(version),
		Purl:    strPtrLocal(fmt.Sprintf("pkg:github/%s/%s@%s", owner, repo, version)),
	}
	if meta.Description != "" {
		component.Description = strPtrLocal(meta.Description)
	}
	if meta.License != nil && validSPDXID(meta.License.SPDXID) {
		component.License = &manifest.License{Expression: meta.License.SPDXID}
	}

	if meta.Homepage != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "website", URL: meta.Homepage})
	}
	if meta.HTMLURL != "" {
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "vcs", URL: meta.HTMLURL})
		component.ExternalReferences = append(component.ExternalReferences,
			manifest.ExternalRef{Type: "issue-tracker", URL: meta.HTMLURL + "/issues"})
	}
	return component, nil
}

// baseURL returns the effective API base for the next request.
// Precedence: explicit BaseURL field → BOMTIQUE_GITHUB_BASE_URL env
// → https://api.github.com.
func (g *GitHubImporter) baseURL() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	if env := strings.TrimSpace(os.Getenv("BOMTIQUE_GITHUB_BASE_URL")); env != "" {
		return env
	}
	return "https://api.github.com"
}

// parseGitHubRef teases an owner, repo, and optional ref out of the
// three accepted input shapes.
func parseGitHubRef(raw string) (owner, repo, ref string, err error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "pkg:github/") {
		p, perr := purl.Parse(raw)
		if perr != nil {
			err = fmt.Errorf("%w: bad pkg:github ref %q: %w", ErrUnsupportedRef, raw, perr)
			return
		}
		// Namespace can be a nested path for vendored repos (§9.3);
		// the importer is meant for plain repos, so reject nested.
		if p.Namespace == "" {
			err = fmt.Errorf("%w: pkg:github ref missing namespace: %q", ErrUnsupportedRef, raw)
			return
		}
		if strings.Contains(p.Namespace, "/") {
			err = fmt.Errorf("%w: nested pkg:github ref (repo-local form, §9.3) not importable: %q", ErrUnsupportedRef, raw)
			return
		}
		owner = p.Namespace
		repo = p.Name
		ref = p.Version
		return
	}
	for _, prefix := range []string{"https://github.com/", "http://github.com/"} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		rest := strings.TrimPrefix(raw, prefix)
		rest = strings.Trim(rest, "/")
		parts := strings.Split(rest, "/")
		if len(parts) < 2 {
			err = fmt.Errorf("%w: GitHub URL missing owner/repo: %q", ErrUnsupportedRef, raw)
			return
		}
		owner = parts[0]
		// Strip a trailing .git on the repo segment (`git clone` form).
		repo = strings.TrimSuffix(parts[1], ".git")
		if len(parts) >= 4 {
			switch parts[2] {
			case "tree", "commits", "commit":
				ref = strings.Join(parts[3:], "/")
			case "releases":
				if parts[3] == "tag" && len(parts) >= 5 {
					ref = strings.Join(parts[4:], "/")
				}
			}
		}
		return
	}
	err = fmt.Errorf("%w: %q is not a GitHub URL or pkg:github purl", ErrUnsupportedRef, raw)
	return
}

// validSPDXID screens out the "NOASSERTION" sentinel GitHub uses
// when it can't classify a repo's license. An empty string is
// silently skipped too.
func validSPDXID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if strings.EqualFold(id, "NOASSERTION") {
		return false
	}
	return true
}

func strPtrLocal(s string) *string { return &s }

func init() {
	Register(&GitHubImporter{})
}
