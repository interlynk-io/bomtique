// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/emit/cyclonedx"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

func strPtr(s string) *string { return &s }

// -----------------------------------------------------------------------------
// Minimum-viable primary → metadata.component.
// -----------------------------------------------------------------------------

func TestEmit_MinimalPrimary(t *testing.T) {
	primary := &manifest.Component{
		Name:    "acme-app",
		Version: strPtr("1.0.0"),
		Purl:    strPtr("pkg:generic/acme/acme-app@1.0.0"),
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var bom map[string]any
	if err := json.Unmarshal(out, &bom); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if bom["bomFormat"] != "CycloneDX" {
		t.Fatalf("bomFormat: got %v", bom["bomFormat"])
	}
	if bom["specVersion"] != "1.7" {
		t.Fatalf("specVersion: got %v", bom["specVersion"])
	}

	meta, ok := bom["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing or wrong type: %T", bom["metadata"])
	}
	comp, ok := meta["component"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.component missing")
	}
	if comp["name"] != "acme-app" {
		t.Fatalf("metadata.component.name: got %v", comp["name"])
	}
	if comp["bom-ref"] != "pkg:generic/acme/acme-app@1.0.0" {
		t.Fatalf("primary bom-ref: got %v", comp["bom-ref"])
	}
	if _, has := comp["scope"]; has {
		t.Fatalf("primary must not carry scope per §5.3 / §14.1")
	}
}

// -----------------------------------------------------------------------------
// Reachable pool → components[]; scope preserved on pool; tags dropped.
// -----------------------------------------------------------------------------

func TestEmit_PoolComponentHasScopeTagsDropped(t *testing.T) {
	primary := &manifest.Component{Name: "p", Version: strPtr("1")}
	pool := manifest.Component{
		Name:    "libfoo",
		Version: strPtr("2.0"),
		Purl:    strPtr("pkg:generic/libfoo@2.0"),
		Scope:   strPtr("optional"),
		Tags:    []string{"core", "networking"},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:   primary,
		Reachable: []cyclonedx.ReachableComponent{{Component: &pool}},
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if strings.Contains(string(out), `"tags"`) {
		t.Fatalf("tags must not be serialized: %s", out)
	}

	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	comps, _ := bom["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components: got %d, want 1", len(comps))
	}
	c := comps[0].(map[string]any)
	if c["scope"] != "optional" {
		t.Fatalf("pool component scope: got %v", c["scope"])
	}
}

// -----------------------------------------------------------------------------
// Licenses: expression + per-id text (inline + file).
// -----------------------------------------------------------------------------

func TestEmit_LicenseExpressionAndTexts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BSD-3.txt"), []byte("BSD text"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		License: &manifest.License{
			Expression: "MIT AND BSD-3-Clause",
			Texts: []manifest.LicenseText{
				{ID: "MIT", Text: strPtr("MIT inline")},
				{ID: "BSD-3-Clause", File: strPtr("./BSD-3.txt")},
			},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:    primary,
		PrimaryDir: dir,
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	comp := meta["component"].(map[string]any)
	licenses, _ := comp["licenses"].([]any)
	if len(licenses) != 3 {
		t.Fatalf("licenses: got %d entries, want 3 (1 expression + 2 texts)", len(licenses))
	}

	var sawExpr, sawMIT, sawBSD bool
	for _, l := range licenses {
		m := l.(map[string]any)
		if expr, ok := m["expression"].(string); ok && expr == "MIT AND BSD-3-Clause" {
			sawExpr = true
			continue
		}
		lic, _ := m["license"].(map[string]any)
		id, _ := lic["id"].(string)
		text, _ := lic["text"].(map[string]any)
		switch id {
		case "MIT":
			sawMIT = true
			if text["contentType"] != "text/plain" || text["content"] != "MIT inline" {
				t.Fatalf("MIT text attachment wrong: %+v", text)
			}
			if _, has := text["encoding"]; has {
				t.Fatalf("inline text should not carry encoding: %+v", text)
			}
		case "BSD-3-Clause":
			sawBSD = true
			if text["encoding"] != "base64" {
				t.Fatalf("BSD-3-Clause file attachment should be base64: %+v", text)
			}
			decoded, err := base64.StdEncoding.DecodeString(text["content"].(string))
			if err != nil {
				t.Fatalf("base64 decode: %v", err)
			}
			if string(decoded) != "BSD text" {
				t.Fatalf("decoded content mismatch: %q", decoded)
			}
		}
	}
	if !sawExpr || !sawMIT || !sawBSD {
		t.Fatalf("missing entries: expr=%v mit=%v bsd=%v", sawExpr, sawMIT, sawBSD)
	}
}

