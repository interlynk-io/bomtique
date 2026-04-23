package purl_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/interlynk-io/bomtique/internal/purl"
)

// specTestDir points to a clone of the purl-spec test suite under
// testdata/. Populate with:
//
//	git clone --depth=1 https://github.com/package-url/purl-spec testdata/purl-spec
//
// If the directory is missing, the tests are skipped rather than failed so
// a fresh clone of bomtique still builds cleanly.
const specTestDir = "../../testdata/purl-spec/tests"

type testFile struct {
	Tests []testCase `json:"tests"`
}

type testCase struct {
	Description           string          `json:"description"`
	TestGroup             string          `json:"test_group"`
	TestType              string          `json:"test_type"`
	Input                 json.RawMessage `json:"input"`
	ExpectedOutput        json.RawMessage `json:"expected_output"`
	ExpectedFailure       bool            `json:"expected_failure"`
	ExpectedFailureReason *string         `json:"expected_failure_reason"`
}

type parseInput struct {
	Type       *string           `json:"type"`
	Namespace  *string           `json:"namespace"`
	Name       *string           `json:"name"`
	Version    *string           `json:"version"`
	Qualifiers map[string]string `json:"qualifiers"`
	Subpath    *string           `json:"subpath"`
}

func loadTestFile(t *testing.T, path string) testFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var tf testFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return tf
}

func nullableStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestSpecificationTests(t *testing.T) {
	path := filepath.Join(specTestDir, "spec", "specification-test.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("spec test suite not found at %s", path)
	}
	tf := loadTestFile(t, path)
	runTestCases(t, tf.Tests)
}

func TestTypeTests(t *testing.T) {
	dir := filepath.Join(specTestDir, "types")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("type test suite not found at %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			tf := loadTestFile(t, path)
			runTestCases(t, tf.Tests)
		})
	}
}

func runTestCases(t *testing.T, tests []testCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.Description+" ["+tc.TestType+"]", func(t *testing.T) {
			switch tc.TestType {
			case "parse":
				runParseTest(t, tc)
			case "build":
				runBuildTest(t, tc)
			case "roundtrip":
				runRoundtripTest(t, tc)
			default:
				t.Fatalf("unknown test_type: %s", tc.TestType)
			}
		})
	}
}

func runParseTest(t *testing.T, tc testCase) {
	t.Helper()

	var input string
	if err := json.Unmarshal(tc.Input, &input); err != nil {
		t.Fatalf("unmarshalling parse input: %v", err)
	}

	p, err := purl.Parse(input)

	if tc.ExpectedFailure {
		if err == nil {
			t.Errorf("expected failure but got success: %s", p.String())
		}
		return
	}

	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	var expected parseInput
	if err := json.Unmarshal(tc.ExpectedOutput, &expected); err != nil {
		t.Fatalf("unmarshalling expected output: %v", err)
	}

	if p.Type != nullableStr(expected.Type) {
		t.Errorf("type: got %q, want %q", p.Type, nullableStr(expected.Type))
	}
	if p.Namespace != nullableStr(expected.Namespace) {
		t.Errorf("namespace: got %q, want %q", p.Namespace, nullableStr(expected.Namespace))
	}
	if p.Name != nullableStr(expected.Name) {
		t.Errorf("name: got %q, want %q", p.Name, nullableStr(expected.Name))
	}
	if p.Version != nullableStr(expected.Version) {
		t.Errorf("version: got %q, want %q", p.Version, nullableStr(expected.Version))
	}
	if p.Subpath != nullableStr(expected.Subpath) {
		t.Errorf("subpath: got %q, want %q", p.Subpath, nullableStr(expected.Subpath))
	}

	if expected.Qualifiers == nil && len(p.Qualifiers) > 0 {
		t.Errorf("qualifiers: got %v, want nil", p.Qualifiers)
	}
	if expected.Qualifiers != nil {
		if len(p.Qualifiers) != len(expected.Qualifiers) {
			t.Errorf("qualifiers count: got %d, want %d", len(p.Qualifiers), len(expected.Qualifiers))
		}
		for k, want := range expected.Qualifiers {
			if got, ok := p.Qualifiers[k]; !ok {
				t.Errorf("missing qualifier %q", k)
			} else if got != want {
				t.Errorf("qualifier %q: got %q, want %q", k, got, want)
			}
		}
	}
}

func runBuildTest(t *testing.T, tc testCase) {
	t.Helper()

	var input parseInput
	if err := json.Unmarshal(tc.Input, &input); err != nil {
		t.Fatalf("unmarshalling build input: %v", err)
	}

	p, err := purl.Build(
		nullableStr(input.Type),
		nullableStr(input.Namespace),
		nullableStr(input.Name),
		nullableStr(input.Version),
		input.Qualifiers,
		nullableStr(input.Subpath),
	)

	if tc.ExpectedFailure {
		if err == nil {
			t.Errorf("expected failure but got success: %s", p.String())
		}
		return
	}

	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	var expected string
	if err := json.Unmarshal(tc.ExpectedOutput, &expected); err != nil {
		t.Fatalf("unmarshalling expected output: %v", err)
	}

	got := p.String()
	if got != expected {
		t.Errorf("build:\n  got  %q\n  want %q", got, expected)
	}
}

func runRoundtripTest(t *testing.T, tc testCase) {
	t.Helper()

	var input string
	if err := json.Unmarshal(tc.Input, &input); err != nil {
		t.Fatalf("unmarshalling roundtrip input: %v", err)
	}

	p, err := purl.Parse(input)

	if tc.ExpectedFailure {
		if err == nil {
			t.Errorf("expected failure but got success: %s", p.String())
		}
		return
	}

	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	var expected string
	if err := json.Unmarshal(tc.ExpectedOutput, &expected); err != nil {
		t.Fatalf("unmarshalling expected output: %v", err)
	}

	got := p.String()
	if got != expected {
		t.Errorf("roundtrip:\n  got  %q\n  want %q", got, expected)
	}
}
