// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package hash_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/hash"
)

func TestParse_AllowlistExactNames(t *testing.T) {
	cases := []struct {
		in   string
		want hash.Algorithm
	}{
		{"SHA-256", hash.SHA256},
		{"SHA-384", hash.SHA384},
		{"SHA-512", hash.SHA512},
		{"SHA-3-256", hash.SHA3_256},
		{"SHA-3-512", hash.SHA3_512},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := hash.Parse(tc.in)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
			}
			if got.SpecName() != tc.in {
				t.Fatalf("SpecName round-trip: got %q, want %q", got.SpecName(), tc.in)
			}
		})
	}
}

func TestParse_RejectsForbidden(t *testing.T) {
	// §8.1 explicitly forbids MD5 and SHA-1 in every form.
	cases := []string{
		"MD5", "md5",
		"SHA-1", "SHA1", "sha-1", "sha1",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := hash.Parse(name)
			if !errors.Is(err, hash.ErrUnsupportedAlgorithm) {
				t.Fatalf("Parse(%q) expected ErrUnsupportedAlgorithm, got %v", name, err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error does not name offending value: %v", err)
			}
		})
	}
}

func TestParse_RejectsCaseAndCycloneDXVariants(t *testing.T) {
	// v1 uses exact canonical names from §8.1: not `sha-256`, not CycloneDX's `SHA3-256`.
	cases := []string{
		"sha-256",
		"Sha-256",
		"SHA256",
		"SHA3-256",
		"SHA3-512",
		"",
		"BLAKE3",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := hash.Parse(name); !errors.Is(err, hash.ErrUnsupportedAlgorithm) {
				t.Fatalf("Parse(%q) expected ErrUnsupportedAlgorithm, got %v", name, err)
			}
		})
	}
}

func TestValidateLiteralValue(t *testing.T) {
	// Known-empty-string SHA-256 hex (lowercase).
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	if err := hash.ValidateLiteralValue(hash.SHA256, emptySHA256); err != nil {
		t.Fatalf("known-good literal: %v", err)
	}

	// Uppercase rejected.
	upper := strings.ToUpper(emptySHA256)
	if err := hash.ValidateLiteralValue(hash.SHA256, upper); !errors.Is(err, hash.ErrInvalidLiteralValue) {
		t.Fatalf("uppercase: expected ErrInvalidLiteralValue, got %v", err)
	}

	// Wrong length rejected.
	if err := hash.ValidateLiteralValue(hash.SHA256, emptySHA256[:63]); !errors.Is(err, hash.ErrInvalidLiteralValue) {
		t.Fatalf("short: expected ErrInvalidLiteralValue, got %v", err)
	}

	// Non-hex rune rejected.
	bad := "z" + emptySHA256[1:]
	if err := hash.ValidateLiteralValue(hash.SHA256, bad); !errors.Is(err, hash.ErrInvalidLiteralValue) {
		t.Fatalf("non-hex: expected ErrInvalidLiteralValue, got %v", err)
	}
}

func TestValidateLiteralValue_LengthPerAlg(t *testing.T) {
	cases := []struct {
		alg    hash.Algorithm
		hexLen int
	}{
		{hash.SHA256, 64},
		{hash.SHA384, 96},
		{hash.SHA512, 128},
		{hash.SHA3_256, 64},
		{hash.SHA3_512, 128},
	}
	for _, tc := range cases {
		t.Run(tc.alg.SpecName(), func(t *testing.T) {
			v := strings.Repeat("a", tc.hexLen)
			if err := hash.ValidateLiteralValue(tc.alg, v); err != nil {
				t.Fatalf("right-length: %v", err)
			}
			if err := hash.ValidateLiteralValue(tc.alg, v[:tc.hexLen-1]); !errors.Is(err, hash.ErrInvalidLiteralValue) {
				t.Fatalf("short: expected ErrInvalidLiteralValue, got %v", err)
			}
			if err := hash.ValidateLiteralValue(tc.alg, v+"a"); !errors.Is(err, hash.ErrInvalidLiteralValue) {
				t.Fatalf("long: expected ErrInvalidLiteralValue, got %v", err)
			}
		})
	}
}

func TestNew_ProducesDifferentHashesPerAlg(t *testing.T) {
	// Sanity: the five algorithms produce distinct digests on the same input.
	data := []byte("abc")
	seen := make(map[string]hash.Algorithm)
	for _, alg := range []hash.Algorithm{hash.SHA256, hash.SHA384, hash.SHA512, hash.SHA3_256, hash.SHA3_512} {
		h := alg.New()
		_, _ = h.Write(data)
		sum := string(h.Sum(nil))
		if other, dup := seen[sum]; dup {
			t.Fatalf("algorithms %v and %v produce identical digest on %q", other, alg, data)
		}
		seen[sum] = alg
	}
}
