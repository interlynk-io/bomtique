// Package purl implements Package-URL (PURL) parsing, construction, and
// canonical comparison following the PURL specification at
// https://github.com/package-url/purl-spec.
//
// The package is organized into several files:
//
//   - purl.go       — public API: PackageURL, Parse, Build, String, Equal.
//   - encoding.go   — percent encoding and decoding.
//   - chars.go      — ASCII character classification helpers.
//   - qualifiers.go — qualifier string parsing.
//   - subpath.go    — subpath parsing and cleaning.
//   - types.go      — per-type rule table (required/prohibited/case-sensitivity).
//   - normalize.go  — type-specific name, namespace, and version normalization.
//   - validate.go   — type, qualifier, and per-type constraint validation.
package purl

import (
	"fmt"
	"sort"
	"strings"
)

// PackageURL holds the components of a validated Package URL in canonical form.
type PackageURL struct {
	Type       string
	Namespace  string
	Name       string
	Version    string
	Qualifiers map[string]string
	Subpath    string
}

// Parse parses a PURL string into its components, following the spec's
// right-to-left parsing algorithm. The returned PackageURL is canonicalized:
// type-specific case folding, namespace normalization, version normalization,
// qualifier cleanup, and subpath cleanup are all applied.
func Parse(raw string) (PackageURL, error) {
	if raw == "" {
		return PackageURL{}, fmt.Errorf("empty PURL string")
	}

	remainder := raw

	// Split from right on '#' for subpath.
	var subpath string
	if idx := strings.LastIndex(remainder, "#"); idx >= 0 {
		subpath = remainder[idx+1:]
		remainder = remainder[:idx]
	}

	subpath, err := parseSubpath(subpath)
	if err != nil {
		return PackageURL{}, fmt.Errorf("invalid subpath: %w", err)
	}

	// Split from right on '?' for qualifiers.
	var qualifiers map[string]string
	if idx := strings.LastIndex(remainder, "?"); idx >= 0 {
		qualifiers, err = parseQualifiers(remainder[idx+1:])
		if err != nil {
			return PackageURL{}, fmt.Errorf("invalid qualifiers: %w", err)
		}
		remainder = remainder[:idx]
	}

	// Split from left on ':' for scheme.
	idx := strings.Index(remainder, ":")
	if idx < 0 {
		return PackageURL{}, fmt.Errorf("missing scheme separator ':'")
	}
	scheme := strings.ToLower(remainder[:idx])
	remainder = remainder[idx+1:]

	if scheme != "pkg" {
		return PackageURL{}, fmt.Errorf("invalid scheme %q: must be \"pkg\"", scheme)
	}

	// Strip leading slashes (pkg:// or pkg:/// etc).
	remainder = strings.TrimLeft(remainder, "/")

	// Split from left on '/' for type.
	idx = strings.Index(remainder, "/")
	if idx < 0 {
		return PackageURL{}, fmt.Errorf("missing type/name separator '/'")
	}
	typ := strings.ToLower(remainder[:idx])
	remainder = remainder[idx+1:]

	if err := validateType(typ); err != nil {
		return PackageURL{}, err
	}

	// Split from right on '@' for version, but only if the '@' comes after
	// the last '/' (i.e., it separates name from version, not a namespace
	// segment like npm's "@scope").
	var version string
	lastSlash := strings.LastIndex(remainder, "/")
	if idx := strings.LastIndex(remainder, "@"); idx >= 0 && idx > lastSlash {
		version = percentDecode(remainder[idx+1:])
		remainder = remainder[:idx]
	}

	// A trailing '/' before '@' or end-of-string means an empty name segment.
	if strings.HasSuffix(remainder, "/") {
		return PackageURL{}, fmt.Errorf("name is required")
	}

	remainder = strings.TrimRight(remainder, "/")

	// Split from right on '/' for name.
	var name, namespacePart string
	if idx := strings.LastIndex(remainder, "/"); idx >= 0 {
		namespacePart = remainder[:idx]
		name = percentDecode(remainder[idx+1:])
	} else {
		name = percentDecode(remainder)
	}

	if name == "" {
		return PackageURL{}, fmt.Errorf("name is required")
	}

	// Parse namespace segments.
	var namespace string
	if namespacePart != "" {
		segments := strings.Split(namespacePart, "/")
		var decoded []string
		for _, seg := range segments {
			seg = percentDecode(seg)
			if seg != "" {
				decoded = append(decoded, seg)
			}
		}
		namespace = strings.Join(decoded, "/")
	}

	name = normalizeTypeName(typ, name)
	namespace = normalizeTypeNamespace(typ, namespace)
	version = normalizeTypeVersion(typ, version)

	// MLflow conditional name lowercasing depends on qualifiers.
	if typ == "mlflow" {
		name = normalizeMLflowName(name, qualifiers)
	}

	if err := validateTypeConstraints(typ, namespace); err != nil {
		return PackageURL{}, err
	}
	if err := validateTypeParse(typ, name, namespace, version, qualifiers); err != nil {
		return PackageURL{}, err
	}

	return PackageURL{
		Type:       typ,
		Namespace:  namespace,
		Name:       name,
		Version:    version,
		Qualifiers: qualifiers,
		Subpath:    subpath,
	}, nil
}

