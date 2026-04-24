// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl

// ASCII character classification helpers. These are byte-oriented by design:
// all PURL grammar classes are in the ASCII range, and operating on bytes
// lets the parser skip rune decoding on the hot path.

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isASCIILowerLetter(c byte) bool {
	return c >= 'a' && c <= 'z'
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isASCIILetterOrDigit(c byte) bool {
	return isASCIILetter(c) || isASCIIDigit(c)
}
