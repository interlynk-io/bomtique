// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
)

// skipFS is the option set for tests that don't touch the filesystem —
// most rule-focused tests set this so they can build Component values
// in code without materialising a backing tree.
var skipFS = validate.Options{SkipFilesystem: true}

// -----------------------------------------------------------------------------
// Constructors for building test manifests in code.
// -----------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

func primaryManifest(c manifest.Component) *manifest.Manifest {
	return &manifest.Manifest{
		Kind:   manifest.KindPrimary,
		Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema:  manifest.SchemaPrimaryV1,
			Primary: c,
		},
	}
}

func componentsManifest(cs ...manifest.Component) *manifest.Manifest {
	return &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatJSON,
		Components: &manifest.ComponentsManifest{
			Schema:     manifest.SchemaComponentsV1,
			Components: cs,
		},
	}
}

func simpleComponent(name, version string) manifest.Component {
	return manifest.Component{
		Name:    name,
		Version: strPtr(version),
	}
}

// expectKind asserts that errs contains exactly one Error of kind k (in
// any position), returning that error for deeper assertions.
func expectKind(t *testing.T, errs []validate.Error, k validate.Kind) validate.Error {
	t.Helper()
	var matches []validate.Error
	for _, e := range errs {
		if e.Kind == k {
			matches = append(matches, e)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one %v error, got %d; all errors: %v", k, len(matches), errs)
	}
	return matches[0]
}

func expectNoKind(t *testing.T, errs []validate.Error, k validate.Kind) {
	t.Helper()
	for _, e := range errs {
		if e.Kind == k {
			t.Fatalf("unexpected %v error: %v", k, e)
		}
	}
}

// -----------------------------------------------------------------------------
// §6.1 Required fields / identity.
// -----------------------------------------------------------------------------

func TestComponent_NameRequired(t *testing.T) {
	m := primaryManifest(manifest.Component{Name: "  "})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrRequiredField)
}

func TestComponent_IdentityRequiresOneOf(t *testing.T) {
	// Name only — no version, purl, or hashes.
	m := primaryManifest(manifest.Component{Name: "only-name"})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrIdentity)
}

func TestComponent_AcceptsPurlAsIdentity(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x",
		Purl: strPtr("pkg:generic/x@1.0.0"),
	})
	errs := validate.Manifest(m, skipFS)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestComponent_AcceptsHashesAsIdentity(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x",
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))},
		},
	})
	errs := validate.Manifest(m, skipFS)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

// -----------------------------------------------------------------------------
// §7 Enumerations.
// -----------------------------------------------------------------------------

func TestComponent_TypeEnum(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Type: strPtr("notarealtype"),
	})
	errs := validate.Manifest(m, skipFS)
	e := expectKind(t, errs, validate.ErrEnumValue)
	if e.Value != "notarealtype" {
		t.Fatalf("value: got %q", e.Value)
	}
}

func TestComponent_ScopeEnum(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Scope: strPtr("Maybe"),
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrEnumValue)
}

func TestComponent_ExternalRefTypeEnum(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		ExternalReferences: []manifest.ExternalRef{
			{Type: "wiki", URL: "https://example"},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrEnumValue)
}

func TestComponent_LifecyclePhaseEnum(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Lifecycles: []manifest.Lifecycle{{Phase: "production"}},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrEnumValue)
}

func TestComponent_AllEnumsAccepted(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Type:  strPtr("library"),
		Scope: strPtr("required"),
		ExternalReferences: []manifest.ExternalRef{
			{Type: "website", URL: "https://example"},
			{Type: "vcs", URL: "https://github.com/example"},
		},
		Lifecycles: []manifest.Lifecycle{{Phase: "build"}},
	})
	errs := validate.Manifest(m, skipFS)
	if len(errs) != 0 {
		t.Fatalf("expected zero errors, got %v", errs)
	}
}

// -----------------------------------------------------------------------------
// §6.2 Supplier.
// -----------------------------------------------------------------------------

func TestComponent_SupplierNameRequired(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Supplier: &manifest.Supplier{Name: "   "},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrRequiredField)
}

// -----------------------------------------------------------------------------
// §6.3 License.
// -----------------------------------------------------------------------------

func TestLicense_ExpressionRequired(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{Expression: "  "},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrLicense)
}

func TestLicense_TextsIDAppearsInExpression(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "MIT",
			Texts: []manifest.LicenseText{
				{ID: "Apache-2.0", File: strPtr("./LICENSE-apache")},
			},
		},
	})
	errs := validate.Manifest(m, skipFS)
	e := expectKind(t, errs, validate.ErrLicense)
	if !strings.Contains(e.Message, "does not appear") {
		t.Fatalf("wrong message: %v", e)
	}
}

func TestLicense_TextsIDInCompoundExpression(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "(MIT AND BSD-3-Clause) OR Apache-2.0",
			Texts: []manifest.LicenseText{
				{ID: "BSD-3-Clause", Text: strPtr("BSD text")},
			},
		},
	})
	errs := validate.Manifest(m, skipFS)
	if len(errs) != 0 {
		t.Fatalf("expected zero errors, got %v", errs)
	}
}

