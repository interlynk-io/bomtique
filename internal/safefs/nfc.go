// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs

import "golang.org/x/text/unicode/norm"

// ToNFC returns s normalized to Unicode Normalization Form C. Component
// Manifest v1 §4.6 mandates NFC for every relative path in a manifest and
// for extension-filter comparisons, so that directory digests remain
// reproducible across filesystems that normalize differently (ext4,
// HFS+, APFS, NTFS).
func ToNFC(s string) string {
	return norm.NFC.String(s)
}
