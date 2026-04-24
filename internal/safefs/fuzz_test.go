// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs_test

import (
	"testing"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

// FuzzResolveRelative feeds arbitrary (manifestDir, relPath) pairs to
// the path resolver. The resolver is pure-function — no filesystem
// access — so the no-panic invariant is strong: any input MUST yield
// either a resolved path string or a named error.
//
// Run locally with `go test -fuzz=FuzzResolveRelative ./internal/safefs`.
func FuzzResolveRelative(f *testing.F) {
	seeds := [][2]string{
		{"/work/m", "foo.txt"},
		{"/work/m", "./sub/foo.txt"},
		{"/work/m", "../escape"},
		{"/work/m", "/etc/passwd"},
		{"/work/m", `\\server\share\file`},
		{"/work/m", `C:\foo`},
		{"/work/m", "café/LICENSE"},
		{"/work/m", ""},
		{"/work/m", "foo\x00bar"},
		{"", "foo.txt"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, manifestDir, relPath string) {
		_, _ = safefs.ResolveRelative(manifestDir, relPath)
	})
}
