// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/emit/spdx"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

func strPtr(s string) *string { return &s }
func epochPtr(n int64) *int64 { return &n }

func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	diag.SetSink(&buf)
	diag.Reset()
	t.Cleanup(func() {
		diag.SetSink(nil)
		diag.Reset()
	})
	fn()
	return buf.String()
}

// -----------------------------------------------------------------------------
// Document-level shape.
// -----------------------------------------------------------------------------

func TestEmit_DocumentShape(t *testing.T) {
	primary := &manifest.Component{
		Name: "acme-server", Version: strPtr("1.0.0"),
		Purl: strPtr("pkg:generic/acme/acme-server@1.0.0"),
	}
	out, err := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{
		SourceDateEpoch: epochPtr(1700000000),
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc["spdxVersion"] != "SPDX-2.3" {
		t.Fatalf("spdxVersion: got %v", doc["spdxVersion"])
	}
	if doc["dataLicense"] != "CC0-1.0" {
		t.Fatalf("dataLicense: got %v", doc["dataLicense"])
	}
	if doc["SPDXID"] != "SPDXRef-DOCUMENT" {
		t.Fatalf("SPDXID: got %v", doc["SPDXID"])
	}
	if !strings.HasPrefix(doc["documentNamespace"].(string), "https://interlynk.io/bomtique/spdx/") {
		t.Fatalf("documentNamespace: got %v", doc["documentNamespace"])
	}
	info := doc["creationInfo"].(map[string]any)
	if info["created"] != "2023-11-14T22:13:20Z" {
		t.Fatalf("created: got %v", info["created"])
	}
	creators := info["creators"].([]any)
	if len(creators) != 1 || !strings.HasPrefix(creators[0].(string), "Tool:") {
		t.Fatalf("creators: got %v", creators)
	}
}

func TestEmit_DescribesRelationship(t *testing.T) {
	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	out, err := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{
		SourceDateEpoch: epochPtr(1700000000),
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	rels := doc["relationships"].([]any)
	if len(rels) == 0 {
		t.Fatal("relationships[] empty")
	}
	first := rels[0].(map[string]any)
	if first["spdxElementId"] != "SPDXRef-DOCUMENT" || first["relationshipType"] != "DESCRIBES" {
		t.Fatalf("first relationship is not DESCRIBES: %v", first)
	}
}

// -----------------------------------------------------------------------------
// Package field mapping.
// -----------------------------------------------------------------------------

func TestEmit_PrimaryPackagePurposeMapping(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"library", "LIBRARY"},
		{"application", "APPLICATION"},
		{"framework", "FRAMEWORK"},
		{"container", "CONTAINER"},
		{"operating-system", "OPERATING-SYSTEM"},
		{"device", "DEVICE"},
		{"firmware", "FIRMWARE"},
		{"file", "FILE"},
		{"platform", "OTHER"},
		{"device-driver", "OTHER"},
		{"machine-learning-model", "OTHER"},
		{"data", "OTHER"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			primary := &manifest.Component{Name: "x", Version: strPtr("1"), Type: strPtr(tc.in)}
			out, err := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
			if err != nil {
				t.Fatalf("Emit: %v", err)
			}
			var doc map[string]any
			_ = json.Unmarshal(out, &doc)
			pkg := doc["packages"].([]any)[0].(map[string]any)
			if pkg["primaryPackagePurpose"] != tc.want {
				t.Fatalf("primaryPackagePurpose: got %v, want %q", pkg["primaryPackagePurpose"], tc.want)
			}
		})
	}
}

func TestEmit_LicensePassthrough(t *testing.T) {
	primary := &manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{Expression: "MIT AND BSD-3-Clause"},
	}
	out, _ := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)
	if pkg["licenseConcluded"] != "MIT AND BSD-3-Clause" {
		t.Fatalf("licenseConcluded: got %v", pkg["licenseConcluded"])
	}
	if pkg["licenseDeclared"] != "MIT AND BSD-3-Clause" {
		t.Fatalf("licenseDeclared: got %v", pkg["licenseDeclared"])
	}
}

func TestEmit_LicenseAbsentCollapsesToNOASSERTION(t *testing.T) {
	primary := &manifest.Component{Name: "x", Version: strPtr("1")}
	out, _ := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)
	if pkg["licenseConcluded"] != "NOASSERTION" {
		t.Fatalf("licenseConcluded: got %v", pkg["licenseConcluded"])
	}
}

