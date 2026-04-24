// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package diag_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
)

func TestWarnFormatsAndCounts(t *testing.T) {
	var buf bytes.Buffer
	diag.SetSink(&buf)
	t.Cleanup(func() { diag.SetSink(nil); diag.Reset() })
	diag.Reset()

	diag.Warn("dropped %d orphans in %s", 3, "pool")
	diag.Warn("component %q has no license", "libfoo")

	got := buf.String()
	wantLines := []string{
		`warning: dropped 3 orphans in pool`,
		`warning: component "libfoo" has no license`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("missing warning line %q in output:\n%s", want, got)
		}
	}
	if diag.Count() != 2 {
		t.Errorf("Count = %d, want 2", diag.Count())
	}
}
