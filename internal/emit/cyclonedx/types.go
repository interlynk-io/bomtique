// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

// CycloneDX 1.7 JSON types, struct-per-field (no maps) so Go's default
// json.Marshal produces stable field ordering. Only the subset the spec
// §14.1 mapping table exercises is modelled; adding fields later is a
// matter of growing the struct, not rewriting the emitter.
//
// Field ordering within each struct matches the CycloneDX schema's
// documented ordering where practical, with `omitempty` on every
// optional scalar / slice / object. The public constant BOMFormat
// / SpecVersion are CycloneDX 1.7.

const (
	bomFormat   = "CycloneDX"
	specVersion = "1.7"
)

// Top-level BOM document.
type cdxBOM struct {
	BOMFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	SerialNumber string          `json:"serialNumber,omitempty"`
	Version      int             `json:"version"`
	Metadata     *cdxMetadata    `json:"metadata,omitempty"`
	Components   []cdxComponent  `json:"components,omitempty"`
	Dependencies []cdxDependency `json:"dependencies,omitempty"`
}

type cdxMetadata struct {
	Timestamp  string         `json:"timestamp,omitempty"`
	Lifecycles []cdxLifecycle `json:"lifecycles,omitempty"`
	Component  *cdxComponent  `json:"component,omitempty"`
}

type cdxLifecycle struct {
	Phase string `json:"phase"`
}

type cdxComponent struct {
	BOMRef             string           `json:"bom-ref,omitempty"`
	Type               string           `json:"type,omitempty"`
	Name               string           `json:"name"`
	Version            string           `json:"version,omitempty"`
	Description        string           `json:"description,omitempty"`
	Scope              string           `json:"scope,omitempty"`
	Supplier           *cdxSupplier     `json:"supplier,omitempty"`
	Licenses           []cdxLicense     `json:"licenses,omitempty"`
	Purl               string           `json:"purl,omitempty"`
	CPE                string           `json:"cpe,omitempty"`
	ExternalReferences []cdxExternalRef `json:"externalReferences,omitempty"`
	Hashes             []cdxHash        `json:"hashes,omitempty"`
	Pedigree           *cdxPedigree     `json:"pedigree,omitempty"`
}

type cdxSupplier struct {
	Name    string       `json:"name,omitempty"`
	URL     []string     `json:"url,omitempty"`
	Contact []cdxContact `json:"contact,omitempty"`
}

type cdxContact struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// cdxLicense holds one entry of CycloneDX's `licenses[]`. The schema
// admits either an `expression` string or an object form with a nested
// `license` record carrying an SPDX `id` and an optional attachment.
// For a component that carries both a compound expression and
// per-identifier texts[], the emitter outputs both shapes in the same
// array: one `{ expression }` entry plus one `{ license: { id, text } }`
// entry per texts[].
type cdxLicense struct {
	License    *cdxLicenseEntry `json:"license,omitempty"`
	Expression string           `json:"expression,omitempty"`
}

type cdxLicenseEntry struct {
	ID   string         `json:"id"`
	Text *cdxAttachment `json:"text,omitempty"`
}

type cdxAttachment struct {
	ContentType string `json:"contentType,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Content     string `json:"content,omitempty"`
}

type cdxHash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

type cdxExternalRef struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Comment string `json:"comment,omitempty"`
}

type cdxPedigree struct {
	Ancestors   []cdxComponent `json:"ancestors,omitempty"`
	Descendants []cdxComponent `json:"descendants,omitempty"`
	Variants    []cdxComponent `json:"variants,omitempty"`
	Commits     []cdxCommit    `json:"commits,omitempty"`
	Patches     []cdxPatch     `json:"patches,omitempty"`
	Notes       string         `json:"notes,omitempty"`
}

type cdxCommit struct {
	UID string `json:"uid,omitempty"`
	URL string `json:"url,omitempty"`
}

type cdxPatch struct {
	Type     string        `json:"type,omitempty"`
	Diff     *cdxDiff      `json:"diff,omitempty"`
	Resolves []cdxResolves `json:"resolves,omitempty"`
}

type cdxDiff struct {
	Text *cdxAttachment `json:"text,omitempty"`
	URL  string         `json:"url,omitempty"`
}

type cdxResolves struct {
	Type        string `json:"type,omitempty"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

type cdxDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn,omitempty"`
}
