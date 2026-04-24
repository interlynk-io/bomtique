// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"reflect"
	"testing"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

func TestMergeComponent_BothNil(t *testing.T) {
	out, touched := MergeComponent(nil, nil)
	if out != nil {
		t.Fatalf("expected nil out, got %+v", out)
	}
	if len(touched) != 0 {
		t.Fatalf("expected no overrides, got %v", touched)
	}
}

func TestMergeComponent_NilBaseReturnsCloneOfOverrides(t *testing.T) {
	ov := &manifest.Component{Name: "x", Version: strPtr("1")}
	out, touched := MergeComponent(nil, ov)
	if out == ov {
		t.Fatal("MergeComponent must return a clone, not the pointer itself")
	}
	if !reflect.DeepEqual(out, ov) {
		t.Fatalf("clone mismatch\n got: %+v\nwant: %+v", out, ov)
	}
	if len(touched) != 0 {
		t.Fatalf("no base means no overrides: got %v", touched)
	}
}

func TestMergeComponent_NilOverridesReturnsCloneOfBase(t *testing.T) {
	base := &manifest.Component{Name: "x", Version: strPtr("1")}
	out, touched := MergeComponent(base, nil)
	if out == base {
		t.Fatal("MergeComponent must return a clone, not the pointer itself")
	}
	if !reflect.DeepEqual(out, base) {
		t.Fatalf("clone mismatch\n got: %+v\nwant: %+v", out, base)
	}
	if len(touched) != 0 {
		t.Fatalf("nil overrides means no overrides: got %v", touched)
	}
}

func TestMergeComponent_OverrideReplacesScalars(t *testing.T) {
	base := &manifest.Component{
		Name:        "libx",
		Version:     strPtr("1.0"),
		License:     &manifest.License{Expression: "MIT"},
		Description: strPtr("old"),
	}
	ov := &manifest.Component{
		Version:     strPtr("2.0"),
		Description: strPtr("new"),
		Purl:        strPtr("pkg:generic/libx@2.0"),
	}
	out, touched := MergeComponent(base, ov)

	if *out.Version != "2.0" {
		t.Fatalf("version not overridden: %v", derefStr(out.Version))
	}
	if *out.Description != "new" {
		t.Fatalf("description not overridden: %v", derefStr(out.Description))
	}
	if out.Purl == nil || *out.Purl != "pkg:generic/libx@2.0" {
		t.Fatalf("purl not overridden: %v", derefStr(out.Purl))
	}
	// Untouched fields preserved.
	if out.Name != "libx" {
		t.Fatalf("name unexpectedly changed: %q", out.Name)
	}
	if out.License == nil || out.License.Expression != "MIT" {
		t.Fatalf("license unexpectedly changed: %+v", out.License)
	}

	wantTouched := []string{"version", "description", "purl"}
	gotTouched := fieldNames(touched)
	if !reflect.DeepEqual(gotTouched, wantTouched) {
		t.Fatalf("touched list: got %v want %v", gotTouched, wantTouched)
	}
}

func TestMergeComponent_OverrideReplacesNestedObjects(t *testing.T) {
	base := &manifest.Component{
		Name:     "libx",
		Version:  strPtr("1"),
		Supplier: &manifest.Supplier{Name: "Old"},
		License:  &manifest.License{Expression: "MIT"},
		Pedigree: &manifest.Pedigree{Notes: strPtr("old note")},
	}
	ov := &manifest.Component{
		Supplier: &manifest.Supplier{Name: "New"},
		License:  &manifest.License{Expression: "Apache-2.0"},
		Pedigree: &manifest.Pedigree{Notes: strPtr("new note")},
	}
	out, touched := MergeComponent(base, ov)

	if out.Supplier.Name != "New" {
		t.Fatalf("supplier not replaced: %+v", out.Supplier)
	}
	if out.License.Expression != "Apache-2.0" {
		t.Fatalf("license not replaced: %+v", out.License)
	}
	if *out.Pedigree.Notes != "new note" {
		t.Fatalf("pedigree not replaced: %+v", out.Pedigree)
	}

	// All three should be in the touched list.
	want := []string{"supplier", "license", "pedigree"}
	if got := fieldNames(touched); !reflect.DeepEqual(got, want) {
		t.Fatalf("touched: got %v want %v", got, want)
	}
}

func TestMergeComponent_SlicesFullyReplaced(t *testing.T) {
	base := &manifest.Component{
		Name:      "libx",
		Version:   strPtr("1"),
		DependsOn: []string{"a@1", "b@1"},
		Tags:      []string{"core"},
		ExternalReferences: []manifest.ExternalRef{
			{Type: "vcs", URL: "https://old"},
		},
	}
	ov := &manifest.Component{
		DependsOn: []string{"c@1"},
		Tags:      []string{"new"},
		ExternalReferences: []manifest.ExternalRef{
			{Type: "website", URL: "https://new"},
		},
	}
	out, touched := MergeComponent(base, ov)

	if !reflect.DeepEqual(out.DependsOn, []string{"c@1"}) {
		t.Fatalf("depends-on not replaced: %v", out.DependsOn)
	}
	if !reflect.DeepEqual(out.Tags, []string{"new"}) {
		t.Fatalf("tags not replaced: %v", out.Tags)
	}
	if len(out.ExternalReferences) != 1 || out.ExternalReferences[0].URL != "https://new" {
		t.Fatalf("external_references not replaced: %+v", out.ExternalReferences)
	}

	want := []string{"external_references", "depends-on", "tags"}
	if got := fieldNames(touched); !reflect.DeepEqual(got, want) {
		t.Fatalf("touched: got %v want %v", got, want)
	}
}

func TestMergeComponent_NameOverrideOnlyWhenDifferent(t *testing.T) {
	// Override sets the same name as base: no touch.
	base := &manifest.Component{Name: "x", Version: strPtr("1")}
	ov := &manifest.Component{Name: "x"}
	_, touched := MergeComponent(base, ov)
	if len(touched) != 0 {
		t.Fatalf("same-name override should not be counted as a touch: %v", touched)
	}

	// Override sets a different name: touch.
	ov2 := &manifest.Component{Name: "y"}
	out, touched := MergeComponent(base, ov2)
	if out.Name != "y" {
		t.Fatalf("name not replaced: %q", out.Name)
	}
	if fieldNames(touched)[0] != "name" {
		t.Fatalf("name not reported as touched: %v", touched)
	}
}

func TestMergeComponent_DoesNotMutateInputs(t *testing.T) {
	base := &manifest.Component{Name: "x", Version: strPtr("1")}
	ov := &manifest.Component{Version: strPtr("2")}
	baseCopy := *base
	ovCopy := *ov

	_, _ = MergeComponent(base, ov)

	if !reflect.DeepEqual(*base, baseCopy) {
		t.Fatalf("MergeComponent mutated base: %+v", *base)
	}
	if !reflect.DeepEqual(*ov, ovCopy) {
		t.Fatalf("MergeComponent mutated overrides: %+v", *ov)
	}
}

func fieldNames(fs []FieldOverride) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Field
	}
	return out
}

func derefStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
