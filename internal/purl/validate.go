// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl

import (
	"fmt"
	"strings"
)

// validateType checks that the type conforms to spec grammar: ASCII letter
// first, then ASCII letter/digit/'.'/'-'.
func validateType(typ string) error {
	if typ == "" {
		return fmt.Errorf("type is required")
	}
	if !isASCIILetter(typ[0]) {
		return fmt.Errorf("type %q must start with a letter", typ)
	}
	for _, c := range typ {
		if !isASCIILetterOrDigit(byte(c)) && c != '.' && c != '-' {
			return fmt.Errorf("type %q contains invalid character %q", typ, c)
		}
	}
	return nil
}

// validateQualifierKey checks that a qualifier key conforms to spec rules:
// lowercase letter or digit first, then lowercase letter/digit/'.'/'-'/'_'.
func validateQualifierKey(key string) error {
	if key == "" {
		return fmt.Errorf("qualifier key is empty")
	}
	if !isASCIILowerLetter(key[0]) && !isASCIIDigit(key[0]) {
		if isASCIILetter(key[0]) {
			return fmt.Errorf("qualifier key %q must be lowercase", key)
		}
		return fmt.Errorf("qualifier key %q must start with a letter", key)
	}
	for _, c := range key {
		if !isASCIILowerLetter(byte(c)) && !isASCIIDigit(byte(c)) && c != '.' && c != '-' && c != '_' {
			return fmt.Errorf("qualifier key %q contains invalid character %q", key, c)
		}
	}
	return nil
}

// validateTypeConstraints checks namespace required/prohibited constraints
// for the given type. Unknown types are permissive: the spec encourages
// forward compatibility with new type IDs.
func validateTypeConstraints(typ, namespace string) error {
	rule, ok := typeRules[typ]
	if !ok {
		return nil
	}
	if rule.NamespaceRequired && namespace == "" {
		return fmt.Errorf("namespace is required for type %q", typ)
	}
	if rule.NamespaceProhibited && namespace != "" {
		return fmt.Errorf("namespace is prohibited for type %q", typ)
	}
	return nil
}

// validateTypeParse runs type-specific validation that goes beyond the basic
// namespace required/prohibited checks.
func validateTypeParse(typ, name, namespace, version string, qualifiers map[string]string) error {
	switch typ {
	case "cpan":
		// CPAN names use distribution form with '-' separators, not "::".
		if strings.Contains(name, "::") || strings.Contains(namespace, "::") {
			return fmt.Errorf("cpan: names and namespaces must use '-' separators, not '::'")
		}
	case "chrome-extension":
		// Chrome extension IDs are exactly 32 lowercase letters.
		if len(name) != 32 {
			return fmt.Errorf("chrome-extension: name must be exactly 32 characters, got %d", len(name))
		}
		for _, c := range name {
			if c < 'a' || c > 'z' {
				return fmt.Errorf("chrome-extension: name must contain only lowercase letters")
			}
		}
		if version != "" {
			parts := strings.Split(version, ".")
			if len(parts) > 4 {
				return fmt.Errorf("chrome-extension: version must have at most 4 segments")
			}
			for _, p := range parts {
				for _, c := range p {
					if c < '0' || c > '9' {
						return fmt.Errorf("chrome-extension: version segments must be numeric")
					}
				}
			}
		}
	case "julia":
		if qualifiers == nil || qualifiers["uuid"] == "" {
			return fmt.Errorf("julia: uuid qualifier is required")
		}
	}
	return nil
}
