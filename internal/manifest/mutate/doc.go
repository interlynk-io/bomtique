// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package mutate is the write-back engine shared by the `bomtique
// manifest init|add|remove|update|patch` commands. It produces
// canonical JSON and CSV output for Component Manifest v1 files and
// exposes the locate / merge / CSV-fit helpers those commands layer
// on top.
//
// See TASKS.md milestone M14.0 for the full scope.
//
// Canonical JSON output: 2-space indent, LF line endings, trailing
// newline, object keys in struct-declaration order from
// internal/manifest/types.go, unknown keys sorted lexicographically
// after known fields. Model-level round-trip is guaranteed; byte-level
// round-trip is not a goal (license and diff.text collapse to object
// form on parse per §6.3 / §9.2).
//
// Canonical CSV output: the frozen §4.5 column order, LF line
// endings, RFC 4180 quoting, trailing newline.
package mutate
