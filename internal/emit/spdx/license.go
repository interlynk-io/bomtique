// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// buildLicenseComments assembles the `licenseComments` field by
// concatenating every license.texts[] entry's content under an
// `id`-label heading. §14.2 calls this an "approximated" mapping
// because SPDX 2.3 has no per-identifier license-text attachment slot.
//
// Order in the output follows `texts[]` order; callers that rely on
// byte-stable output feed already-ordered manifest data.
func buildLicenseComments(l *manifest.License, manifestDir string, opts Options) (string, error) {
	if l == nil || len(l.Texts) == 0 {
		return "", nil
	}
	var b strings.Builder
	for i, t := range l.Texts {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "=== %s ===\n", t.ID)

		switch {
		case t.Text != nil && *t.Text != "":
			b.WriteString(*t.Text)
		case t.File != nil && *t.File != "":
			data, err := safefs.ReadFile(manifestDir, *t.File, opts.MaxFileSize)
			if err != nil {
				return "", fmt.Errorf("%s: %w", *t.File, err)
			}
			b.Write(data)
		default:
			// M4 validation rejects entries with neither text nor file;
			// this path is defensive.
			b.WriteString("(no text)")
		}
	}
	return b.String(), nil
}
