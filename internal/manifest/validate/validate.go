// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package validate implements Component Manifest v1 §13.1 structural and
// §13.2 semantic validation. Rules fall into two scopes:
//
//   - [Manifest] validates one parsed [manifest.Manifest] — the rules
//     that can be answered from a single file (and, optionally, the
//     filesystem it references).
//   - [ProcessingSet] validates rules that only make sense across the
//     full run: at least one primary (§12.1), multi-primary depends-on
//     (§10.4), empty-`components[]` rejection (§5.2).
//
// Both return a slice of [Error] values. An empty slice means the
// manifest conforms. The validator never panics; any internal mismatch
// surfaces as an Error with `Kind == ErrInternal`.
package validate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// Options controls optional validator behaviour. The zero value is a
// reasonable default — filesystem checks enabled, no SPDX grammar check,
// 10 MiB per-read cap from safefs.
type Options struct {
	// MaxFileSize is the per-read cap enforced by filesystem hash
	// checks. A zero or negative value uses safefs.DefaultMaxFileSize.
	MaxFileSize int64

	// SkipFilesystem turns off file-existence, regular-file-type, and
	// directory-walk checks. Useful for unit tests or for pre-flight
	// validation that doesn't yet have a populated working tree.
	SkipFilesystem bool

	// SPDXExpressionStrict enables full SPDX License Expression grammar
	// validation when true. When false (default), the validator only
	// checks that each texts[].id appears as a bare identifier within
	// the expression string (§6.3's MUST), which is cheap and catches
	// the most common mistakes without pulling in an SPDX grammar.
	SPDXExpressionStrict bool
}

// Kind classifies an [Error] for programmatic matching. Stderr messages
// include the Kind's short string too, so operators can filter for
// specific rule failures.
type Kind int

const (
	ErrKindUnspecified Kind = iota
	ErrRequiredField
	ErrEnumValue
	ErrIdentity          // §6.1 — name and at-least-one-of identity fields
	ErrLicense           // §6.3 — license expression / texts rules
	ErrHashForm          // §8 — exactly-one-form-per-entry
	ErrHashAlgorithm     // §8.1 — permitted algorithms
	ErrHashValue         // §8.1 — lowercase hex, correct length
	ErrHashFilesystem    // §8.2 / §8.3 — file or path resolution on disk
	ErrEmptyDirectory    // §8.4 step 3 — zero eligible files
	ErrPatchedPurl       // §9.3 — patched component shares ancestor's purl
	ErrPurlParse         // §6.4 — purl is not valid per purl-spec
	ErrDependsOn         // §10.4 — multi-primary requires non-empty depends-on
	ErrProcessingSet     // §12.1 — at least one primary
	ErrComponentsMissing // §5.2 — components[] absent or empty
	ErrPathTraversal     // §4.3
	ErrSymlink           // §18.2
	ErrInternal
)

// String names the kind for human output. Keep these stable — tooling
// may grep for them.
func (k Kind) String() string {
	switch k {
	case ErrRequiredField:
		return "required-field"
	case ErrEnumValue:
		return "enum-value"
	case ErrIdentity:
		return "identity"
	case ErrLicense:
		return "license"
	case ErrHashForm:
		return "hash-form"
	case ErrHashAlgorithm:
		return "hash-algorithm"
	case ErrHashValue:
		return "hash-value"
	case ErrHashFilesystem:
		return "hash-filesystem"
	case ErrEmptyDirectory:
		return "empty-directory"
	case ErrPatchedPurl:
		return "patched-purl"
	case ErrPurlParse:
		return "purl-parse"
	case ErrDependsOn:
		return "depends-on"
	case ErrProcessingSet:
		return "processing-set"
	case ErrComponentsMissing:
		return "components-missing"
	case ErrPathTraversal:
		return "path-traversal"
	case ErrSymlink:
		return "symlink"
	case ErrInternal:
		return "internal"
	}
	return "unspecified"
}

// Error is a single validation failure. A given validator run produces
// zero-or-more Errors; the slice is empty iff the manifest conforms.
type Error struct {
	// Path is the file path of the manifest that produced the error.
	// Empty if the error was produced from an in-memory manifest.
	Path string

	// Pointer is a JSON pointer (RFC 6901) for JSON manifests, e.g.
	// "/primary/hashes/0/algorithm". Empty for CSV inputs.
	Pointer string

	// Row is the 1-based CSV data row (0 for JSON inputs). Row 1 is the
	// first data row after the schema marker and column header.
	Row int

	// Column is the CSV column name when Row > 0 (empty otherwise).
	Column string

	// Kind classifies the rule that was violated. See the Err* constants.
	Kind Kind

	// Value is the offending value as it appeared in the manifest
	// (trimmed or truncated for display). Optional.
	Value string

	// Message is the human-readable explanation.
	Message string
}