// -----------------------------------------------------------------------------
// Hashes: literal passthrough + algorithm name translation.
// -----------------------------------------------------------------------------

func TestEmit_HashLiteralPassthrough(t *testing.T) {
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))},
			{Algorithm: "SHA-3-256", Value: strPtr(strings.Repeat("b", 64))},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	comp := bom["metadata"].(map[string]any)["component"].(map[string]any)
	hashes, _ := comp["hashes"].([]any)
	if len(hashes) != 2 {
		t.Fatalf("hashes: got %d", len(hashes))
	}
	// SHA-256 unchanged, SHA-3-256 → SHA3-256 (CDX form).
	if alg := hashes[0].(map[string]any)["alg"]; alg != "SHA-256" {
		t.Fatalf("alg[0]: got %v", alg)
	}
	if alg := hashes[1].(map[string]any)["alg"]; alg != "SHA3-256" {
		t.Fatalf("alg[1]: got %v (want SHA3-256)", alg)
	}
}

// -----------------------------------------------------------------------------
// Hashes: file form computes digest against manifestDir.
// -----------------------------------------------------------------------------

func TestEmit_HashFileComputed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abc.txt"), []byte("abc"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-256", File: strPtr("./abc.txt")},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:    primary,
		PrimaryDir: dir,
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// FIPS 180-4 vector for "abc".
	const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if !strings.Contains(string(out), abcSHA256) {
		t.Fatalf("expected SHA-256(abc) in output, got: %s", out)
	}
}

// -----------------------------------------------------------------------------
// Patch diff attachment rules §9.2.
// -----------------------------------------------------------------------------

func TestEmit_PatchDiffStringBecomesAttachment(t *testing.T) {
	primary := &manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Patches: []manifest.Patch{{
				Type: "backport",
				Diff: &manifest.Diff{Text: &manifest.Attachment{Content: "--- a\n+++ b\n"}},
			}},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	comp := meta["component"].(map[string]any)
	pedigree := comp["pedigree"].(map[string]any)
	patches := pedigree["patches"].([]any)
	diff := patches[0].(map[string]any)["diff"].(map[string]any)
	text := diff["text"].(map[string]any)
	if text["contentType"] != "text/plain" {
		t.Fatalf("contentType: got %v", text["contentType"])
	}
	if text["content"] != "--- a\n+++ b\n" {
		t.Fatalf("content: got %v", text["content"])
	}
}

func TestEmit_PatchDiffHTTPUrlPreserved(t *testing.T) {
	primary := &manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Patches: []manifest.Patch{{
				Type: "backport",
				Diff: &manifest.Diff{URL: strPtr("https://example.com/fix.patch")},
			}},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(string(out), "https://example.com/fix.patch") {
		t.Fatalf("http url not preserved: %s", out)
	}
}

