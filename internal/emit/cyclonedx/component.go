// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// buildComponent maps a manifest.Component onto its CycloneDX-shaped
// counterpart. `isPrimary` controls §5.3's "scope and tags are ignored
// on the primary" rule: primary components never carry scope; tags are
// never serialized on either side (§14.1).
func buildComponent(src manifest.Component, manifestDir, bomRef string, isPrimary bool, opts Options) (cdxComponent, error) {
	out := cdxComponent{
		BOMRef: bomRef,
		Type:   ptrValueOr(src.Type, "library"),
		Name:   src.Name,
	}

	if src.Version != nil {
		out.Version = *src.Version
	}
	if src.Description != nil {
		out.Description = *src.Description
	}
	if !isPrimary && src.Scope != nil && *src.Scope != "" {
		out.Scope = *src.Scope
	}
	if src.Purl != nil {
		out.Purl = *src.Purl
	}
	if src.CPE != nil {
		out.CPE = *src.CPE
	}
	if src.Supplier != nil {
		out.Supplier = buildSupplier(src.Supplier)
	}

	licenses, err := buildLicenses(src.License, manifestDir, opts)
	if err != nil {
		return cdxComponent{}, err
	}
	out.Licenses = licenses

	out.ExternalReferences = buildExternalRefs(src.ExternalReferences)

	hashes, err := buildHashes(src.Hashes, manifestDir, opts)
	if err != nil {
		return cdxComponent{}, err
	}
	out.Hashes = hashes

	if src.Pedigree != nil {
		p, err := buildPedigree(src.Pedigree, manifestDir, opts)
		if err != nil {
			return cdxComponent{}, err
		}
		out.Pedigree = p
	}

	// Tags are NEVER serialized (§14.1 "tags | not serialized").
	return out, nil
}

func buildSupplier(s *manifest.Supplier) *cdxSupplier {
	if s == nil {
		return nil
	}
	out := &cdxSupplier{Name: s.Name}
	if s.URL != nil && *s.URL != "" {
		out.URL = []string{*s.URL}
	}
	if s.Email != nil && *s.Email != "" {
		out.Contact = []cdxContact{{Email: *s.Email}}
	}
	return out
}

func buildExternalRefs(in []manifest.ExternalRef) []cdxExternalRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]cdxExternalRef, 0, len(in))
	for _, r := range in {
		entry := cdxExternalRef{Type: r.Type, URL: r.URL}
		if r.Comment != nil {
			entry.Comment = *r.Comment
		}
		out = append(out, entry)
	}
	return out
}

func ptrValueOr(p *string, fallback string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallback
}