func (e Error) Error() string {
	var b strings.Builder
	if e.Path != "" {
		b.WriteString(e.Path)
		b.WriteString(": ")
	}
	switch {
	case e.Pointer != "":
		b.WriteString(e.Pointer)
		b.WriteString(": ")
	case e.Row > 0:
		fmt.Fprintf(&b, "row %d", e.Row)
		if e.Column != "" {
			b.WriteByte('.')
			b.WriteString(e.Column)
		}
		b.WriteString(": ")
	}
	if e.Kind != ErrKindUnspecified {
		b.WriteString(e.Kind.String())
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Value != "" {
		fmt.Fprintf(&b, " (value: %q)", e.Value)
	}
	return b.String()
}

// Manifest validates one parsed manifest. Rules that require the full
// processing set — §12.1 cardinality, §10.4 multi-primary, §5.2 empty
// pool — are enforced by [ProcessingSet] instead. `m` must be non-nil.
func Manifest(m *manifest.Manifest, opts Options) []Error {
	v := newValidator(m, opts)
	switch m.Kind {
	case manifest.KindPrimary:
		if m.Primary == nil {
			v.add(Error{Kind: ErrInternal, Message: "primary kind without Primary field"})
			return v.errors
		}
		v.validateComponent(&m.Primary.Primary, "/primary", rolePrimary)
	case manifest.KindComponents:
		if m.Components == nil {
			v.add(Error{Kind: ErrInternal, Message: "components kind without Components field"})
			return v.errors
		}
		if len(m.Components.Components) == 0 {
			// §5.2 — already raised at ProcessingSet level too, but a
			// single-file validator call should flag it as well.
			v.add(Error{
				Kind:    ErrComponentsMissing,
				Pointer: "/components",
				Message: "components manifest must carry a non-empty components[] array",
			})
		}
		for i := range m.Components.Components {
			v.validateComponent(&m.Components.Components[i],
				fmt.Sprintf("/components/%d", i), rolePool)
		}
	default:
		v.add(Error{Kind: ErrInternal, Message: "unknown manifest kind"})
	}
	return v.errors
}

type componentRole int

const (
	rolePool componentRole = iota
	rolePrimary
	roleAncestor
)

// validator is the per-run accumulator.
type validator struct {
	path        string
	manifestDir string
	format      manifest.Format
	opts        Options
	errors      []Error
}

func newValidator(m *manifest.Manifest, opts Options) *validator {
	v := &validator{
		path:   m.Path,
		format: m.Format,
		opts:   opts,
	}
	if m.Path != "" {
		v.manifestDir = filepath.Dir(m.Path)
	}
	return v
}

func (v *validator) add(e Error) {
	if e.Path == "" {
		e.Path = v.path
	}
	// CSV-sourced components drop JSON pointers in favour of row/column.
	if v.format == manifest.FormatCSV && e.Pointer != "" {
		if row, col := pointerToRowColumn(e.Pointer); row > 0 {
			e.Pointer = ""
			e.Row = row
			e.Column = col
		}
	}
	v.errors = append(v.errors, e)
}

// pointerToRowColumn converts the JSON-pointer form used internally
// ("/components/0/hashes/0/algorithm") to the CSV row/column locator
// M1's CSV parser produces.  Only the top-level `/components/<i>` slice
// has a row mapping; nested paths fall back to a best-effort column
// name drawn from the last segment.
func pointerToRowColumn(ptr string) (int, string) {
	parts := strings.Split(strings.TrimPrefix(ptr, "/"), "/")
	if len(parts) < 2 || parts[0] != "components" {
		return 0, ""
	}
	var row int
	if _, err := fmt.Sscanf(parts[1], "%d", &row); err != nil {
		return 0, ""
	}
	// Convert 0-based component index to 1-based data row.
	row++
	if len(parts) <= 2 {
		return row, ""
	}
	// Map JSON field names to CSV columns where they match one-to-one.
	switch parts[2] {
	case "name", "version", "type", "description", "license", "purl", "cpe", "scope":
		return row, parts[2]
	case "supplier":
		if len(parts) >= 4 {
			switch parts[3] {
			case "name":
				return row, "supplier_name"
			case "email":
				return row, "supplier_email"
			}
		}
		return row, "supplier_name"
	case "hashes":
		if len(parts) >= 4 {
			switch parts[len(parts)-1] {
			case "algorithm":
				return row, "hash_algorithm"
			case "value":
				return row, "hash_value"
			case "file":
				return row, "hash_file"
			}
		}
		return row, "hash_algorithm"
	case "depends-on":
		return row, "depends_on"
	case "tags":
		return row, "tags"
	}
	return row, ""
}
