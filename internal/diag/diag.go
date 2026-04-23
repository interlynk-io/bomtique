// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

// Package diag is the single emitter for the `warning:`-prefixed stderr
// channel required by Component Manifest v1 §13.3. All warnings emitted
// by the consumer MUST go through this package so that the channel stays
// uniform for CI tooling.
package diag

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// sink is the destination for Warn. Tests swap it via SetSink.
var sink io.Writer = os.Stderr

// counter tracks the number of warnings emitted in the current run. It is
// used by --warnings-as-errors to decide the process exit code.
var counter atomic.Uint64

// SetSink redirects warning output. Intended for tests. Pass nil to restore
// the default of os.Stderr.
func SetSink(w io.Writer) {
	if w == nil {
		sink = os.Stderr
		return
	}
	sink = w
}

// Warn writes a warning to the configured sink. The message is prefixed with
// "warning: " and terminated with a newline.
func Warn(format string, args ...any) {
	counter.Add(1)
	fmt.Fprintf(sink, "warning: "+format+"\n", args...)
}

// Count returns the number of warnings emitted so far in the process.
func Count() uint64 {
	return counter.Load()
}

// Reset zeroes the warning counter. Intended for tests.
func Reset() {
	counter.Store(0)
}
