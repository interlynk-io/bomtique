// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"encoding/base64"
	"fmt"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// buildLicenses maps Component.License onto the CycloneDX `licenses[]`
// array per §14.1:
//
//   - A single `{ expression: "<expr>" }` entry always, when expression
//     is non-empty.
//   - One `{ license: { id, text? } }` entry per license.texts[] —
//     inline `text` → `{ content, contentType: "text/plain" }`;
//     `file` → read via safefs, base64-encode the bytes, emit
//     `{ content: <base64>, encoding: "base64", contentType: "text/plain" }`.
//
// Emission of both the expression and the per-id entries matches
// §14.1's intent: the expression captures compound licensing while the
// per-id entries attach the actual text.
func buildLicenses(l *manifest.License, manifestDir string, opts Options) ([]cdxLicense, error) {
	if l == nil || l.Expression == "" {
		return nil, nil
	}
	out := make([]cdxLicense, 0, 1+len(l.Texts))
	out = append(out, cdxLicense{Expression: l.Expression})

	for _, t := range l.Texts {
		att, err := readLicenseTextAttachment(t, manifestDir, opts)
		if err != nil {
			return nil, fmt.Errorf("license text %q: %w", t.ID, err)
		}
		out = append(out, cdxLicense{
			License: &cdxLicenseEntry{ID: t.ID, Text: att},
		})
	}
	return out, nil
}

// readLicenseTextAttachment materialises one LicenseText entry. Exactly
// one of Text / File is expected to be set (M4 validation rejects any
// other shape). A missing attachment returns nil so the caller can
// still emit a bare `{ license: { id } }` entry with no text.
func readLicenseTextAttachment(t manifest.LicenseText, manifestDir string, opts Options) (*cdxAttachment, error) {
	if t.Text != nil {
		return &cdxAttachment{
			ContentType: "text/plain",
			Content:     *t.Text,
		}, nil
	}
	if t.File != nil {
		data, err := safefs.ReadFile(manifestDir, *t.File, opts.MaxFileSize)
		if err != nil {
			return nil, err
		}
		return &cdxAttachment{
			ContentType: "text/plain",
			Encoding:    "base64",
			Content:     base64.StdEncoding.EncodeToString(data),
		}, nil
	}
	return nil, nil
}
