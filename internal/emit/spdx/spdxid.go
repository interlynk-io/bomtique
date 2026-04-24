// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"fmt"
	"strings"
)

// idAssigner hands out unique SPDXIDs derived from component names.
// The SPDX 2.3 spec restricts SPDXIDs to `SPDXRef-[A-Za-z0-9.\-+]+`;
// sanitizeID collapses everything outside that set and `newSPDXID`
// appends a numeric suffix on collision so the document's SPDXIDs are
// guaranteed unique regardless of input.
type idAssigner struct {
	seen map[string]int
}

func newIDAssigner() *idAssigner {
	return &idAssigner{seen: map[string]int{}}
}

func (a *idAssigner) assign(raw string) string {
	base := sanitizeID(raw)
	id := "SPDXRef-" + base
	if _, taken := a.seen[id]; !taken {
		a.seen[id] = 1
		return id
	}
	// Collision: bump a counter until we land on an unused ID. The
	// counter is deterministic in the order of assign() calls.
	a.seen[id]++
	for {
		candidate := fmt.Sprintf("%s-%d", id, a.seen[id])
		if _, taken := a.seen[candidate]; !taken {
			a.seen[candidate] = 1
			return candidate
		}
		a.seen[id]++
	}
}

// sanitizeID maps `raw` onto the SPDX ID-allowed set. Characters
// outside `[A-Za-z0-9.\-+]` are replaced with `-`; runs of `-` are
// collapsed to a single `-`; leading / trailing `-` are trimmed. An
// empty result falls back to "id" so the returned SPDXID stays valid.
func sanitizeID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.',
			r == '-',
			r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		return "id"
	}
	return s
}
