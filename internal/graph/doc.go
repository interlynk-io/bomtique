// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package graph resolves depends-on references and computes transitive
// reachability from a primary. See TASKS.md milestone M6 for scope:
// reference parsing per spec §10.2, pool lookup, closure with cycle
// tolerance, and unreachable/orphan warnings per §10.3 and §10.4.
package graph
