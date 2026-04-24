// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"errors"
	"fmt"
	"strings"
)

// Schema markers defined by v1 (Spec §4.4).
const (
	SchemaPrimaryV1    = "primary-manifest/v1"
	SchemaComponentsV1 = "component-manifest/v1"
)

// schemaFamily identifies the family prefix of a marker — `primary-manifest/*`
// or `component-manifest/*` — so §4.4's "reject unknown version within a
// known family" rule can be enforced.
type schemaFamily int

const (
	familyNone schemaFamily = iota
	familyPrimary
	familyComponents
)

// ErrNoSchemaMarker indicates a file carries no recognisable schema marker.
// Per §4.4, such a file is not a manifest; discovery (M11) silently ignores
// it while explicit-path parsers turn this into a user-facing error.
var ErrNoSchemaMarker = errors.New("no component-manifest schema marker")

// classifySchemaMarker decides what to do with a marker string.
//
// Returned Kind is KindPrimary / KindComponents on success. An error of
// ErrNoSchemaMarker indicates the file is not a manifest. A non-nil non-
// sentinel error indicates a matched family at an unsupported version — a
// hard rejection per §4.4.
func classifySchemaMarker(marker string) (Kind, error) {
	switch marker {
	case "":
		return KindUnknown, ErrNoSchemaMarker
	case SchemaPrimaryV1:
		return KindPrimary, nil
	case SchemaComponentsV1:
		return KindComponents, nil
	}
	switch familyOf(marker) {
	case familyPrimary:
		return KindUnknown, fmt.Errorf("unsupported primary manifest version %q: only %q is defined by v1", marker, SchemaPrimaryV1)
	case familyComponents:
		return KindUnknown, fmt.Errorf("unsupported components manifest version %q: only %q is defined by v1", marker, SchemaComponentsV1)
	}
	return KindUnknown, ErrNoSchemaMarker
}

func familyOf(marker string) schemaFamily {
	switch {
	case strings.HasPrefix(marker, "primary-manifest/"):
		return familyPrimary
	case strings.HasPrefix(marker, "component-manifest/"):
		return familyComponents
	}
	return familyNone
}