func TestEmit_LicenseCommentsFromTexts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BSD.txt"), []byte("BSD text body"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	primary := &manifest.Component{
		Name: "x", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "MIT AND BSD-3-Clause",
			Texts: []manifest.LicenseText{
				{ID: "MIT", Text: strPtr("inline MIT text")},
				{ID: "BSD-3-Clause", File: strPtr("./BSD.txt")},
			},
		},
	}
	out, err := spdx.Emit(spdx.EmitInput{Primary: primary, PrimaryDir: dir}, spdx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)
	comments := pkg["licenseComments"].(string)
	if !strings.Contains(comments, "=== MIT ===") || !strings.Contains(comments, "inline MIT text") {
		t.Fatalf("MIT heading/body missing:\n%s", comments)
	}
	if !strings.Contains(comments, "=== BSD-3-Clause ===") || !strings.Contains(comments, "BSD text body") {
		t.Fatalf("BSD heading/body missing:\n%s", comments)
	}
}

// -----------------------------------------------------------------------------
// externalRefs mapping.
// -----------------------------------------------------------------------------

func TestEmit_ExternalRefsMapping(t *testing.T) {
	primary := &manifest.Component{
		Name: "x", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/x@1"),
		CPE:  strPtr("cpe:2.3:a:acme:x:1:*:*:*:*:*:*:*"),
		ExternalReferences: []manifest.ExternalRef{
			{Type: "website", URL: "https://example.com"},
			{Type: "distribution", URL: "https://example.com/download"},
			{Type: "vcs", URL: "https://github.com/example"},
			{Type: "release-notes", URL: "https://example.com/notes"},
		},
	}
	out, _ := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)

	if pkg["homepage"] != "https://example.com" {
		t.Fatalf("homepage: got %v", pkg["homepage"])
	}
	if pkg["downloadLocation"] != "https://example.com/download" {
		t.Fatalf("downloadLocation: got %v", pkg["downloadLocation"])
	}

	refs := pkg["externalRefs"].([]any)
	var sawPurl, sawCPE, sawVCS, sawOther bool
	for _, r := range refs {
		m := r.(map[string]any)
		switch {
		case m["referenceType"] == "purl" && m["referenceCategory"] == "PACKAGE-MANAGER":
			sawPurl = true
		case m["referenceType"] == "cpe23Type" && m["referenceCategory"] == "SECURITY":
			sawCPE = true
		case m["referenceType"] == "vcs" && m["referenceCategory"] == "OTHER":
			sawVCS = true
		case m["referenceType"] == "other":
			sawOther = true
			if !strings.Contains(m["comment"].(string), "release-notes") {
				t.Fatalf("other ref should name original type in comment: %v", m)
			}
		}
	}
	if !sawPurl || !sawCPE || !sawVCS || !sawOther {
		t.Fatalf("missing refs: purl=%v cpe=%v vcs=%v other=%v\n%v", sawPurl, sawCPE, sawVCS, sawOther, refs)
	}
}

// -----------------------------------------------------------------------------
// Checksums — algorithm translation + file-form compute.
// -----------------------------------------------------------------------------

func TestEmit_ChecksumAlgorithmTranslation(t *testing.T) {
	primary := &manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))},
			{Algorithm: "SHA-3-512", Value: strPtr(strings.Repeat("b", 128))},
		},
	}
	out, _ := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)
	sums := pkg["checksums"].([]any)
	if sums[0].(map[string]any)["algorithm"] != "SHA256" {
		t.Fatalf("SHA-256 → SHA256 expected, got %v", sums[0])
	}
	if sums[1].(map[string]any)["algorithm"] != "SHA3-512" {
		t.Fatalf("SHA-3-512 → SHA3-512 expected, got %v", sums[1])
	}
}

