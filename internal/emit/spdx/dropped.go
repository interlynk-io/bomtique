// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import "github.com/interlynk-io/bomtique/internal/diag"

// droppedCounter tracks which dropped-field classes we've already
// warned about. §14.2's closing MUST requires "one warning per dropped
// field class" per run; this counter enforces that by suppressing
// second-and-later warnings of the same class.
//
// The counter also exposes per-class triggers so code paths (e.g.
// scope handling in package.go) can flag the drop without building
// the warning text themselves.
type droppedCounter struct {
	sawScope       bool
	sawVariants    bool
	sawDescendants bool
	sawLifecycles  bool
}

func newDroppedCounter() *droppedCounter { return &droppedCounter{} }

// scope marks that at least one component's scope field was dropped.
func (d *droppedCounter) scope() { d.sawScope = true }

// variants marks that at least one component's pedigree.variants was dropped.
func (d *droppedCounter) variants() { d.sawVariants = true }

// descendants marks that at least one component's pedigree.descendants was dropped.
func (d *droppedCounter) descendants() { d.sawDescendants = true }

// lifecycles marks that the primary's metadata.lifecycles was dropped.
func (d *droppedCounter) lifecycles() { d.sawLifecycles = true }

// emitWarnings emits one stderr warning for each triggered class,
// through internal/diag so the §13.3 channel remains the single exit
// point for consumer warnings.
func (d *droppedCounter) emitWarnings() {
	if d.sawScope {
		diag.Warn("spdx: dropped field class `scope` — SPDX 2.3 has no runtime-presence concept (§14.2)")
	}
	if d.sawVariants {
		diag.Warn("spdx: dropped field class `pedigree.variants` — SPDX 2.3 has no equivalent (§14.2)")
	}
	if d.sawDescendants {
		diag.Warn("spdx: dropped field class `pedigree.descendants` — SPDX 2.3 has no equivalent (§14.2)")
	}
	if d.sawLifecycles {
		diag.Warn("spdx: dropped field class `metadata.lifecycles` — SPDX 2.3 has no equivalent (§14.2)")
	}
}
