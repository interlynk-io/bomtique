// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Introspect the Component Manifest v1 schema",
	}
	cmd.AddCommand(newManifestSchemaCmd())
	return cmd
}

func newManifestSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the Component Manifest v1 JSON Schema (draft 2020-12)",
		Long: `Prints the JSON Schema draft 2020-12 document describing Component Manifest v1
shapes. The canonical schema referenced by spec Appendix A is still TODO; the
output here is a placeholder identifying the v1 markers, ready for schema
authoring in a follow-up PR.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestSchema(cmd.OutOrStdout())
		},
	}
}

// runManifestSchema emits a placeholder draft 2020-12 document. The
// shape is intentionally minimal so downstream tooling can still
// validate `schema` markers without any of the field-level detail —
// that comes with the full schema authoring task.
func runManifestSchema(stdout io.Writer) error {
	placeholder := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://interlynk.io/schemas/component-manifest/v1.schema.json",
		"title":   "Component Manifest v1 (placeholder)",
		"description": "Placeholder schema — canonical shape is TODO per spec Appendix A. " +
			"The current document only validates the top-level schema marker; field-level " +
			"structural rules are enforced by the Go validator (internal/manifest/validate).",
		"oneOf": []any{
			map[string]any{
				"type":     "object",
				"required": []string{"schema", "primary"},
				"properties": map[string]any{
					"schema":  map[string]any{"const": "primary-manifest/v1"},
					"primary": map[string]any{"type": "object"},
				},
			},
			map[string]any{
				"type":     "object",
				"required": []string{"schema", "components"},
				"properties": map[string]any{
					"schema":     map[string]any{"const": "component-manifest/v1"},
					"components": map[string]any{"type": "array", "minItems": 1},
				},
			},
		},
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(placeholder); err != nil {
		return fmt.Errorf("encode schema: %w", err)
	}
	return nil
}
