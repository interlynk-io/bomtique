// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package hash

import (
	"encoding/hex"
	"io"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

// File computes the file-form hash (§8.2): open relPath under the safefs
// rules — NFC path normalisation, symlink refusal, regular-file check,
// size cap — stream the bytes into alg.New(), and return the lowercase
// hex digest.
//
// A zero or negative maxSize uses safefs.DefaultMaxFileSize (10 MiB).
func File(manifestDir, relPath string, alg Algorithm, maxSize int64) (string, error) {
	rc, err := safefs.Open(manifestDir, relPath, maxSize)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()

	h := alg.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
