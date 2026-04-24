// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// buildExternalRefs maps Component identifiers onto SPDX externalRefs
// per §14.2. Some manifest external_references route elsewhere:
//
//   - `external_references[type=website]` → `package.homepage` (not here)
//   - `external_references[type=distribution]` → `package.downloadLocation` (not here)
//
// Everything else, plus the component's `purl` and `cpe`, contributes
// one SPDX externalRefs entry:
//
//   - `purl` → category PACKAGE-MANAGER, type purl
//   - `cpe`  → category SECURITY, type cpe23Type
//   - `external_references[type=vcs]` → category OTHER, type vcs
//   - other types → category OTHER, type other, with a comment
//     preserving the original manifest type
func buildExternalRefs(c *manifest.Component) []spdxExternalRef {
	var out []spdxExternalRef

	if c.Purl != nil && *c.Purl != "" {
		out = append(out, spdxExternalRef{
			ReferenceCategory: "PACKAGE-MANAGER",
			ReferenceType:     "purl",
			ReferenceLocator:  *c.Purl,
		})
	}
	if c.CPE != nil && *c.CPE != "" {
		out = append(out, spdxExternalRef{
			ReferenceCategory: "SECURITY",
			ReferenceType:     "cpe23Type",
			ReferenceLocator:  *c.CPE,
		})
	}
	for _, r := range c.ExternalReferences {
		switch r.Type {
		case "website", "distribution":
			// Routed into package.homepage / downloadLocation upstream.
			continue
		case "vcs":
			out = append(out, spdxExternalRef{
				ReferenceCategory: "OTHER",
				ReferenceType:     "vcs",
				ReferenceLocator:  r.URL,
			})
		default:
			out = append(out, spdxExternalRef{
				ReferenceCategory: "OTHER",
				ReferenceType:     "other",
				ReferenceLocator:  r.URL,
				Comment:           "original type: " + r.Type,
			})
		}
	}
	return out
}
