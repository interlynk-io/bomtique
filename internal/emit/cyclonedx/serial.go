// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
)

// serialNamespace is the DNS namespace UUID from RFC 4122 Appendix C.
// Component Manifest v1 §15.3 pins this as the namespace for the
// deterministic serialNumber derivation.
var serialNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// serialPrefix is the fixed string the spec prepends to the SHA-256 digest
// of the JCS-canonicalized components array before UUIDv5 hashing.
const serialPrefix = "component-manifest/v1/serial/"

// deterministicSerial returns the `urn:uuid:<uuid>` form required by
// §15.3 from the JCS-canonicalized bytes of the output SBOM's components
// array. The bytes are SHA-256-hashed, hex-encoded, concatenated with the
// fixed prefix, and fed to UUIDv5 with the DNS namespace.
//
// The full emitter plumbing that feeds canonicalized bytes into this
// helper lands in M8; this function exists now so the uuid dependency is
// anchored in go.mod and its behaviour is testable in isolation.
func deterministicSerial(canonicalComponents []byte) string {
	digest := sha256.Sum256(canonicalComponents)
	name := serialPrefix + hex.EncodeToString(digest[:])
	return "urn:uuid:" + uuid.NewSHA1(serialNamespace, []byte(name)).String()
}
