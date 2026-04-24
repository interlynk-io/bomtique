// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package hash

import (
	"crypto/sha256"
	"crypto/sha3"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
)

// Algorithm is one of the five hash algorithms §8.1 permits. MD5, SHA-1,
// and any other identifier are outside the allowlist in *every* hash form
// — literal, file, and directory alike.
type Algorithm int

const (
	algUnknown Algorithm = iota
	SHA256
	SHA384
	SHA512
	SHA3_256
	SHA3_512
)

// SpecName is the canonical algorithm label as it appears in a manifest's
// `hashes[].algorithm` field. These strings are the exact forms §8.1
// mandates: `SHA-3-256` (not CycloneDX's `SHA3-256`) — the CycloneDX-
// compatible form happens at emission (M7), not here.
func (a Algorithm) SpecName() string {
	switch a {
	case SHA256:
		return "SHA-256"
	case SHA384:
		return "SHA-384"
	case SHA512:
		return "SHA-512"
	case SHA3_256:
		return "SHA-3-256"
	case SHA3_512:
		return "SHA-3-512"
	}
	return "<unknown>"
}

// String makes Algorithm play nicely with %v / %s formatting.
func (a Algorithm) String() string { return a.SpecName() }

// New returns a freshly-initialised hash.Hash for this algorithm. Callers
// MUST have obtained Algorithm from Parse, which enforces the allowlist;
// using a zero-value Algorithm panics so bugs surface as crashes rather
// than silent MD5-style weakness.
func (a Algorithm) New() hash.Hash {
	switch a {
	case SHA256:
		return sha256.New()
	case SHA384:
		return sha512.New384()
	case SHA512:
		return sha512.New()
	case SHA3_256:
		return sha3.New256()
	case SHA3_512:
		return sha3.New512()
	}
	panic(fmt.Sprintf("hash: New called on invalid Algorithm %d — Parse it first", a))
}

// hexLen returns the number of lowercase hex characters a valid literal
// value MUST carry for this algorithm.
func (a Algorithm) hexLen() int {
	switch a {
	case SHA256, SHA3_256:
		return 64
	case SHA384:
		return 96
	case SHA512, SHA3_512:
		return 128
	}
	return 0
}

// ErrUnsupportedAlgorithm signals an algorithm name outside the §8.1
// allowlist — including the explicitly forbidden MD5 and SHA-1. The
// error message names the offending value so manifest-field validation
// can surface it.
var ErrUnsupportedAlgorithm = errors.New("unsupported hash algorithm")

// ErrInvalidLiteralValue signals that a literal-form `value` string is
// not a lowercase-hex digest of the declared algorithm's expected length.
// §8.1 mandates lowercase hex for producer-supplied literals.
var ErrInvalidLiteralValue = errors.New("invalid hash value")

// Parse maps a spec `hashes[].algorithm` string to an Algorithm. Matching
// is case-sensitive against the canonical forms in §8.1 — producers are
// responsible for using the exact form. Any other value, including `MD5`,
// `SHA-1`, `sha-256`, or `SHA3-256`, returns ErrUnsupportedAlgorithm.
func Parse(name string) (Algorithm, error) {
	switch name {
	case "SHA-256":
		return SHA256, nil
	case "SHA-384":
		return SHA384, nil
	case "SHA-512":
		return SHA512, nil
	case "SHA-3-256":
		return SHA3_256, nil
	case "SHA-3-512":
		return SHA3_512, nil
	}
	return algUnknown, fmt.Errorf("%w: %q (permitted: SHA-256, SHA-384, SHA-512, SHA-3-256, SHA-3-512)",
		ErrUnsupportedAlgorithm, name)
}

// ValidateLiteralValue checks that v is a lowercase-hex digest of the
// declared algorithm's expected length (§8.1). It does not parse the
// value into bytes — the consumer passes the literal through to the
// output SBOM unchanged — but it does reject uppercase, mixed-case, and
// wrong-length values.
func ValidateLiteralValue(alg Algorithm, v string) error {
	want := alg.hexLen()
	if want == 0 {
		return fmt.Errorf("%w: algorithm %d is not initialised", ErrUnsupportedAlgorithm, alg)
	}
	if len(v) != want {
		return fmt.Errorf("%w: %s expects %d hex chars, got %d",
			ErrInvalidLiteralValue, alg.SpecName(), want, len(v))
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("%w: non-lowercase-hex byte %q at offset %d",
				ErrInvalidLiteralValue, c, i)
		}
	}
	return nil
}
