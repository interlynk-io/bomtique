// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package safefs_test

import (
	"testing"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

func TestToNFC(t *testing.T) {
	// NFD form: ASCII 'e' (U+0065) followed by combining acute accent (U+0301).
	nfd := "café"
	// NFC form: precomposed e-acute (U+00E9).
	nfc := "café"

	if nfd == nfc {
		t.Fatal("test fixture broken: nfd and nfc compare equal before normalization")
	}

	if got := safefs.ToNFC(nfd); got != nfc {
		t.Errorf("ToNFC(NFD) did not produce NFC:\n  got  %q (%d bytes)\n  want %q (%d bytes)",
			got, len(got), nfc, len(nfc))
	}
	if got := safefs.ToNFC(nfc); got != nfc {
		t.Errorf("ToNFC(NFC) not idempotent: got %q", got)
	}
	if got := safefs.ToNFC("plain"); got != "plain" {
		t.Errorf("ToNFC(ASCII) mutated: got %q", got)
	}
}