func TestEmit_ChecksumFileComputed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abc.txt"), []byte("abc"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	primary := &manifest.Component{
		Name: "x", Version: strPtr("1"),
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-256", File: strPtr("./abc.txt")},
		},
	}
	out, err := spdx.Emit(spdx.EmitInput{Primary: primary, PrimaryDir: dir}, spdx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if !strings.Contains(string(out), abcSHA256) {
		t.Fatalf("expected SHA-256(abc) in output:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// DEPENDS_ON relationships.
// -----------------------------------------------------------------------------

func TestEmit_DependsOnRelationships(t *testing.T) {
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Purl:      strPtr("pkg:generic/app@1"),
		DependsOn: []string{"pkg:generic/libfoo@1"},
	}
	libfoo := manifest.Component{
		Name: "libfoo", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/libfoo@1"),
	}
	out, err := spdx.Emit(spdx.EmitInput{
		Primary:   primary,
		Reachable: []spdx.ReachableComponent{{Component: &libfoo}},
	}, spdx.Options{SourceDateEpoch: epochPtr(1)})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)

	rels := doc["relationships"].([]any)
	var dependsOn []map[string]any
	for _, r := range rels {
		m := r.(map[string]any)
		if m["relationshipType"] == "DEPENDS_ON" {
			dependsOn = append(dependsOn, m)
		}
	}
	if len(dependsOn) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON, got %d", len(dependsOn))
	}
	if !strings.Contains(dependsOn[0]["spdxElementId"].(string), "app") {
		t.Fatalf("source ID: %v", dependsOn[0]["spdxElementId"])
	}
	if !strings.Contains(dependsOn[0]["relatedSpdxElement"].(string), "libfoo") {
		t.Fatalf("target ID: %v", dependsOn[0]["relatedSpdxElement"])
	}
}

// -----------------------------------------------------------------------------
// Dropped-field warnings.
// -----------------------------------------------------------------------------

func TestEmit_ScopeDroppedWithWarning(t *testing.T) {
	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	pool := manifest.Component{
		Name: "libfoo", Version: strPtr("1"),
		Scope: strPtr("optional"),
	}
	warns := captureWarnings(t, func() {
		out, err := spdx.Emit(spdx.EmitInput{
			Primary:   primary,
			Reachable: []spdx.ReachableComponent{{Component: &pool}},
		}, spdx.Options{})
		if err != nil {
			t.Fatalf("Emit: %v", err)
		}
		if strings.Contains(string(out), `"scope"`) {
			t.Fatalf("SPDX output should not carry scope:\n%s", out)
		}
	})
	if !strings.Contains(warns, "dropped field class `scope`") {
		t.Fatalf("expected scope drop warning, got:\n%s", warns)
	}
}

func TestEmit_LifecyclesDroppedWithWarning(t *testing.T) {
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Lifecycles: []manifest.Lifecycle{{Phase: "build"}, {Phase: "post-build"}},
	}
	warns := captureWarnings(t, func() {
		_, err := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
		if err != nil {
			t.Fatalf("Emit: %v", err)
		}
	})
	if !strings.Contains(warns, "dropped field class `metadata.lifecycles`") {
		t.Fatalf("expected lifecycles drop warning:\n%s", warns)
	}
}

func TestEmit_VariantsDescendantsDroppedOncePerRun(t *testing.T) {
	// Two components each carry pedigree.variants and pedigree.descendants
	// — the run must still produce exactly one warning per class.
	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	a := manifest.Component{
		Name: "a", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Variants:    []manifest.Component{{Name: "va", Version: strPtr("1")}},
			Descendants: []manifest.Component{{Name: "da", Version: strPtr("1")}},
		},
	}
	b := manifest.Component{
		Name: "b", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Variants:    []manifest.Component{{Name: "vb", Version: strPtr("1")}},
			Descendants: []manifest.Component{{Name: "db", Version: strPtr("1")}},
		},
	}
	warns := captureWarnings(t, func() {
		_, err := spdx.Emit(spdx.EmitInput{
			Primary: primary,
			Reachable: []spdx.ReachableComponent{
				{Component: &a}, {Component: &b},
			},
		}, spdx.Options{})
		if err != nil {
			t.Fatalf("Emit: %v", err)
		}
	})
	if got := strings.Count(warns, "`pedigree.variants`"); got != 1 {
		t.Fatalf("variants warning fires %d times, want 1:\n%s", got, warns)
	}
	if got := strings.Count(warns, "`pedigree.descendants`"); got != 1 {
		t.Fatalf("descendants warning fires %d times, want 1:\n%s", got, warns)
	}
}

// -----------------------------------------------------------------------------
// Supplier + pedigree sourceInfo + comment + annotations.
// -----------------------------------------------------------------------------

func TestEmit_SupplierFormatted(t *testing.T) {
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Supplier: &manifest.Supplier{Name: "Acme Corp", Email: strPtr("ops@acme.test")},
	}
	out, _ := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
	if !strings.Contains(string(out), `"supplier":"Organization: Acme Corp (ops@acme.test)"`) {
		t.Fatalf("supplier format wrong:\n%s", out)
	}
}

