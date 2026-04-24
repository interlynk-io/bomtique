// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package schema wraps the JSON Schema validator used by the CycloneDX
// and SPDX emitters for `--output-validate`. The schemas are loaded
// from an embedded [fs.FS] (see `schemas` package), never from the
// network, to honour spec §18.3.
package schema

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates SBOM JSON bytes against a compiled schema set.
// One Validator per (format, schema-bundle) pair — instantiate with
// New and reuse across many documents.
type Validator struct {
	schema *jsonschema.Schema
}

// New compiles every `*.json` file under `bundle` into a single
// schema. `entrypoint` is the basename (relative to the bundle root)
// of the top-level schema. Sibling files are pre-loaded so `$ref`
// resolution stays off the network.
//
// Each schema is registered under its own `$id` URL when present, so
// that absolute `$ref`s in the upstream CycloneDX bundle (which point
// at `http://cyclonedx.org/schema/jsf-0.82.schema.json` etc.) resolve
// against sibling files we've already loaded. Schemas without an
// `$id` get a deterministic mem:// URL based on the filename.
func New(bundle fs.FS, entrypoint string) (*Validator, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft7)

	urls, err := registerBundle(compiler, bundle)
	if err != nil {
		return nil, err
	}

	entryURL, ok := urls[entrypoint]
	if !ok {
		return nil, fmt.Errorf("schema: entrypoint %q not found in bundle", entrypoint)
	}
	sch, err := compiler.Compile(entryURL)
	if err != nil {
		return nil, fmt.Errorf("schema: compile %s: %w", entrypoint, err)
	}
	return &Validator{schema: sch}, nil
}

// Validate parses `data` as JSON and validates it against the schema.
// A nil return means the document conforms.
func (v *Validator) Validate(data []byte) error {
	if v == nil || v.schema == nil {
		return fmt.Errorf("schema: validator not initialised")
	}
	var instance any
	if err := json.Unmarshal(data, &instance); err != nil {
		return fmt.Errorf("schema: parse instance: %w", err)
	}
	if err := v.schema.Validate(instance); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	return nil
}

// fallbackSchemaURL gives a schema without an `$id` a deterministic
// URL so sibling `$ref`s that use relative paths still resolve.
func fallbackSchemaURL(name string) string {
	return "mem://bomtique/" + name
}

// registerBundle walks every `*.json` file in bundle (flat layout —
// no nested directories are expected in the vendored schemas) and
// registers each with the compiler under its own `$id` URL (falling
// back to a mem:// URL when `$id` is absent). Returns a map of
// filename → registration URL so the caller can compile the
// entrypoint by name.
func registerBundle(c *jsonschema.Compiler, bundle fs.FS) (map[string]string, error) {
	entries, err := fs.ReadDir(bundle, ".")
	if err != nil {
		return nil, fmt.Errorf("schema: read bundle root: %w", err)
	}
	urls := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if path.Ext(name) != ".json" {
			continue
		}
		f, err := bundle.Open(name)
		if err != nil {
			return nil, fmt.Errorf("schema: open %s: %w", name, err)
		}
		data, rerr := io.ReadAll(f)
		_ = f.Close()
		if rerr != nil {
			return nil, fmt.Errorf("schema: read %s: %w", name, rerr)
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("schema: parse %s: %w", name, err)
		}
		url := fallbackSchemaURL(name)
		if m, ok := doc.(map[string]any); ok {
			if id, ok := m["$id"].(string); ok && id != "" {
				url = id
			}
		}
		if err := c.AddResource(url, doc); err != nil {
			return nil, fmt.Errorf("schema: add resource %s: %w", name, err)
		}
		urls[name] = url
	}
	return urls, nil
}