func TestEmit_PatchDiffLocalURLEmbedded(t *testing.T) {
	dir := t.TempDir()
	patchBytes := []byte("--- a.c\n+++ b.c\n@@ -1 +1 @@\n-old\n+new\n")
	if err := os.WriteFile(filepath.Join(dir, "fix.patch"), patchBytes, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	primary := &manifest.Component{
		Name: "fork", Version: strPtr("1"),
		Pedigree: &manifest.Pedigree{
			Patches: []manifest.Patch{{
				Type: "backport",
				Diff: &manifest.Diff{URL: strPtr("./fix.patch")},
			}},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:    primary,
		PrimaryDir: dir,
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	comp := meta["component"].(map[string]any)
	pedigree := comp["pedigree"].(map[string]any)
	patches := pedigree["patches"].([]any)
	diff := patches[0].(map[string]any)["diff"].(map[string]any)
	text := diff["text"].(map[string]any)
	if text["encoding"] != "base64" {
		t.Fatalf("encoding: got %v", text["encoding"])
	}
	decoded, err := base64.StdEncoding.DecodeString(text["content"].(string))
	if err != nil {
		t.Fatalf("b64 decode: %v", err)
	}
	if string(decoded) != string(patchBytes) {
		t.Fatalf("decoded bytes mismatch")
	}
	if _, has := diff["url"]; has {
		t.Fatalf("local url should be consumed, not preserved in output: %+v", diff)
	}
}

// -----------------------------------------------------------------------------
// dependencies[]: primary + pool entries with resolved refs.
// -----------------------------------------------------------------------------

func TestEmit_DependenciesLinkPrimaryToPool(t *testing.T) {
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Purl:      strPtr("pkg:generic/app@1"),
		DependsOn: []string{"pkg:generic/libfoo@1"},
	}
	libfoo := manifest.Component{
		Name: "libfoo", Version: strPtr("1"),
		Purl:      strPtr("pkg:generic/libfoo@1"),
		DependsOn: []string{"libbar@1"},
	}
	libbar := manifest.Component{Name: "libbar", Version: strPtr("1")}

	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary: primary,
		Reachable: []cyclonedx.ReachableComponent{
			{Component: &libfoo},
			{Component: &libbar},
		},
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	deps, _ := bom["dependencies"].([]any)
	if len(deps) != 3 {
		t.Fatalf("dependencies: got %d, want 3 (primary + 2 pool)", len(deps))
	}

	// §15.2 sorts dependencies[] by ref; look up entries by ref rather
	// than assuming positional order.
	byRef := make(map[string]map[string]any, len(deps))
	for _, d := range deps {
		m := d.(map[string]any)
		byRef[m["ref"].(string)] = m
	}

	primaryDep, ok := byRef["pkg:generic/app@1"]
	if !ok {
		t.Fatalf("no primary dep entry in %v", byRef)
	}
	plist := primaryDep["dependsOn"].([]any)
	if len(plist) != 1 || plist[0] != "pkg:generic/libfoo@1" {
		t.Fatalf("primary dependsOn: got %v", plist)
	}

	libfooDep, ok := byRef["pkg:generic/libfoo@1"]
	if !ok {
		t.Fatalf("no libfoo dep entry in %v", byRef)
	}
	libfooList := libfooDep["dependsOn"].([]any)
	if len(libfooList) != 1 {
		t.Fatalf("libfoo dependsOn: got %v", libfooList)
	}
	libbarRef := libfooList[0].(string)
	if !strings.HasPrefix(libbarRef, "pkg:generic/libbar") {
		t.Fatalf("libbar derived bom-ref unexpected: %v", libbarRef)
	}
	if _, ok := byRef[libbarRef]; !ok {
		t.Fatalf("libbar ref %q is missing its own dependencies entry", libbarRef)
	}
}

// -----------------------------------------------------------------------------
// bom-ref derivation precedence.
// -----------------------------------------------------------------------------

func TestEmit_BOMRefExplicitOverridesPurl(t *testing.T) {
	primary := &manifest.Component{
		BOMRef:  strPtr("urn:app:root"),
		Name:    "app",
		Version: strPtr("1"),
		Purl:    strPtr("pkg:generic/app@1"),
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	comp := meta["component"].(map[string]any)
	if comp["bom-ref"] != "urn:app:root" {
		t.Fatalf("explicit bom-ref should win: got %v", comp["bom-ref"])
	}
}

func TestEmit_BOMRefFallbackForNoPurlNoExplicit(t *testing.T) {
	primary := &manifest.Component{
		Name:    "app",
		Version: strPtr("1.0.0"),
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	comp := meta["component"].(map[string]any)
	if comp["bom-ref"] != "pkg:generic/app@1.0.0" {
		t.Fatalf("fallback bom-ref: got %v", comp["bom-ref"])
	}
}

func TestEmit_BOMRefCollisionIsError(t *testing.T) {
	primary := &manifest.Component{
		Name: "app", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/shared@1"),
	}
	pool := manifest.Component{
		Name: "copy", Version: strPtr("1"),
		Purl: strPtr("pkg:generic/shared@1"),
	}
	_, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:   primary,
		Reachable: []cyclonedx.ReachableComponent{{Component: &pool}},
	}, cyclonedx.Options{})
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Fatalf("collision error shape: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Lifecycle default.
// -----------------------------------------------------------------------------

func TestEmit_LifecyclesDefaultBuild(t *testing.T) {
	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	lcs := meta["lifecycles"].([]any)
	if len(lcs) != 1 || lcs[0].(map[string]any)["phase"] != "build" {
		t.Fatalf("default lifecycle should be [{phase: build}], got %v", lcs)
	}
}

// -----------------------------------------------------------------------------
// Struct ordering — bomFormat first, specVersion next.
// -----------------------------------------------------------------------------

func TestEmit_FieldOrderTopLevel(t *testing.T) {
	primary := &manifest.Component{Name: "app", Version: strPtr("1")}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	s := string(out)
	// Because we hand-rolled the top-level struct, bomFormat must appear
	// before specVersion in the serialised output.
	idxBOM := strings.Index(s, `"bomFormat"`)
	idxSpec := strings.Index(s, `"specVersion"`)
	if idxBOM == -1 || idxSpec == -1 || idxBOM > idxSpec {
		t.Fatalf("field order broken: bomFormat=%d specVersion=%d", idxBOM, idxSpec)
	}
}
