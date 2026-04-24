// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"
)

// ParseJSON parses one JSON manifest file. Path is optional and only used
// in error messages.
//
// Enforced in M1:
//   - §4.2 invalid-UTF-8 rejection.
//   - duplicate top-level / object keys rejected (JSON's object semantics
//     treat duplicates as undefined; we refuse rather than silently
//     taking "last wins").
//   - §4.4 schema marker is read from the top-level `"schema"` string;
//     unknown version within a known family is a hard error.
//   - §5.1 / §5.2 top-level unknown fields preserved into the manifest's
//     Unknown sidecar map.
//   - §6.2 unknown component fields preserved into Component.Unknown.
//
// Deeper semantic rules (§6.1 "at least one of version/purl/hashes",
// §9.3 patched-purl rule, enumeration membership, …) are M4's job.
func ParseJSON(data []byte, path string) (*Manifest, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: invalid UTF-8", pathOrUnknown(path))
	}
	if err := checkNoDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("%s: %w", pathOrUnknown(path), err)
	}

	marker, err := peekSchema(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pathOrUnknown(path), err)
	}
	kind, err := classifySchemaMarker(marker)
	if err != nil {
		if errors.Is(err, ErrNoSchemaMarker) {
			return nil, fmt.Errorf("%s: %w", pathOrUnknown(path), err)
		}
		return nil, fmt.Errorf("%s: %w", pathOrUnknown(path), err)
	}

	m := &Manifest{Path: path, Kind: kind, Format: FormatJSON}
	switch kind {
	case KindPrimary:
		var pm PrimaryManifest
		if err := json.Unmarshal(data, &pm); err != nil {
			return nil, fmt.Errorf("%s: %w", pathOrUnknown(path), err)
		}
		m.Primary = &pm
	case KindComponents:
		var cm ComponentsManifest
		if err := json.Unmarshal(data, &cm); err != nil {
			return nil, fmt.Errorf("%s: %w", pathOrUnknown(path), err)
		}
		m.Components = &cm
	default:
		return nil, fmt.Errorf("%s: internal: schema kind classifier returned %v", pathOrUnknown(path), kind)
	}
	return m, nil
}

func pathOrUnknown(path string) string {
	if path == "" {
		return "<json input>"
	}
	return path
}

// peekSchema reads only the top-level "schema" field without rejecting
// anything else. It returns the empty string when the field is absent or
// not a string — classifySchemaMarker then decides whether that's
// ErrNoSchemaMarker or something else.
func peekSchema(data []byte) (string, error) {
	var peek struct {
		Schema string `json:"schema"`
	}
	// json.Unmarshal tolerates other top-level fields; an invalid JSON
	// document here fails fast with a syntax error.
	if err := json.Unmarshal(data, &peek); err != nil {
		return "", err
	}
	return peek.Schema, nil
}

// checkNoDuplicateKeys walks the JSON token stream and returns an error on
// any object that carries the same key twice.
func checkNoDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := walkDupKeys(dec, ""); err != nil {
		return err
	}
	// Trailing tokens after the top-level value are a structural error.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("trailing content after top-level value: %w", err)
		}
		return errors.New("trailing content after top-level value")
	}
	return nil
}

