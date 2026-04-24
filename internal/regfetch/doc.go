// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package regfetch is the registry-metadata import framework used by
// `bomtique manifest add` and `bomtique manifest update`.
//
// Scope deliberately narrow: one HTTPS GET per call against a
// well-known JSON endpoint (npm registry, PyPI JSON API, GitHub API,
// etc.), produce a single [manifest.Component]. No repository clones,
// no tarball downloads, no archive extraction.
//
// This package owns the only net/http usage inside internal/. Every
// other mutation-path import — scan, validate, emit — must stay
// network-free; a lint-style test (`TestNoNetworkImportsOutsideRegfetch`
// in internal/manifest/mutate) enforces that invariant by grepping
// package imports.
//
// See TASKS.md milestone M14.7 for the shape of the shared Client and
// Importer interface.
package regfetch