func TestEmit_PedigreeNotesCommentSourceInfoAnnotations(t *testing.T) {
	primary := &manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Ancestors: []manifest.Ancestor{{
				Name:    "upstream",
				Version: strPtr("1.0.0"),
				Purl:    strPtr("pkg:generic/upstream@1.0.0"),
			}},
			Commits: []manifest.Commit{{UID: strPtr("abc123"), URL: strPtr("https://example/abc123")}},
			Patches: []manifest.Patch{{Type: "backport", Resolves: []manifest.Resolves{{Type: strPtr("security"), Name: strPtr("CVE-2024-XXXXX")}}}},
			Notes:   strPtr("forked at abc123"),
		},
	}
	out, err := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{SourceDateEpoch: epochPtr(1)})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)
	if info, _ := pkg["sourceInfo"].(string); !strings.Contains(info, "Ancestor:") || !strings.Contains(info, "upstream@1.0.0") {
		t.Fatalf("sourceInfo missing ancestor: %v", info)
	}
	if info := pkg["sourceInfo"].(string); !strings.Contains(info, "Commit: abc123") {
		t.Fatalf("sourceInfo missing commit: %v", info)
	}
	if pkg["comment"] != "forked at abc123" {
		t.Fatalf("comment: got %v", pkg["comment"])
	}
	ann := pkg["annotations"].([]any)
	if len(ann) != 1 {
		t.Fatalf("annotations: got %d", len(ann))
	}
	a := ann[0].(map[string]any)
	if !strings.Contains(a["comment"].(string), "backport") || !strings.Contains(a["comment"].(string), "CVE-2024-XXXXX") {
		t.Fatalf("annotation comment: got %v", a["comment"])
	}
}

// -----------------------------------------------------------------------------
// Determinism with SOURCE_DATE_EPOCH.
// -----------------------------------------------------------------------------

func TestEmit_DeterministicDocumentNamespace(t *testing.T) {
	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	libfoo := manifest.Component{Name: "libfoo", Version: strPtr("1")}
	in := spdx.EmitInput{
		Primary:   primary,
		Reachable: []spdx.ReachableComponent{{Component: &libfoo}},
	}
	opts := spdx.Options{SourceDateEpoch: epochPtr(1700000000)}

	a, err := spdx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit #1: %v", err)
	}
	b, err := spdx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit #2: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("byte mismatch across runs")
	}
}

// -----------------------------------------------------------------------------
// SPDXID generation.
// -----------------------------------------------------------------------------

func TestEmit_SPDXIDSanitization(t *testing.T) {
	// Name with characters outside SPDX's allowed set gets sanitised;
	// it must still produce a valid SPDXID.
	primary := &manifest.Component{Name: "weird/name@1.0", Version: strPtr("1")}
	out, _ := spdx.Emit(spdx.EmitInput{Primary: primary}, spdx.Options{})
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkg := doc["packages"].([]any)[0].(map[string]any)
	id := pkg["SPDXID"].(string)
	if !strings.HasPrefix(id, "SPDXRef-") {
		t.Fatalf("SPDXID must start with SPDXRef-: %q", id)
	}
	// Allowed charset: [A-Za-z0-9.\-+].
	body := strings.TrimPrefix(id, "SPDXRef-")
	for _, r := range body {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+'
		if !ok {
			t.Fatalf("SPDXID %q contains disallowed rune %q", id, r)
		}
	}
}

func TestEmit_SPDXIDCollisionSafety(t *testing.T) {
	// Two components whose names sanitise to the same base must get
	// distinct SPDXIDs.
	primary := &manifest.Component{Name: "alpha", Version: strPtr("1")}
	a := manifest.Component{Name: "foo/bar", Version: strPtr("1")}
	b := manifest.Component{Name: "foo-bar", Version: strPtr("1")}
	out, err := spdx.Emit(spdx.EmitInput{
		Primary:   primary,
		Reachable: []spdx.ReachableComponent{{Component: &a}, {Component: &b}},
	}, spdx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	pkgs := doc["packages"].([]any)
	seen := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		id := p.(map[string]any)["SPDXID"].(string)
		if seen[id] {
			t.Fatalf("duplicate SPDXID %q", id)
		}
		seen[id] = true
	}
}
