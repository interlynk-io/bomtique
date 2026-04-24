// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// FieldOverride describes a single field that the override Component
// provided a non-zero value for, overriding the base. Consumers use
// it to emit stderr lines along the lines of
// "flag --name overrode file field name".
type FieldOverride struct {
	// Field is the manifest field name (matching §6.2 / JSON keys).
	Field string
}

// MergeComponent combines a base Component (typically read from a
// --from file or an existing manifest entry) with an overrides
// Component (typically assembled from command-line flags). Overrides
// fully replace corresponding base fields when present; nothing is
// merged element-wise.
//
// Presence semantics:
//   - Required scalar fields (Name): treated as overriding when the
//     override carries a non-empty value.
//   - Optional pointer fields (*string, *Supplier, *License, *Pedigree):
//     overriding when non-nil.
//   - Slice fields (ExternalReferences, Hashes, DependsOn, Tags,
//     Lifecycles): overriding when non-nil (len==0 counts as a clear
//     intent when the override slice was explicitly allocated empty,
//     but callers should prefer dedicated --clear-<field> flags for
//     that semantic when the field-level API needs it).
//   - Unknown: overriding when the map is non-nil.
//
// The returned []FieldOverride lists every field the override layer
// replaced. Order mirrors the field declaration order in
// internal/manifest/types.go so diagnostics are deterministic.
func MergeComponent(base, overrides *manifest.Component) (*manifest.Component, []FieldOverride) {
	if base == nil && overrides == nil {
		return nil, nil
	}
	if base == nil {
		clone := *overrides
		return &clone, nil
	}
	if overrides == nil {
		clone := *base
		return &clone, nil
	}

	out := *base
	var touched []FieldOverride

	if overrides.BOMRef != nil {
		out.BOMRef = overrides.BOMRef
		touched = append(touched, FieldOverride{Field: "bom-ref"})
	}
	if overrides.Name != "" && overrides.Name != base.Name {
		out.Name = overrides.Name
		touched = append(touched, FieldOverride{Field: "name"})
	}
	if overrides.Version != nil {
		out.Version = overrides.Version
		touched = append(touched, FieldOverride{Field: "version"})
	}
	if overrides.Type != nil {
		out.Type = overrides.Type
		touched = append(touched, FieldOverride{Field: "type"})
	}
	if overrides.Description != nil {
		out.Description = overrides.Description
		touched = append(touched, FieldOverride{Field: "description"})
	}
	if overrides.Supplier != nil {
		out.Supplier = overrides.Supplier
		touched = append(touched, FieldOverride{Field: "supplier"})
	}
	if overrides.License != nil {
		out.License = overrides.License
		touched = append(touched, FieldOverride{Field: "license"})
	}
	if overrides.Purl != nil {
		out.Purl = overrides.Purl
		touched = append(touched, FieldOverride{Field: "purl"})
	}
	if overrides.CPE != nil {
		out.CPE = overrides.CPE
		touched = append(touched, FieldOverride{Field: "cpe"})
	}
	if overrides.ExternalReferences != nil {
		out.ExternalReferences = overrides.ExternalReferences
		touched = append(touched, FieldOverride{Field: "external_references"})
	}
	if overrides.Hashes != nil {
		out.Hashes = overrides.Hashes
		touched = append(touched, FieldOverride{Field: "hashes"})
	}
	if overrides.Scope != nil {
		out.Scope = overrides.Scope
		touched = append(touched, FieldOverride{Field: "scope"})
	}
	if overrides.Pedigree != nil {
		out.Pedigree = overrides.Pedigree
		touched = append(touched, FieldOverride{Field: "pedigree"})
	}
	if overrides.DependsOn != nil {
		out.DependsOn = overrides.DependsOn
		touched = append(touched, FieldOverride{Field: "depends-on"})
	}
	if overrides.Tags != nil {
		out.Tags = overrides.Tags
		touched = append(touched, FieldOverride{Field: "tags"})
	}
	if overrides.Lifecycles != nil {
		out.Lifecycles = overrides.Lifecycles
		touched = append(touched, FieldOverride{Field: "lifecycles"})
	}
	if overrides.Unknown != nil {
		out.Unknown = overrides.Unknown
		touched = append(touched, FieldOverride{Field: "unknown"})
	}
	return &out, touched
}
