// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl

import "strings"

// normalizeTypeName applies type-specific normalization to a name.
func normalizeTypeName(typ, name string) string {
	rule, ok := typeRules[typ]
	if !ok {
		return name
	}
	if rule.NormalizeName != nil {
		name = rule.NormalizeName(name)
	}
	if !rule.NameCaseSensitive {
		name = strings.ToLower(name)
	}
	return name
}

// normalizeTypeNamespace applies type-specific normalization to a namespace.
func normalizeTypeNamespace(typ, namespace string) string {
	if namespace == "" {
		return namespace
	}
	rule, ok := typeRules[typ]
	if !ok {
		return namespace
	}
	if !rule.NsCaseSensitive {
		namespace = strings.ToLower(namespace)
	}
	return namespace
}

// normalizeTypeVersion applies type-specific normalization to a version.
func normalizeTypeVersion(typ, version string) string {
	if version == "" {
		return version
	}
	rule, ok := typeRules[typ]
	if !ok {
		return version
	}
	if rule.LowercaseVersion {
		version = strings.ToLower(version)
	}
	return version
}

// normalizeMLflowName conditionally lowercases MLflow names based on the
// repository_url qualifier. Databricks URLs trigger lowercasing; Azure ML
// URLs preserve case.
func normalizeMLflowName(name string, qualifiers map[string]string) string {
	if qualifiers == nil {
		return name
	}
	repoURL := qualifiers["repository_url"]
	if repoURL == "" {
		return name
	}
	if strings.Contains(strings.ToLower(repoURL), "databricks") {
		return strings.ToLower(name)
	}
	return name
}

// normalizePyPIName lowercases and replaces underscores with dashes per PEP 503.
func normalizePyPIName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	return name
}
