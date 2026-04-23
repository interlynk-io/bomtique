// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package pool merges components manifests into the single shared pool
// consumed by every primary in a processing set. See TASKS.md milestone
// M5 for scope: identity precedence per spec §11, dedup passes including
// the secondary mixed purl / no-purl cross-check, and canonical purl
// comparison via internal/purl.CanonEqual.
package pool
