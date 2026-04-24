// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest

import "encoding/json"

// Kind identifies the manifest variant — primary or components.
type Kind int

const (
	KindUnknown Kind = iota
	KindPrimary
	KindComponents
)

func (k Kind) String() string {
	switch k {
	case KindPrimary:
		return "primary"
	case KindComponents:
		return "components"
	}
	return "unknown"
}

// Format identifies the serialization of a manifest file.
type Format int

const (
	FormatUnknown Format = iota
	FormatJSON
	FormatCSV
)

func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatCSV:
		return "csv"
	}
	return "unknown"
}

// Manifest is the top-level result of parsing one manifest file. Exactly one
// of Primary or Components is non-nil, matching Kind.
type Manifest struct {
	Path       string
	Kind       Kind
	Format     Format
	Primary    *PrimaryManifest
	Components *ComponentsManifest
}

// PrimaryManifest is a `primary-manifest/v1` document (Spec §5.1).
type PrimaryManifest struct {
	Schema  string                     `json:"schema"`
	Primary Component                  `json:"primary"`
	Unknown map[string]json.RawMessage `json:"-"`
}

// ComponentsManifest is a `component-manifest/v1` document (Spec §5.2).
type ComponentsManifest struct {
	Schema     string                     `json:"schema"`
	Components []Component                `json:"components"`
	Unknown    map[string]json.RawMessage `json:"-"`
}

// Component is the shared shape used for `primary` and every entry of
// `components[]` (Spec §6). Optional string fields are pointer-valued so
// absence is distinguishable from the empty string — that distinction
// matters for default handling (e.g. `type` defaults to `library`, `scope`
// to `required`) and for round-tripping.
type Component struct {
	BOMRef             *string                    `json:"bom-ref,omitempty"`
	Name               string                     `json:"name"`
	Version            *string                    `json:"version,omitempty"`
	Type               *string                    `json:"type,omitempty"`
	Description        *string                    `json:"description,omitempty"`
	Supplier           *Supplier                  `json:"supplier,omitempty"`
	License            *License                   `json:"license,omitempty"`
	Purl               *string                    `json:"purl,omitempty"`
	CPE                *string                    `json:"cpe,omitempty"`
	ExternalReferences []ExternalRef              `json:"external_references,omitempty"`
	Hashes             []Hash                     `json:"hashes,omitempty"`
	Scope              *string                    `json:"scope,omitempty"`
	Pedigree           *Pedigree                  `json:"pedigree,omitempty"`
	DependsOn          []string                   `json:"depends-on,omitempty"`
	Tags               []string                   `json:"tags,omitempty"`
	Lifecycles         []Lifecycle                `json:"lifecycles,omitempty"`
	Unknown            map[string]json.RawMessage `json:"-"`
}

// Supplier matches the CycloneDX supplier shape (Spec §6.2).
type Supplier struct {
	Name  string  `json:"name"`
	Email *string `json:"email,omitempty"`
	URL   *string `json:"url,omitempty"`
}

// License is the v1 license object (Spec §6.3). Accepts either a bare
// string (shorthand for `{ "expression": "<str>" }`) or the object form.
type License struct {
	Expression string        `json:"expression"`
	Texts      []LicenseText `json:"texts,omitempty"`
}

// LicenseText is a per-license-id text reference under License.Texts.
// Exactly one of Text / File is required per entry (Spec §6.3).
type LicenseText struct {
	ID   string  `json:"id"`
	Text *string `json:"text,omitempty"`
	File *string `json:"file,omitempty"`
}

// Hash is one entry of a component's `hashes[]` (Spec §8). It takes one of
// three forms — literal (Value), file (File), or directory/path (Path
// with optional Extensions). M1 captures the raw shape; M4 enforces the
// one-form-per-entry rule.
type Hash struct {
	Algorithm  string   `json:"algorithm"`
	Value      *string  `json:"value,omitempty"`
	File       *string  `json:"file,omitempty"`
	Path       *string  `json:"path,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

// ExternalRef matches CycloneDX externalReferences[] (Spec §7.3).
type ExternalRef struct {
	Type    string  `json:"type"`
	URL     string  `json:"url"`
	Comment *string `json:"comment,omitempty"`
}

// Pedigree declares provenance for a vendored or modified component
// (Spec §9). Its shape mirrors CycloneDX 1.6 `pedigree`.
type Pedigree struct {
	Ancestors   []Ancestor `json:"ancestors,omitempty"`
	Descendants []Ancestor `json:"descendants,omitempty"`
	Variants    []Ancestor `json:"variants,omitempty"`
	Commits     []Commit   `json:"commits,omitempty"`
	Patches     []Patch    `json:"patches,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
}

// Ancestor is structurally identical to Component (Spec §9.1).
type Ancestor = Component

// Patch is one entry of `pedigree.patches[]` (Spec §9.2).
type Patch struct {
	Type     string     `json:"type"`
	Diff     *Diff      `json:"diff,omitempty"`
	Resolves []Resolves `json:"resolves,omitempty"`
}

// Diff holds a patch's content (Spec §9.2). The `text` JSON field accepts
// either a bare string or a CycloneDX Attachment; both forms collapse to
// an Attachment here, with the bare string stored as Text.Content and no
// encoding/contentType.
type Diff struct {
	Text *Attachment `json:"text,omitempty"`
	URL  *string     `json:"url,omitempty"`
}

// Attachment matches the CycloneDX attachment shape (Spec §9.2).
type Attachment struct {
	Content     string  `json:"content"`
	Encoding    *string `json:"encoding,omitempty"`
	ContentType *string `json:"contentType,omitempty"`
}

// Commit captures a pedigree commit reference (Spec §9).
type Commit struct {
	UID *string `json:"uid,omitempty"`
	URL *string `json:"url,omitempty"`
}

// Resolves captures an advisory/issue referenced by a patch (Spec §9.2).
type Resolves struct {
	Type        *string `json:"type,omitempty"`
	ID          *string `json:"id,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	URL         *string `json:"url,omitempty"`
}

// Lifecycle is one entry of a primary's `lifecycles[]` (Spec §7.5).
type Lifecycle struct {
	Phase string `json:"phase"`
}