func TestLicense_TextAndFileBothSet(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "MIT",
			Texts: []manifest.LicenseText{
				{ID: "MIT", Text: strPtr("inline"), File: strPtr("./LICENSE")},
			},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrLicense)
}

func TestLicense_NeitherTextNorFile(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "MIT",
			Texts: []manifest.LicenseText{
				{ID: "MIT"},
			},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrLicense)
}

func TestLicense_TextsIDRejectsOperator(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "MIT",
			Texts: []manifest.LicenseText{
				{ID: "MIT OR Apache-2.0", Text: strPtr("x")},
			},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrLicense)
}

// -----------------------------------------------------------------------------
// §6.4 / §9.1 Purl parsing.
// -----------------------------------------------------------------------------

func TestComponent_PurlInvalid(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x",
		Purl: strPtr("not-a-purl"),
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrPurlParse)
}

// -----------------------------------------------------------------------------
// §8 Hash form / algorithm / literal value.
// -----------------------------------------------------------------------------

func TestHash_MixedForms(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{{
			Algorithm: "SHA-256",
			Value:     strPtr(strings.Repeat("a", 64)),
			File:      strPtr("./x"),
		}},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrHashForm)
}

func TestHash_NoForm(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{{Algorithm: "SHA-256"}},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrHashForm)
}

func TestHash_AlgorithmForbidden(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{{
			Algorithm: "MD5",
			Value:     strPtr(strings.Repeat("a", 32)),
		}},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrHashAlgorithm)
}

func TestHash_LiteralValueWrongLength(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{{
			Algorithm: "SHA-256",
			Value:     strPtr("9f86d08..."),
		}},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrHashValue)
}

func TestHash_LiteralValueUppercase(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{{
			Algorithm: "SHA-256",
			Value:     strPtr(strings.Repeat("A", 64)),
		}},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrHashValue)
}

// -----------------------------------------------------------------------------
// §8.2 / §8.3 / §8.4 Filesystem.
// -----------------------------------------------------------------------------

func TestHash_FileMissing(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "m.json")
	if err := os.WriteFile(mp, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := &manifest.Manifest{
		Path:   mp,
		Kind:   manifest.KindPrimary,
		Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema: manifest.SchemaPrimaryV1,
			Primary: manifest.Component{
				Name: "x", Version: strPtr("1"),
				Hashes: []manifest.Hash{{
					Algorithm: "SHA-256",
					File:      strPtr("./nope.c"),
				}},
			},
		},
	}
	errs := validate.Manifest(m, validate.Options{})
	expectKind(t, errs, validate.ErrHashFilesystem)
}

func TestHash_FileTraversal(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "m.json")
	if err := os.WriteFile(mp, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := &manifest.Manifest{
		Path:   mp,
		Kind:   manifest.KindPrimary,
		Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema: manifest.SchemaPrimaryV1,
			Primary: manifest.Component{
				Name: "x", Version: strPtr("1"),
				Hashes: []manifest.Hash{{
					Algorithm: "SHA-256",
					File:      strPtr("../escape.c"),
				}},
			},
		},
	}
	errs := validate.Manifest(m, validate.Options{})
	expectKind(t, errs, validate.ErrPathTraversal)
}

func TestHash_FileSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Windows developer mode")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("exfil"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.c")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	mp := filepath.Join(dir, "m.json")
	if err := os.WriteFile(mp, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := &manifest.Manifest{
		Path:   mp,
		Kind:   manifest.KindPrimary,
		Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema: manifest.SchemaPrimaryV1,
			Primary: manifest.Component{
				Name: "x", Version: strPtr("1"),
				Hashes: []manifest.Hash{{
					Algorithm: "SHA-256",
					File:      strPtr("./link.c"),
				}},
			},
		},
	}
	errs := validate.Manifest(m, validate.Options{})
	expectKind(t, errs, validate.ErrSymlink)
}

