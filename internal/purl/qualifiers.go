// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package purl

import "strings"

// parseQualifiers parses a qualifiers string (the portion after the '?') into
// a canonical key=value map. Keys are lowercased; values are percent-decoded;
// empty values are dropped per spec.
func parseQualifiers(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}

	result := make(map[string]string)
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx < 0 {
			// Malformed pairs without '=' are skipped silently per spec tolerance.
			continue
		}
		key := strings.ToLower(pair[:idx])
		value := percentDecode(pair[idx+1:])

		if err := validateQualifierKey(key); err != nil {
			return nil, err
		}

		if value == "" {
			continue
		}
		result[key] = value
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