func walkDupKeys(dec *json.Decoder, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("expected string key at %s", pathOrRoot(path))
			}
			if _, dup := seen[key]; dup {
				return fmt.Errorf("duplicate key %q at %s", key, pathOrRoot(path))
			}
			seen[key] = struct{}{}
			if err := walkDupKeys(dec, path+"."+key); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	case '[':
		i := 0
		for dec.More() {
			if err := walkDupKeys(dec, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
			i++
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	return nil
}

func pathOrRoot(path string) string {
	if path == "" {
		return "<root>"
	}
	return path
}

// --- Custom UnmarshalJSON methods: preserve unknowns, accept shorthand forms.

func (p *PrimaryManifest) UnmarshalJSON(data []byte) error {
	type primaryAlias PrimaryManifest
	var aux primaryAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*p = PrimaryManifest(aux)

	raw, err := rawObject(data)
	if err != nil {
		return err
	}
	p.Unknown = extractUnknown(raw, primaryKnownFields)
	return nil
}

func (p PrimaryManifest) MarshalJSON() ([]byte, error) {
	type primaryAlias PrimaryManifest
	base, err := json.Marshal(primaryAlias(p))
	if err != nil {
		return nil, err
	}
	return appendUnknowns(base, p.Unknown)
}

func (c *ComponentsManifest) UnmarshalJSON(data []byte) error {
	type componentsAlias ComponentsManifest
	var aux componentsAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = ComponentsManifest(aux)

	raw, err := rawObject(data)
	if err != nil {
		return err
	}
	c.Unknown = extractUnknown(raw, componentsManifestKnownFields)
	return nil
}

func (c ComponentsManifest) MarshalJSON() ([]byte, error) {
	type componentsAlias ComponentsManifest
	base, err := json.Marshal(componentsAlias(c))
	if err != nil {
		return nil, err
	}
	return appendUnknowns(base, c.Unknown)
}

func (c *Component) UnmarshalJSON(data []byte) error {
	type componentAlias Component
	var aux componentAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = Component(aux)

	raw, err := rawObject(data)
	if err != nil {
		return err
	}
	c.Unknown = extractUnknown(raw, componentKnownFields)
	return nil
}

func (c Component) MarshalJSON() ([]byte, error) {
	type componentAlias Component
	base, err := json.Marshal(componentAlias(c))
	if err != nil {
		return nil, err
	}
	return appendUnknowns(base, c.Unknown)
}

// appendUnknowns splices the entries of `unknown` into the trailing
// position of a compact JSON object `base`, in sorted key order. It is
// used by the custom MarshalJSON methods on the three manifest types
// that carry an Unknown sidecar map (§5.1, §5.2, §6.2). Base is expected
// to be the default json.Marshal output of a Go struct, so it always
// starts with '{' and ends with '}'.
func appendUnknowns(base []byte, unknown map[string]json.RawMessage) ([]byte, error) {
	if len(unknown) == 0 {
		return base, nil
	}
	if len(base) < 2 || base[0] != '{' || base[len(base)-1] != '}' {
		return nil, fmt.Errorf("appendUnknowns: unexpected base marshal shape")
	}
	keys := make([]string, 0, len(unknown))
	for k := range unknown {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.Grow(len(base) + 64)
	buf.Write(base[:len(base)-1])
	first := len(base) == 2 // base == "{}"
	for _, k := range keys {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kjson, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kjson)
		buf.WriteByte(':')
		buf.Write(unknown[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// License accepts either a bare string (shorthand for `{"expression": "<str>"}`)
// or the object form (Spec §6.3).
func (l *License) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		l.Expression = s
		l.Texts = nil
		return nil
	}
	type licenseAlias License
	var aux licenseAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*l = License(aux)
	return nil
}

// Diff accepts `text` as either a bare string or an Attachment object
// (Spec §9.2). Bare-string input stores the content on Text.Content with
// no encoding/contentType set; emission (M7) decides whether to materialise
// the attachment wrapper.
func (d *Diff) UnmarshalJSON(data []byte) error {
	var aux struct {
		Text json.RawMessage `json:"text,omitempty"`
		URL  *string         `json:"url,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	d.URL = aux.URL
	d.Text = nil
	if len(aux.Text) == 0 {
		return nil
	}
	trimmed := bytes.TrimSpace(aux.Text)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		d.Text = &Attachment{Content: s}
		return nil
	}
	var a Attachment
	if err := json.Unmarshal(aux.Text, &a); err != nil {
		return err
	}
	d.Text = &a
	return nil
}

// rawObject parses data as a JSON object and returns its raw-message map.
// Callers use it to harvest unknown top-level or component keys alongside
// the typed unmarshal pass.
func rawObject(data []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func extractUnknown(raw map[string]json.RawMessage, known map[string]struct{}) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	for k, v := range raw {
		if _, ok := known[k]; ok {
			continue
		}
		if out == nil {
			out = make(map[string]json.RawMessage)
		}
		// Store unknown values in compact form so the map is stable
		// across round-trips: json.Indent reflows whitespace inside
		// spliced RawMessage values when WriteJSON pretty-prints, and
		// canonicalising here means a second parse sees the same bytes.
		var buf bytes.Buffer
		if err := json.Compact(&buf, v); err == nil {
			out[k] = json.RawMessage(buf.Bytes())
		} else {
			out[k] = v
		}
	}
	return out
}

var primaryKnownFields = map[string]struct{}{
	"schema":  {},
	"primary": {},
}

var componentsManifestKnownFields = map[string]struct{}{
	"schema":     {},
	"components": {},
}

var componentKnownFields = map[string]struct{}{
	"bom-ref":             {},
	"name":                {},
	"version":             {},
	"type":                {},
	"description":         {},
	"supplier":            {},
	"license":             {},
	"purl":                {},
	"cpe":                 {},
	"external_references": {},
	"hashes":              {},
	"scope":               {},
	"pedigree":            {},
	"depends-on":          {},
	"tags":                {},
	"lifecycles":          {},
}
