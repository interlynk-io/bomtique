// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package safefs provides the path-resolution and file-open primitives
// required by Component Manifest v1 §4.3 (relative-path resolution),
// §4.6 (NFC normalization), §8 (per-read size cap), and §18.1 / §18.2
// (path-traversal and symlink refusal). See TASKS.md milestone M2 for
// scope.
package safefs
