// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"strings"
	"testing"
)

func TestDeterministicSerialIsStable(t *testing.T) {
	input := []byte(`[{"name":"libfoo","version":"1.0.0"}]`)
	first := deterministicSerial(input)
	second := deterministicSerial(input)
	if first != second {
		t.Errorf("deterministicSerial not stable:\n  first  %s\n  second %s", first, second)
	}
	if !strings.HasPrefix(first, "urn:uuid:") {
		t.Errorf("serial must start with urn:uuid:, got %q", first)
	}
	if len(first) != len("urn:uuid:")+36 {
		t.Errorf("serial length = %d, want %d", len(first), len("urn:uuid:")+36)
	}
}

func TestDeterministicSerialDiffersWithInput(t *testing.T) {
	a := deterministicSerial([]byte(`[{"name":"libfoo"}]`))
	b := deterministicSerial([]byte(`[{"name":"libbar"}]`))
	if a == b {
		t.Errorf("different inputs produced the same serial: %s", a)
	}
}
