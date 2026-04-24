// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package schemas vendors the JSON Schema documents the CycloneDX 1.7
// and SPDX 2.3 emitters validate their output against. Both schema
// bundles are embedded via `embed.FS` so the binary is self-contained
// — `--output-validate` works with no network access at runtime,
// matching spec §18.3.
//
// The schemas are fetched verbatim from their upstream publishers:
//
//   - CycloneDX 1.7: https://cyclonedx.org/schema/bom-1.7.schema.json
//     (plus spdx.schema.json, jsf-0.82.schema.json,
//     cryptography-defs.schema.json — all referenced via `$ref`).
//   - SPDX 2.3: https://raw.githubusercontent.com/spdx/spdx-spec/v2.3/
//     schemas/spdx-schema.json
//
// Refresh by re-running the fetch commands; see the Makefile `schemas`
// target if one is added later.
package schemas

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed cyclonedx/*.json spdx/*.json
var vendored embed.FS

// CycloneDX17 returns the vendored CycloneDX 1.7 schema set as an
// fs.FS rooted at `cyclonedx/`. The top-level schema is
// `bom-1.7.schema.json`; its `$ref`s resolve against sibling files in
// the same directory.
func CycloneDX17() (fs.FS, string, error) {
	sub, err := fs.Sub(vendored, "cyclonedx")
	if err != nil {
		return nil, "", fmt.Errorf("schemas: cyclonedx subtree: %w", err)
	}
	return sub, "bom-1.7.schema.json", nil
}

// SPDX23 returns the vendored SPDX 2.3 schema set. The top-level
// schema is `spdx-schema.json`; it has no external `$ref`s.
func SPDX23() (fs.FS, string, error) {
	sub, err := fs.Sub(vendored, "spdx")
	if err != nil {
		return nil, "", fmt.Errorf("schemas: spdx subtree: %w", err)
	}
	return sub, "spdx-schema.json", nil
}
