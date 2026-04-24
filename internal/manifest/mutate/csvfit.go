// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"fmt"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// CheckFitsCSV returns nil when the component can be expressed in the
// frozen §4.5 CSV format, or an error naming the first field that
// cannot. Spec §4.5 (trailing paragraph) enumerates the shape
// restrictions; this helper mirrors that list.
//
// Callers MUST use the message to point the user at --into <json-path>
// or the equivalent escape hatch.
func CheckFitsCSV(c *manifest.Component) error {
	if c == nil {
		return fmt.Errorf("CheckFitsCSV: component is nil")
	}
	if c.BOMRef != nil && *c.BOMRef != "" {
		return fmt.Errorf("csv cannot carry bom-ref (§4.5); use a JSON components manifest")
	}
	if len(c.ExternalReferences) > 0 {
		return fmt.Errorf("csv cannot carry external_references (§4.5); use a JSON components manifest")
	}
	if c.License != nil && len(c.License.Texts) > 0 {
		return fmt.Errorf("csv cannot carry structured license texts (§4.5); use a JSON components manifest")
	}
	for i, h := range c.Hashes {
		if h.Path != nil {
			return fmt.Errorf("csv cannot carry directory-form hashes (hashes[%d].path, §4.5); use a JSON components manifest", i)
		}
	}
	if len(c.Hashes) > 1 {
		return fmt.Errorf("csv supports at most one hash entry (§4.5); use a JSON components manifest for additional hashes")
	}
	if c.Pedigree != nil {
		return fmt.Errorf("csv cannot carry pedigree (§4.5); use a JSON components manifest")
	}
	if len(c.Lifecycles) > 0 {
		return fmt.Errorf("csv cannot carry lifecycles (§4.5); use a JSON components manifest")
	}
	if len(c.Unknown) > 0 {
		return fmt.Errorf("csv cannot carry unknown fields (§4.5); use a JSON components manifest")
	}
	return nil
}
