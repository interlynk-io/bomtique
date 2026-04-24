// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package jcs implements JSON Canonicalization Scheme per RFC 8785. See
// TASKS.md milestone M8 for scope: number canonicalization, string
// escaping, key sorting, and no whitespace. Used by the CycloneDX emitter
// to canonicalize the components array before hashing for deterministic
// serialNumber derivation per spec §15.3.
package jcs
