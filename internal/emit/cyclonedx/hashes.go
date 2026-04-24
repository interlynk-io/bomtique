// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"fmt"

	"github.com/interlynk-io/bomtique/internal/hash"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

// buildHashes converts a component's `hashes[]` to CycloneDX form per
// §14.1. Each entry is one of the three §8 forms:
//
//   - literal (value): passed through as-is (§8.1 "consumer MUST pass
//     the value through to the output SBOM without recomputation").
//   - file: hash.File computes the digest against manifestDir.
//   - path: hash.Directory handles both the regular-file and directory
//     cases (§8.3), returning the appropriate §8.4 digest.
//
// Algorithm names are translated from the manifest's canonical form
// (`SHA-3-256`) to CycloneDX's form (`SHA3-256`) on the way out;
// elsewhere (purl `hashes[].algorithm` fields, internal/hash.Parse)
// uses the manifest-canonical form.
func buildHashes(in []manifest.Hash, manifestDir string, opts Options) ([]cdxHash, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]cdxHash, 0, len(in))
	for i, h := range in {
		entry, err := buildHashEntry(h, manifestDir, opts)
		if err != nil {
			return nil, fmt.Errorf("hashes[%d]: %w", i, err)
		}
		out = append(out, entry)
	}
	return out, nil
}

func buildHashEntry(h manifest.Hash, manifestDir string, opts Options) (cdxHash, error) {
	alg, err := hash.Parse(h.Algorithm)
	if err != nil {
		return cdxHash{}, err
	}

	var value string
	switch {
	case h.Value != nil && *h.Value != "":
		value = *h.Value
	case h.File != nil && *h.File != "":
		v, err := hash.File(manifestDir, *h.File, alg, opts.MaxFileSize)
		if err != nil {
			return cdxHash{}, err
		}
		value = v
	case h.Path != nil && *h.Path != "":
		v, err := hash.Directory(manifestDir, *h.Path, alg, h.Extensions, opts.MaxFileSize)
		if err != nil {
			return cdxHash{}, err
		}
		value = v
	default:
		return cdxHash{}, fmt.Errorf("hash entry carries no value, file, or path")
	}

	return cdxHash{Alg: cdxAlgorithmName(alg), Content: value}, nil
}

// cdxAlgorithmName translates the manifest's canonical §8.1 algorithm
// name into the form CycloneDX 1.7 uses in `hashes[].alg`.
// CycloneDX drops the internal hyphen in SHA-3 family names:
//
//	Manifest         CycloneDX
//	SHA-256          SHA-256
//	SHA-384          SHA-384
//	SHA-512          SHA-512
//	SHA-3-256        SHA3-256
//	SHA-3-512        SHA3-512
func cdxAlgorithmName(a hash.Algorithm) string {
	switch a {
	case hash.SHA256:
		return "SHA-256"
	case hash.SHA384:
		return "SHA-384"
	case hash.SHA512:
		return "SHA-512"
	case hash.SHA3_256:
		return "SHA3-256"
	case hash.SHA3_512:
		return "SHA3-512"
	}
	return a.SpecName() // fallback — unreachable by construction
}
