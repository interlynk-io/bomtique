package purl

import (
	"fmt"
	"strings"
)

// percentEncode encodes a PURL component per the spec's encoding rules.
// Alphanumeric, '.-_~' and ':' are NOT encoded. Everything else is.
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if shouldNotEncode(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// percentEncodeQualifierValue encodes a qualifier value. Commas in checksum
// values are encoded as %2C; anything outside the safe set is encoded.
func percentEncodeQualifierValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if shouldNotEncode(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// shouldNotEncode returns true for characters that MUST NOT be percent-encoded
// per the spec: alphanumeric, punctuation (.-_~), and colon.
func shouldNotEncode(c byte) bool {
	if isASCIILetterOrDigit(c) {
		return true
	}
	switch c {
	case '.', '-', '_', '~', ':':
		return true
	}
	return false
}

// percentDecode decodes percent-encoded characters in a string.
func percentDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi := unhex(s[i+1])
			lo := unhex(s[i+2])
			if hi >= 0 && lo >= 0 {
				b.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	}
	return -1
}