// Build constructs a PackageURL from components, applying validation and
// type-specific normalization. Returns an error if required fields are
// missing or constraints are violated.
func Build(typ, namespace, name, version string, qualifiers map[string]string, subpath string) (PackageURL, error) {
	if typ == "" {
		return PackageURL{}, fmt.Errorf("type is required")
	}

	typ = strings.ToLower(typ)
	if err := validateType(typ); err != nil {
		return PackageURL{}, err
	}

	if name == "" {
		return PackageURL{}, fmt.Errorf("name is required")
	}

	// Validate qualifier keys (empty values are silently dropped below).
	for k, v := range qualifiers {
		if v == "" {
			continue
		}
		if err := validateQualifierKey(k); err != nil {
			return PackageURL{}, err
		}
	}

	if subpath != "" {
		var err error
		subpath, err = parseSubpath(subpath)
		if err != nil {
			return PackageURL{}, fmt.Errorf("invalid subpath: %w", err)
		}
	}

	namespace = strings.Trim(namespace, "/")
	name = strings.Trim(name, "/")

	name = normalizeTypeName(typ, name)
	namespace = normalizeTypeNamespace(typ, namespace)
	version = normalizeTypeVersion(typ, version)

	if typ == "mlflow" {
		name = normalizeMLflowName(name, qualifiers)
	}

	if err := validateTypeConstraints(typ, namespace); err != nil {
		return PackageURL{}, err
	}
	if err := validateTypeParse(typ, name, namespace, version, qualifiers); err != nil {
		return PackageURL{}, err
	}

	cleaned := make(map[string]string)
	for k, v := range qualifiers {
		if v != "" {
			cleaned[k] = v
		}
	}
	if len(cleaned) == 0 {
		cleaned = nil
	}

	return PackageURL{
		Type:       typ,
		Namespace:  namespace,
		Name:       name,
		Version:    version,
		Qualifiers: cleaned,
		Subpath:    subpath,
	}, nil
}

// String renders the PackageURL to its canonical string form.
func (p PackageURL) String() string {
	var b strings.Builder
	b.WriteString("pkg:")
	b.WriteString(p.Type)
	b.WriteByte('/')

	if p.Namespace != "" {
		segments := strings.Split(p.Namespace, "/")
		for i, seg := range segments {
			if i > 0 {
				b.WriteByte('/')
			}
			b.WriteString(percentEncode(seg))
		}
		b.WriteByte('/')
	}

	b.WriteString(percentEncode(p.Name))

	if p.Version != "" {
		b.WriteByte('@')
		b.WriteString(percentEncode(p.Version))
	}

	if len(p.Qualifiers) > 0 {
		b.WriteByte('?')

		keys := make([]string, 0, len(p.Qualifiers))
		for k := range p.Qualifiers {
			if p.Qualifiers[k] != "" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)

		for i, k := range keys {
			if i > 0 {
				b.WriteByte('&')
			}
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(percentEncodeQualifierValue(p.Qualifiers[k]))
		}
	}

	if p.Subpath != "" {
		b.WriteByte('#')
		segments := strings.Split(p.Subpath, "/")
		for i, seg := range segments {
			if i > 0 {
				b.WriteByte('/')
			}
			b.WriteString(percentEncode(seg))
		}
	}

	return b.String()
}

// Equal reports whether two canonicalized PackageURLs are equivalent. Both
// arguments MUST be the output of Parse or Build; callers must not compose
// PackageURL values directly.
func Equal(a, b PackageURL) bool {
	if a.Type != b.Type ||
		a.Namespace != b.Namespace ||
		a.Name != b.Name ||
		a.Version != b.Version ||
		a.Subpath != b.Subpath {
		return false
	}
	if len(a.Qualifiers) != len(b.Qualifiers) {
		return false
	}
	for k, v := range a.Qualifiers {
		if b.Qualifiers[k] != v {
			return false
		}
	}
	return true
}

// CanonEqual reports whether two PURL strings are equivalent in canonical
// form. Both sides are parsed and normalized before comparison. A parse
// error on either side is returned as-is.
func CanonEqual(a, b string) (bool, error) {
	pa, err := Parse(a)
	if err != nil {
		return false, fmt.Errorf("left: %w", err)
	}
	pb, err := Parse(b)
	if err != nil {
		return false, fmt.Errorf("right: %w", err)
	}
	return Equal(pa, pb), nil
}
