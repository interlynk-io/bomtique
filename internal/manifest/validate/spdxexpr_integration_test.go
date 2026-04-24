// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate_test

import (
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
)

// TestValidateLicense_SPDXExpressionStrict flips the strict flag on
// and off for a deliberately ungrammatical expression (lowercase
// "or"). Without strict mode the existing token-containment check
// still passes so no ErrLicense fires for grammar reasons; with
// strict mode the grammar parser surfaces an ErrLicense.
func TestValidateLicense_SPDXExpressionStrict(t *testing.T) {
	m := &manifest.Manifest{
		Kind:   manifest.KindPrimary,
		Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema: manifest.SchemaPrimaryV1,
			Primary: manifest.Component{
				Name:    "app",
				Version: strPtr("1"),
				License: &manifest.License{Expression: "MIT or Apache-2.0"},
			},
		},
	}

	lax := validate.Manifest(m, validate.Options{SkipFilesystem: true})
	for _, e := range lax {
		if e.Kind == validate.ErrLicense && strings.Contains(e.Message, "grammar") {
			t.Fatalf("grammar error fired without strict flag: %v", e)
		}
	}

	strict := validate.Manifest(m, validate.Options{
		SkipFilesystem:       true,
		SPDXExpressionStrict: true,
	})
	var found bool
	for _, e := range strict {
		if e.Kind == validate.ErrLicense && strings.Contains(e.Message, "grammar") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected grammar-error ErrLicense under strict mode; got %v", strict)
	}
}
