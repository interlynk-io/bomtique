// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// WriteJSON serialises a parsed manifest to canonical JSON output.
//
// Formatting:
//   - 2-space indent.
//   - LF line endings.
//   - Trailing newline.
//   - Object keys in struct-declaration order (see
//     internal/manifest/types.go); unknown keys appended in sorted
//     order by the MarshalJSON methods on PrimaryManifest,
//     ComponentsManifest, and Component.
//
// The Manifest argument MUST be of KindPrimary or KindComponents, and
// the corresponding embedded field MUST be non-nil.
func WriteJSON(w io.Writer, m *manifest.Manifest) error {
	if m == nil {
		return errors.New("WriteJSON: manifest is nil")
	}

	var payload any
	switch m.Kind {
	case manifest.KindPrimary:
		if m.Primary == nil {
			return errors.New("WriteJSON: primary manifest has nil Primary")
		}
		payload = m.Primary
	case manifest.KindComponents:
		if m.Components == nil {
			return errors.New("WriteJSON: components manifest has nil Components")
		}
		payload = m.Components
	default:
		return fmt.Errorf("WriteJSON: unsupported manifest kind %v", m.Kind)
	}

	compact, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("WriteJSON: marshal: %w", err)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return fmt.Errorf("WriteJSON: indent: %w", err)
	}
	buf.WriteByte('\n')
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("WriteJSON: write: %w", err)
	}
	return nil
}
