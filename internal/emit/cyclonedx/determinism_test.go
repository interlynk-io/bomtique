// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/interlynk-io/bomtique/internal/emit/cyclonedx"
	"github.com/interlynk-io/bomtique/internal/manifest"
)

func epochPtr(n int64) *int64 { return &n }

// TestDeterminism_ByteIdentical exercises the §15 guarantee: two runs
// with the same inputs + SOURCE_DATE_EPOCH produce byte-identical
// output. Fixture is rich enough to touch sorting (multiple
// components, multiple hashes, multiple externalRefs) and the serial
// derivation path (non-empty components[]).
func TestDeterminism_ByteIdentical(t *testing.T) {
	in := richFixture()
	opts := cyclonedx.Options{SourceDateEpoch: epochPtr(1730000000)}

	a, err := cyclonedx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit #1: %v", err)
	}
	b, err := cyclonedx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit #2: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("byte-mismatch between runs:\n--- a:\n%s\n--- b:\n%s", a, b)
	}
}

// TestDeterminism_SortComponentsByBomRef asserts that the emitted
// components[] follows §15.2's bom-ref-ascending rule even when the
// input is in reverse order.
func TestDeterminism_SortComponentsByBomRef(t *testing.T) {
	primary := &manifest.Component{Name: "p", Version: strPtr("1"), Purl: strPtr("pkg:generic/p@1")}
	pz := manifest.Component{Name: "z", Version: strPtr("1"), Purl: strPtr("pkg:generic/z@1")}
	pa := manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")}
	pm := manifest.Component{Name: "m", Version: strPtr("1"), Purl: strPtr("pkg:generic/m@1")}

	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:   primary,
		Reachable: []cyclonedx.ReachableComponent{{Component: &pz}, {Component: &pa}, {Component: &pm}},
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	comps := bom["components"].([]any)
	want := []string{"pkg:generic/a@1", "pkg:generic/m@1", "pkg:generic/z@1"}
	for i, c := range comps {
		got := c.(map[string]any)["bom-ref"].(string)
		if got != want[i] {
			t.Fatalf("components[%d]: got %q, want %q", i, got, want[i])
		}
	}
}

// TestDeterminism_SortHashesAndExternalRefs asserts per-component
// sorting of nested arrays.
func TestDeterminism_SortHashesAndExternalRefs(t *testing.T) {
	primary := &manifest.Component{
		Name: "p", Version: strPtr("1"),
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-512", Value: strPtr(strings.Repeat("c", 128))},
			{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("b", 64))},
			{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))},
		},
		ExternalReferences: []manifest.ExternalRef{
			{Type: "vcs", URL: "https://github.com/example"},
			{Type: "documentation", URL: "https://docs.example.com"},
			{Type: "website", URL: "https://example.com"},
		},
	}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	comp := bom["metadata"].(map[string]any)["component"].(map[string]any)

	hashes := comp["hashes"].([]any)
	if hashes[0].(map[string]any)["alg"] != "SHA-256" {
		t.Fatalf("hashes[0] alg: got %v", hashes[0].(map[string]any)["alg"])
	}
	// Within same alg, sort by content ascending.
	if h0 := hashes[0].(map[string]any)["content"].(string); h0 != strings.Repeat("a", 64) {
		t.Fatalf("hashes[0] content: got %q", h0)
	}

	refs := comp["externalReferences"].([]any)
	if refs[0].(map[string]any)["type"] != "documentation" {
		t.Fatalf("externalReferences[0]: got %v", refs[0].(map[string]any)["type"])
	}
	if refs[1].(map[string]any)["type"] != "vcs" {
		t.Fatalf("externalReferences[1]: got %v", refs[1].(map[string]any)["type"])
	}
	if refs[2].(map[string]any)["type"] != "website" {
		t.Fatalf("externalReferences[2]: got %v", refs[2].(map[string]any)["type"])
	}
}

