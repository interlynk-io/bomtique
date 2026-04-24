// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

func TestResolveRelative_RejectsAbsolute(t *testing.T) {
	cases := []struct {
		name, path string
	}{
		{"posix absolute", "/etc/passwd"},
		{"unc backslash", `\\server\share\file`},
		{"unc forward slash", "//server/share/file"},
		{"rooted backslash", `\foo`},
		{"drive letter backslash upper", `C:\foo`},
		{"drive letter backslash lower", `c:\foo`},
		{"drive letter forward slash", "C:/foo"},
		{"drive letter relative", "C:foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := safefs.ResolveRelative("/work/m", tc.path)
			if !errors.Is(err, safefs.ErrAbsolutePath) {
				t.Fatalf("expected ErrAbsolutePath, got %v", err)
			}
		})
	}
}

func TestResolveRelative_RejectsTraversal(t *testing.T) {
	cases := []string{
		"..",
		"../sibling",
		"sub/../../escape",
		"./../../escape",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			_, err := safefs.ResolveRelative("/work/m", p)
			if !errors.Is(err, safefs.ErrTraversal) {
				t.Fatalf("expected ErrTraversal, got %v", err)
			}
		})
	}
}

func TestResolveRelative_AllowsSaneRelative(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"foo", "/work/m/foo"},
		{"./foo", "/work/m/foo"},
		{"sub/deep/file.txt", "/work/m/sub/deep/file.txt"},
		{"sub/../file.txt", "/work/m/file.txt"}, // inside after clean
		{".", "/work/m"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := safefs.ResolveRelative("/work/m", tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := filepath.Clean(tc.want)
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestResolveRelative_NFCNormalization(t *testing.T) {
	// NFD (decomposed) "café" vs NFC (composed) "café".
	nfd := "café/LICENSE"
	nfc := "café/LICENSE"
	if nfd == nfc {
		t.Fatal("fixture broken: NFD == NFC pre-normalize")
	}
	got, err := safefs.ResolveRelative("/work/m", nfd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Clean(filepath.Join("/work/m", nfc))
	if got != want {
		t.Fatalf("NFC normalization failed: got %q, want %q", got, want)
	}
}

func TestResolveRelative_EmptyAndNull(t *testing.T) {
	if _, err := safefs.ResolveRelative("/work/m", ""); !errors.Is(err, safefs.ErrEmptyPath) {
		t.Fatalf("expected ErrEmptyPath, got %v", err)
	}
	if _, err := safefs.ResolveRelative("/work/m", "foo\x00bar"); !errors.Is(err, safefs.ErrNullByte) {
		t.Fatalf("expected ErrNullByte, got %v", err)
	}
}

func TestResolveRelative_ErrorWording(t *testing.T) {
	// The rejection message must identify the offending path, so field-level
	// error reporting in M4 can surface something useful.
	_, err := safefs.ResolveRelative("/work/m", "/absolute")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "/absolute") {
		t.Fatalf("error does not name offending path: %v", err)
	}
}
