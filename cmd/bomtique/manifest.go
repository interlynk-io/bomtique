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
		Short: "Inspect and mutate Component Manifest v1 files",
		Long: `manifest scaffolds and edits .primary.json and .components.json files.

All mutation subcommands share a common grammar:

  <ref>               accepted by remove / update / patch — either
                      pkg:<type>/<name>[@<version>] or <name>@<version>
  -C, --chdir <dir>   run from this directory (default: CWD)
  --into <path>       explicit target components manifest; otherwise the
                      nearest .components.json walking up from CWD is used,
                      or one is created alongside the primary on first add
  --dry-run           (add/remove/update/patch) report the plan; do not write

Component types (spec §7.1): library, application, framework, container,
operating-system, device, firmware, file, platform, device-driver,
machine-learning-model, data.

Scopes (§7.2): required, optional, excluded.

External-reference types (§7.3): website, vcs, documentation, issue-tracker,
distribution, support, release-notes, advisories, other.

Patch types (§7.4): unofficial, monkey, backport, cherry-pick.

Registry importers used by 'add' / 'update' --online:
  pkg:github/<owner>/<repo>[@<ref>]         or a github.com URL
  pkg:gitlab/<group>/.../<proj>[@<ref>]     or a gitlab.com URL
  pkg:npm/<name>[@<ver>]                    or npmjs.com URL
  pkg:pypi/<name>[@<ver>]                   or pypi.org URL
  pkg:cargo/<name>[@<ver>]                  or crates.io URL
Auth env: GITHUB_TOKEN, GITLAB_TOKEN. Override base URLs with
BOMTIQUE_{GITHUB,GITLAB,NPM,PYPI,CARGO}_BASE_URL.

Exit codes: see 'bomtique --help'.`,
	}
	cmd.AddCommand(
		newManifestSchemaCmd(),
		newManifestInitCmd(),
		newManifestAddCmd(),
		newManifestRemoveCmd(),
		newManifestUpdateCmd(),
		newManifestPatchCmd(),
	)
	return cmd
}

func newManifestSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the Component Manifest v1 JSON Schema (draft 2020-12)",
		Long: `Prints a JSON Schema draft 2020-12 document for Component Manifest v1 on
stdout.

Note: this is a PLACEHOLDER schema. It validates only the two top-level
schema markers (primary-manifest/v1 and component-manifest/v1); field-level
rules are enforced by the Go validator, which you can run via
'bomtique validate'. The canonical schema referenced by spec Appendix A is
still being authored.

Examples:
  bomtique manifest schema | jq .
  bomtique manifest schema > schema.json`,
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
