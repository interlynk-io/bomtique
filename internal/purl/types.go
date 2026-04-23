// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl

// typeRule captures the normalization and validation posture for a PURL type.
// Defaults are chosen so the zero value means "namespace optional, everything
// lowercased, no special version handling, no extra name normalization".
type typeRule struct {
	NamespaceRequired   bool
	NamespaceProhibited bool
	NameCaseSensitive   bool
	NsCaseSensitive     bool
	LowercaseVersion    bool
	NormalizeName       func(string) string
}

// typeRules maps PURL type identifiers to their rules.
// Derived from https://github.com/package-url/purl-spec/tree/main/types.
var typeRules = map[string]typeRule{
	// --- Namespace required, both lowercase ---
	"alpm":             {NamespaceRequired: true},
	"apk":              {NamespaceRequired: true},
	"composer":         {NamespaceRequired: true},
	"deb":              {NamespaceRequired: true},
	"github":           {NamespaceRequired: true},
	"golang":           {NamespaceRequired: true},
	"qpkg":             {NamespaceRequired: true},
	"vscode-extension": {NamespaceRequired: true},
	"bitbucket":        {NamespaceRequired: true},

	// --- Namespace required, namespace lowercase, name case-sensitive ---
	"rpm": {NamespaceRequired: true, NameCaseSensitive: true},

	// --- Namespace required, both case-sensitive ---
	"maven":       {NamespaceRequired: true, NameCaseSensitive: true, NsCaseSensitive: true},
	"huggingface": {NamespaceRequired: true, NameCaseSensitive: true, NsCaseSensitive: true, LowercaseVersion: true},
	"swift":       {NamespaceRequired: true, NameCaseSensitive: true, NsCaseSensitive: true},
	"cpan":        {NamespaceRequired: true, NameCaseSensitive: true, NsCaseSensitive: true},

	// --- Namespace optional, both lowercase ---
	"hex":      {},
	"luarocks": {},
	"npm":      {},

	// --- Namespace optional, both case-sensitive ---
	"conan":   {NameCaseSensitive: true, NsCaseSensitive: true},
	"docker":  {NameCaseSensitive: true, NsCaseSensitive: true},
	"swid":    {NameCaseSensitive: true, NsCaseSensitive: true},
	"generic": {NameCaseSensitive: true, NsCaseSensitive: true},

	// --- Namespace optional, namespace lowercase, name case-sensitive ---
	"yocto": {NameCaseSensitive: true},

	// --- Namespace prohibited, name lowercase ---
	"bitnami": {NamespaceProhibited: true},
	"oci":     {NamespaceProhibited: true},
	"otp":     {NamespaceProhibited: true},
	"pub":     {NamespaceProhibited: true},

	// --- Namespace prohibited, name lowercase + special normalization ---
	"pypi": {
		NamespaceProhibited: true,
		NormalizeName:       normalizePyPIName,
	},

	// --- Namespace prohibited, name case-sensitive ---
	"bazel":            {NamespaceProhibited: true, NameCaseSensitive: true},
	"cargo":            {NamespaceProhibited: true, NameCaseSensitive: true},
	"chrome-extension": {NamespaceProhibited: true, NameCaseSensitive: true},
	"cocoapods":        {NamespaceProhibited: true, NameCaseSensitive: true},
	"conda":            {NamespaceProhibited: true, NameCaseSensitive: true},
	"cran":             {NamespaceProhibited: true, NameCaseSensitive: true},
	"gem":              {NamespaceProhibited: true, NameCaseSensitive: true},
	"hackage":          {NamespaceProhibited: true, NameCaseSensitive: true},
	"julia":            {NamespaceProhibited: true, NameCaseSensitive: true},
	// mlflow: Databricks repository_url → lowercase name (handled in normalizeMLflowName).
	"mlflow": {NamespaceProhibited: true, NameCaseSensitive: true},
	"nuget":  {NamespaceProhibited: true, NameCaseSensitive: true},
	"opam":   {NamespaceProhibited: true, NameCaseSensitive: true},
}