// TestDeterminism_SortDependsOn asserts each dependsOn array is sorted.
func TestDeterminism_SortDependsOn(t *testing.T) {
	primary := &manifest.Component{
		Name: "p", Version: strPtr("1"), Purl: strPtr("pkg:generic/p@1"),
		DependsOn: []string{"pkg:generic/z@1", "pkg:generic/a@1", "pkg:generic/m@1"},
	}
	pz := manifest.Component{Name: "z", Version: strPtr("1"), Purl: strPtr("pkg:generic/z@1")}
	pa := manifest.Component{Name: "a", Version: strPtr("1"), Purl: strPtr("pkg:generic/a@1")}
	pm := manifest.Component{Name: "m", Version: strPtr("1"), Purl: strPtr("pkg:generic/m@1")}

	out, err := cyclonedx.Emit(cyclonedx.EmitInput{
		Primary:   primary,
		Reachable: []cyclonedx.ReachableComponent{{Component: &pz}, {Component: &pa}, {Component: &pm}},
	}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	deps := bom["dependencies"].([]any)
	for _, d := range deps {
		m := d.(map[string]any)
		if m["ref"] != "pkg:generic/p@1" {
			continue
		}
		list := m["dependsOn"].([]any)
		want := []string{"pkg:generic/a@1", "pkg:generic/m@1", "pkg:generic/z@1"}
		for i, v := range list {
			if v != want[i] {
				t.Fatalf("dependsOn[%d]: got %v, want %v", i, v, want[i])
			}
		}
	}
}

// TestDeterminism_Timestamp asserts ISO 8601 UTC-second formatting.
func TestDeterminism_Timestamp(t *testing.T) {
	primary := &manifest.Component{Name: "p", Version: strPtr("1")}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{
		SourceDateEpoch: epochPtr(1234567890),
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	meta := bom["metadata"].(map[string]any)
	// 1234567890 = 2009-02-13T23:31:30Z
	if ts := meta["timestamp"]; ts != "2009-02-13T23:31:30Z" {
		t.Fatalf("timestamp: got %v, want 2009-02-13T23:31:30Z", ts)
	}
}

// TestDeterminism_SerialUUIDv5 asserts the serial is a UUIDv5
// urn:uuid:<...> form and is stable across runs.
func TestDeterminism_SerialUUIDv5(t *testing.T) {
	in := richFixture()
	opts := cyclonedx.Options{SourceDateEpoch: epochPtr(1000)}

	out1, err := cyclonedx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	out2, err := cyclonedx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit #2: %v", err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(out1, &a)
	_ = json.Unmarshal(out2, &b)
	sa, _ := a["serialNumber"].(string)
	sb, _ := b["serialNumber"].(string)
	if !strings.HasPrefix(sa, "urn:uuid:") {
		t.Fatalf("serialNumber not urn:uuid form: %q", sa)
	}
	if sa != sb {
		t.Fatalf("serial not stable: %q vs %q", sa, sb)
	}
	// §15.3 derives the serial from the components[] array specifically
	// (not metadata.component), so mutating a pool component's name
	// must shift the serial.
	in.Reachable[0].Component.Name = in.Reachable[0].Component.Name + "-variant"
	out3, err := cyclonedx.Emit(in, opts)
	if err != nil {
		t.Fatalf("Emit #3: %v", err)
	}
	var c map[string]any
	_ = json.Unmarshal(out3, &c)
	sc, _ := c["serialNumber"].(string)
	if sc == sa {
		t.Fatalf("serial did not change for differing components[]: %q", sa)
	}
}

// TestDeterminism_NoSDENoSerialNoTimestamp asserts that without
// SOURCE_DATE_EPOCH the emitter omits timestamp and serialNumber
// rather than inventing non-deterministic values.
func TestDeterminism_NoSDENoSerialNoTimestamp(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	primary := &manifest.Component{Name: "p", Version: strPtr("1")}
	out, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bom map[string]any
	_ = json.Unmarshal(out, &bom)
	if _, has := bom["serialNumber"]; has {
		t.Fatalf("serialNumber should be absent without SDE: %v", bom["serialNumber"])
	}
	meta := bom["metadata"].(map[string]any)
	if ts, has := meta["timestamp"]; has {
		t.Fatalf("timestamp should be absent without SDE: %v", ts)
	}
}

// TestDeterminism_EnvSDE asserts that setting SOURCE_DATE_EPOCH via env
// behaves identically to passing Options.SourceDateEpoch.
func TestDeterminism_EnvSDE(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1730000000")
	primary := &manifest.Component{Name: "p", Version: strPtr("1")}
	fromEnv, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err != nil {
		t.Fatalf("Emit (env): %v", err)
	}

	t.Setenv("SOURCE_DATE_EPOCH", "")
	fromOpts, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{
		SourceDateEpoch: epochPtr(1730000000),
	})
	if err != nil {
		t.Fatalf("Emit (opts): %v", err)
	}
	if !bytes.Equal(fromEnv, fromOpts) {
		t.Fatalf("env and opts forms produced different bytes:\n env: %s\nopts: %s", fromEnv, fromOpts)
	}
}

// TestDeterminism_SDENegativeError asserts rejection of negative SDE.
func TestDeterminism_SDENegativeError(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "-1")
	primary := &manifest.Component{Name: "p", Version: strPtr("1")}
	_, err := cyclonedx.Emit(cyclonedx.EmitInput{Primary: primary}, cyclonedx.Options{})
	if err == nil {
		t.Fatal("expected error for negative SDE")
	}
	if !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("expected non-negative message: %v", err)
	}
}

// richFixture builds a complex EmitInput that exercises sorting,
// dependency resolution, pedigree, hashes, external refs, lifecycles,
// and scope on pool components.
func richFixture() cyclonedx.EmitInput {
	primary := &manifest.Component{
		Name: "acme-server", Version: strPtr("1.0.0"),
		Purl:       strPtr("pkg:generic/acme/server@1.0.0"),
		DependsOn:  []string{"pkg:generic/zlib@1", "pkg:generic/libfoo@1"},
		Lifecycles: []manifest.Lifecycle{{Phase: "build"}},
	}
	libfoo := manifest.Component{
		Name: "libfoo", Version: strPtr("1"),
		Purl:  strPtr("pkg:generic/libfoo@1"),
		Scope: strPtr("required"),
		Hashes: []manifest.Hash{
			{Algorithm: "SHA-256", Value: strPtr(strings.Repeat("a", 64))},
		},
		ExternalReferences: []manifest.ExternalRef{
			{Type: "website", URL: "https://libfoo.example"},
		},
	}
	zlib := manifest.Component{
		Name: "zlib", Version: strPtr("1"),
		Purl:  strPtr("pkg:generic/zlib@1"),
		Scope: strPtr("required"),
	}
	return cyclonedx.EmitInput{
		Primary:   primary,
		Reachable: []cyclonedx.ReachableComponent{{Component: &libfoo}, {Component: &zlib}},
	}
}
