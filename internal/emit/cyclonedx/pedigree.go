// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// buildPedigree mirrors the manifest pedigree onto CycloneDX. Ancestor/
// descendant/variant entries reuse buildComponent so their field
// mapping matches the top-level components (minus scope and tags).
// Patch diff handling follows §9.2 exactly — see buildDiff below.
func buildPedigree(p *manifest.Pedigree, manifestDir string, opts Options) (*cdxPedigree, error) {
	if p == nil {
		return nil, nil
	}
	out := &cdxPedigree{}

	ancestors, err := buildPedigreeComponents(p.Ancestors, manifestDir, opts)
	if err != nil {
		return nil, fmt.Errorf("ancestors: %w", err)
	}
	out.Ancestors = ancestors

	descendants, err := buildPedigreeComponents(p.Descendants, manifestDir, opts)
	if err != nil {
		return nil, fmt.Errorf("descendants: %w", err)
	}
	out.Descendants = descendants

	variants, err := buildPedigreeComponents(p.Variants, manifestDir, opts)
	if err != nil {
		return nil, fmt.Errorf("variants: %w", err)
	}
	out.Variants = variants

	for _, c := range p.Commits {
		entry := cdxCommit{}
		if c.UID != nil {
			entry.UID = *c.UID
		}
		if c.URL != nil {
			entry.URL = *c.URL
		}
		out.Commits = append(out.Commits, entry)
	}

	for i, patch := range p.Patches {
		cdxp, err := buildPatch(patch, manifestDir, opts)
		if err != nil {
			return nil, fmt.Errorf("patches[%d]: %w", i, err)
		}
		out.Patches = append(out.Patches, cdxp)
	}

	if p.Notes != nil {
		out.Notes = *p.Notes
	}

	return out, nil
}

// buildPedigreeComponents applies the component mapping to a slice of
// Ancestor-shaped entries. Pedigree subcomponents don't carry bom-refs
// in the output (CycloneDX doesn't require them inside `pedigree.*`),
// so the derivation used for top-level components is skipped here.
func buildPedigreeComponents(in []manifest.Ancestor, manifestDir string, opts Options) ([]cdxComponent, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]cdxComponent, 0, len(in))
	for _, c := range in {
		comp, err := buildComponent(c, manifestDir, "" /* no bom-ref */, false, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, comp)
	}
	return out, nil
}

// buildPatch maps one patch entry, resolving the diff attachment rules
// from §9.2.
func buildPatch(p manifest.Patch, manifestDir string, opts Options) (cdxPatch, error) {
	out := cdxPatch{Type: p.Type}
	if p.Diff != nil {
		diff, err := buildDiff(p.Diff, manifestDir, opts)
		if err != nil {
			return cdxPatch{}, err
		}
		out.Diff = diff
	}
	for _, r := range p.Resolves {
		entry := cdxResolves{}
		if r.Type != nil {
			entry.Type = *r.Type
		}
		if r.ID != nil {
			entry.ID = *r.ID
		}
		if r.Name != nil {
			entry.Name = *r.Name
		}
		if r.Description != nil {
			entry.Description = *r.Description
		}
		if r.URL != nil {
			entry.URL = *r.URL
		}
		out.Resolves = append(out.Resolves, entry)
	}
	return out, nil
}

// buildDiff applies §9.2's three rules for patch `diff`:
//
//   - A local `url` (relative path): read bytes via safefs, base64, emit
//     as `text = { content: <base64>, encoding: "base64" }`. The url
//     field is dropped from the output — its information now lives in
//     the text field.
//   - An `http://` / `https://` url: preserve as-is; no network fetch.
//   - A bare-string `text` (Attachment.Content set, no encoding /
//     contentType): emit as `text = { content, contentType: "text/plain" }`.
//   - An attachment-form `text` (encoding / contentType set by the
//     producer): preserve all supplied fields verbatim.
//
// When both text and url are present AND the url is http(s), both end
// up in the output.  When both are present AND the url is local, the
// url is consumed — the text field reflects the file bytes.
func buildDiff(d *manifest.Diff, manifestDir string, opts Options) (*cdxDiff, error) {
	out := &cdxDiff{}

	if d.URL != nil && *d.URL != "" {
		url := *d.URL
		if isHTTPURL(url) {
			out.URL = url
		} else {
			data, err := safefs.ReadFile(manifestDir, url, opts.MaxFileSize)
			if err != nil {
				return nil, fmt.Errorf("reading patch diff file %q: %w", url, err)
			}
			out.Text = &cdxAttachment{
				Encoding: "base64",
				Content:  base64.StdEncoding.EncodeToString(data),
			}
			return out, nil
		}
	}

	if d.Text != nil {
		// Plain string form: no encoding / no contentType set on the
		// Attachment struct means the producer wrote `"text": "<bare>"`
		// (M1's UnmarshalJSON hangs the bare string on Content).
		att := *d.Text
		if att.Encoding == nil && att.ContentType == nil {
			out.Text = &cdxAttachment{
				ContentType: "text/plain",
				Content:     att.Content,
			}
		} else {
			out.Text = &cdxAttachment{Content: att.Content}
			if att.Encoding != nil {
				out.Text.Encoding = *att.Encoding
			}
			if att.ContentType != nil {
				out.Text.ContentType = *att.ContentType
			}
		}
	}

	return out, nil
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
