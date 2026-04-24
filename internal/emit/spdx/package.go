// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/hash"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// buildPackage projects one manifest.Component onto an spdxPackage per
// §14.2's mapping table. The component can be either the primary or a
// pool entry — all field rules are package-level. `manifestDir` is the
// directory the component came from, used for license.texts[].file
// reads.
//
// Dropped-field classes register on `drops`; the caller emits a single
// warning per class after processing every package.
func buildPackage(c *manifest.Component, manifestDir, id string, drops *droppedCounter, opts Options) (spdxPackage, error) {
	pkg := spdxPackage{
		SPDXID:           id,
		Name:             c.Name,
		FilesAnalyzed:    boolPtr(false),
		DownloadLocation: noAssertion,
	}
	if c.Version != nil {
		pkg.VersionInfo = *c.Version
	}
	if c.Description != nil {
		pkg.Description = *c.Description
	}
	pkg.PrimaryPackagePurpose = mapType(c.Type)
	pkg.Supplier = formatSupplier(c.Supplier)

	// homepage + downloadLocation come from external_references before
	// the rest of the ref set is expanded.
	pkg.Homepage = findExternalRef(c.ExternalReferences, "website")
	if dl := findExternalRef(c.ExternalReferences, "distribution"); dl != "" {
		pkg.DownloadLocation = dl
	}

	pkg.LicenseConcluded, pkg.LicenseDeclared = mapLicense(c.License)
	comments, err := buildLicenseComments(c.License, manifestDir, opts)
	if err != nil {
		return spdxPackage{}, fmt.Errorf("license texts: %w", err)
	}
	pkg.LicenseComments = comments

	checksums, err := buildChecksums(c.Hashes, manifestDir, opts)
	if err != nil {
		return spdxPackage{}, err
	}
	pkg.Checksums = checksums

	pkg.ExternalRefs = buildExternalRefs(c)

	if c.Pedigree != nil {
		applyPedigree(&pkg, c.Pedigree, drops, timestampNow(), toolCreator(opts.ToolVersion))
	}

	if c.Scope != nil && *c.Scope != "" {
		drops.scope()
	}

	return pkg, nil
}

// timestampNow is split out so tests that mock time (via the SDE path)
// still see stable annotations. It matches the §15.3 formatting.
// Currently it just forwards to time.Now — annotations don't use the
// SDE override; that's a future tightening if needed.
func timestampNow() string {
	return timestamp(nil)
}

func boolPtr(b bool) *bool { return &b }

// mapType projects Component.type onto SPDX primaryPackagePurpose per
// §14.2. Unlisted values fold to OTHER.
func mapType(t *string) string {
	if t == nil || *t == "" {
		return "LIBRARY" // default per §7.1
	}
	switch *t {
	case "library":
		return "LIBRARY"
	case "application":
		return "APPLICATION"
	case "framework":
		return "FRAMEWORK"
	case "container":
		return "CONTAINER"
	case "operating-system":
		return "OPERATING-SYSTEM"
	case "device":
		return "DEVICE"
	case "firmware":
		return "FIRMWARE"
	case "file":
		return "FILE"
	}
	// platform, device-driver, machine-learning-model, data — §14.2
	// projects these to OTHER.
	return "OTHER"
}

// formatSupplier renders Supplier into SPDX's "Organization: <name>
// (<email>)" string shape. Empty supplier returns "".
func formatSupplier(s *manifest.Supplier) string {
	if s == nil || strings.TrimSpace(s.Name) == "" {
		return ""
	}
	out := "Organization: " + strings.TrimSpace(s.Name)
	if s.Email != nil && *s.Email != "" {
		out += " (" + *s.Email + ")"
	}
	return out
}

// findExternalRef returns the first url for the given manifest type, or
// "" when none is present.
func findExternalRef(refs []manifest.ExternalRef, wantType string) string {
	for _, r := range refs {
		if r.Type == wantType {
			return r.URL
		}
	}
	return ""
}

// mapLicense returns (licenseConcluded, licenseDeclared). Per §14.2
// both get the same expression verbatim. An empty license collapses to
// NOASSERTION on both.
func mapLicense(l *manifest.License) (string, string) {
	if l == nil || strings.TrimSpace(l.Expression) == "" {
		return noAssertion, noAssertion
	}
	return l.Expression, l.Expression
}

// buildChecksums maps Component.Hashes to SPDX Checksums, translating
// algorithm names from the manifest form to SPDX's form and computing
// file/path digests on demand.
func buildChecksums(in []manifest.Hash, manifestDir string, opts Options) ([]spdxChecksum, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]spdxChecksum, 0, len(in))
	for i, h := range in {
		alg, err := hash.Parse(h.Algorithm)
		if err != nil {
			return nil, fmt.Errorf("hashes[%d]: %w", i, err)
		}
		var value string
		switch {
		case h.Value != nil && *h.Value != "":
			value = *h.Value
		case h.File != nil && *h.File != "":
			v, err := hash.File(manifestDir, *h.File, alg, opts.MaxFileSize)
			if err != nil {
				return nil, fmt.Errorf("hashes[%d]: %w", i, err)
			}
			value = v
		case h.Path != nil && *h.Path != "":
			v, err := hash.Directory(manifestDir, *h.Path, alg, h.Extensions, opts.MaxFileSize)
			if err != nil {
				return nil, fmt.Errorf("hashes[%d]: %w", i, err)
			}
			value = v
		default:
			return nil, fmt.Errorf("hashes[%d]: entry has no value, file, or path", i)
		}
		out = append(out, spdxChecksum{
			Algorithm:     spdxAlgorithmName(alg),
			ChecksumValue: value,
		})
	}
	return out, nil
}

// spdxAlgorithmName translates bomtique's canonical §8.1 algorithm name
// to the SPDX 2.3 spelling.  For SHA-256/384/512 the form is uppercase
// SHAn (no hyphen). SHA-3 family drops the inner hyphen too:
// `SHA-3-256` → `SHA3-256`.
func spdxAlgorithmName(a hash.Algorithm) string {
	switch a {
	case hash.SHA256:
		return "SHA256"
	case hash.SHA384:
		return "SHA384"
	case hash.SHA512:
		return "SHA512"
	case hash.SHA3_256:
		return "SHA3-256"
	case hash.SHA3_512:
		return "SHA3-512"
	}
	return a.SpecName()
}