func TestHash_DirectoryEmptyAfterFilter(t *testing.T) {
	dir := t.TempDir()
	// Only a non-.c file present.
	if err := os.WriteFile(filepath.Join(dir, "only.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mp := filepath.Join(dir, "m.json")
	if err := os.WriteFile(mp, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := &manifest.Manifest{
		Path:   mp,
		Kind:   manifest.KindPrimary,
		Format: manifest.FormatJSON,
		Primary: &manifest.PrimaryManifest{
			Schema: manifest.SchemaPrimaryV1,
			Primary: manifest.Component{
				Name: "x", Version: strPtr("1"),
				Hashes: []manifest.Hash{{
					Algorithm:  "SHA-256",
					Path:       strPtr("."),
					Extensions: []string{"c"},
				}},
			},
		},
	}
	errs := validate.Manifest(m, validate.Options{})
	expectKind(t, errs, validate.ErrEmptyDirectory)
}

// -----------------------------------------------------------------------------
// §9.3 Patched-purl rule.
// -----------------------------------------------------------------------------

func TestPedigree_PatchedPurlCollision(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/upstream@1.0.0"),
		Pedigree: &manifest.Pedigree{
			Ancestors: []manifest.Ancestor{{
				Name:    "upstream",
				Version: strPtr("1.0.0"),
				Purl:    strPtr("pkg:generic/upstream@1.0.0"),
			}},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrPatchedPurl)
}

func TestPedigree_PatchedPurlQualifierDistinct(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/upstream@1.0.0?vendored=true"),
		Pedigree: &manifest.Pedigree{
			Ancestors: []manifest.Ancestor{{
				Name:    "upstream",
				Version: strPtr("1.0.0"),
				Purl:    strPtr("pkg:generic/upstream@1.0.0"),
			}},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectNoKind(t, errs, validate.ErrPatchedPurl)
}

func TestPedigree_PatchedPurlAncestorRequiresName(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/fork@1.0.0"),
		Pedigree: &manifest.Pedigree{
			Ancestors: []manifest.Ancestor{{
				Purl: strPtr("pkg:generic/upstream@1.0.0"),
			}},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrRequiredField)
}

func TestPedigree_PatchTypeEnum(t *testing.T) {
	m := primaryManifest(manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Patches: []manifest.Patch{{Type: "reversal"}},
		},
	})
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrEnumValue)
}

// -----------------------------------------------------------------------------
// §5.2 / §12.1 / §10.4 Processing set.
// -----------------------------------------------------------------------------

func TestProcessingSet_ZeroPrimaries(t *testing.T) {
	cm := componentsManifest(simpleComponent("a", "1"))
	errs := validate.ProcessingSet([]*manifest.Manifest{cm}, skipFS)
	expectKind(t, errs, validate.ErrProcessingSet)
}

func TestProcessingSet_MultiPrimaryRequiresDependsOn(t *testing.T) {
	p1 := primaryManifest(manifest.Component{Name: "a", Version: strPtr("1"), DependsOn: []string{"pkg:generic/dep@1"}})
	p2 := primaryManifest(manifest.Component{Name: "b", Version: strPtr("1")}) // missing depends-on
	errs := validate.ProcessingSet([]*manifest.Manifest{p1, p2}, skipFS)
	expectKind(t, errs, validate.ErrDependsOn)
}

func TestProcessingSet_SinglePrimaryMayOmitDependsOn(t *testing.T) {
	p := primaryManifest(manifest.Component{Name: "a", Version: strPtr("1")})
	cm := componentsManifest(simpleComponent("b", "1"))
	errs := validate.ProcessingSet([]*manifest.Manifest{p, cm}, skipFS)
	if len(errs) != 0 {
		t.Fatalf("expected zero errors, got %v", errs)
	}
}

func TestComponentsManifest_EmptyComponents(t *testing.T) {
	m := &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatJSON,
		Components: &manifest.ComponentsManifest{
			Schema: manifest.SchemaComponentsV1,
		},
	}
	errs := validate.Manifest(m, skipFS)
	expectKind(t, errs, validate.ErrComponentsMissing)
}

// -----------------------------------------------------------------------------
// Appendix B positive sweep — the clean ones validate with SkipFilesystem=true.
// B.2 and B.8 carry illustrative "9f86d08..." literals that fail hash-value
// checks; B.5 has an ancestor without name (§9.1 mismatch in the spec). Those
// are covered by the negative-rule tests above.
// -----------------------------------------------------------------------------

func TestAppendix_CleanExamplesValidate(t *testing.T) {
	cases := []string{
		"b1.json", "b3_server_primary.json", "b3_worker_primary.json",
		"b3_shared_components.json", "b6.json",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "testdata", "appendix", name))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			m, err := manifest.ParseJSON(data, name)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			errs := validate.Manifest(m, skipFS)
			if len(errs) != 0 {
				t.Fatalf("expected clean validation, got %v", errs)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// CSV row/column reporting sanity.
// -----------------------------------------------------------------------------

func TestError_CSVRowReporting(t *testing.T) {
	m := &manifest.Manifest{
		Kind:   manifest.KindComponents,
		Format: manifest.FormatCSV,
		Components: &manifest.ComponentsManifest{
			Schema: manifest.SchemaComponentsV1,
			Components: []manifest.Component{
				simpleComponent("first", "1"),
				{Name: "  "}, // bad row: empty name
			},
		},
	}
	errs := validate.Manifest(m, skipFS)
	if len(errs) == 0 {
		t.Fatal("expected errors")
	}
	var gotRow int
	var gotCol string
	for _, e := range errs {
		if e.Kind == validate.ErrRequiredField && e.Row > 0 {
			gotRow = e.Row
			gotCol = e.Column
		}
	}
	if gotRow != 2 { // 0-based index 1 → 1-based row 2
		t.Fatalf("expected row 2, got %d", gotRow)
	}
	if gotCol != "name" {
		t.Fatalf("expected column name, got %q", gotCol)
	}
}
