// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDogfood_RegenerateByteIdentical proves the committed
// `.primary.json` and `.components.json` at the repo root can be
// rebuilt byte-for-byte by driving `manifest init` + `manifest add`
// against an empty directory. If this test fails, either a mutate
// writer regression landed OR the dogfood manifests drifted by hand;
// in either case the fix is to re-run the recipe in
// scripts/regenerate-dogfood.sh.
//
// The recipe deliberately uses flag-driven adds with no --ref, so no
// registry fetches happen and the test is hermetic.
func TestDogfood_RegenerateByteIdentical(t *testing.T) {
	repoRoot := findRepoRootFromCWD(t)
	tmp := t.TempDir()

	runOK := func(args ...string) {
		t.Helper()
		full := append([]string{args[0], args[1], "-C", tmp}, args[2:]...)
		_, stderr, err := withArgs(t, full...)
		if got := exitCodeOf(err); got != exitOK {
			t.Fatalf("cmd %v exited %d:\n%s", args, got, stderr.String())
		}
	}

	// init: matches the committed primary's header fields.
	runOK(
		"manifest", "init",
		"--name", "bomtique",
		"--version", "0.1.0",
		"--type", "application",
		"--description", "Reference consumer for the Component Manifest v1 specification",
		"--supplier", "Interlynk.io",
		"--supplier-url", "https://interlynk.io",
		"--license", "Apache-2.0",
		"--purl", "pkg:github/interlynk-io/bomtique@0.1.0",
		"--website", "https://github.com/interlynk-io/bomtique",
		"--vcs", "https://github.com/interlynk-io/bomtique",
		"--issue-tracker", "https://github.com/interlynk-io/bomtique/issues",
	)

	// Pool adds in the order they appear in the committed
	// .components.json.
	type dep struct {
		name, version, license, purl, vcs string
		dependsOn                         []string
	}
	pool := []dep{
		{name: "github.com/google/uuid", version: "v1.6.0", license: "BSD-3-Clause",
			purl: "pkg:golang/github.com/google/uuid@v1.6.0",
			vcs:  "https://github.com/google/uuid"},
		{name: "github.com/santhosh-tekuri/jsonschema/v6", version: "v6.0.2", license: "Apache-2.0",
			purl: "pkg:golang/github.com/santhosh-tekuri/jsonschema/v6@v6.0.2",
			vcs:  "https://github.com/santhosh-tekuri/jsonschema"},
		{name: "github.com/spf13/cobra", version: "v1.10.2", license: "Apache-2.0",
			purl: "pkg:golang/github.com/spf13/cobra@v1.10.2",
			vcs:  "https://github.com/spf13/cobra",
			dependsOn: []string{
				"pkg:golang/github.com/inconshreveable/mousetrap@v1.1.0",
				"pkg:golang/github.com/spf13/pflag@v1.0.9",
			}},
		{name: "golang.org/x/text", version: "v0.36.0", license: "BSD-3-Clause",
			purl: "pkg:golang/golang.org/x/text@v0.36.0",
			vcs:  "https://cs.opensource.google/go/x/text"},
		{name: "github.com/inconshreveable/mousetrap", version: "v1.1.0", license: "Apache-2.0",
			purl: "pkg:golang/github.com/inconshreveable/mousetrap@v1.1.0",
			vcs:  "https://github.com/inconshreveable/mousetrap"},
		{name: "github.com/spf13/pflag", version: "v1.0.9", license: "BSD-3-Clause",
			purl: "pkg:golang/github.com/spf13/pflag@v1.0.9",
			vcs:  "https://github.com/spf13/pflag"},
	}
	for _, d := range pool {
		args := []string{"manifest", "add",
			"--name", d.name,
			"--version", d.version,
			"--type", "library",
			"--license", d.license,
			"--purl", d.purl,
			"--vcs", d.vcs,
		}
		for _, dep := range d.dependsOn {
			args = append(args, "--depends-on", dep)
		}
		runOK(args...)
	}

	// Primary depends-on refs, in the order they appear in the
	// committed primary. We pass --name + --version solely so the
	// mutate.Add validator is happy; the ref derivation uses the
	// purl.
	primaryDeps := []struct{ name, version, purl string }{
		{"uuid", "v1.6.0", "pkg:golang/github.com/google/uuid@v1.6.0"},
		{"jsonschema", "v6.0.2", "pkg:golang/github.com/santhosh-tekuri/jsonschema/v6@v6.0.2"},
		{"cobra", "v1.10.2", "pkg:golang/github.com/spf13/cobra@v1.10.2"},
		{"text", "v0.36.0", "pkg:golang/golang.org/x/text@v0.36.0"},
		{"mousetrap", "v1.1.0", "pkg:golang/github.com/inconshreveable/mousetrap@v1.1.0"},
		{"pflag", "v1.0.9", "pkg:golang/github.com/spf13/pflag@v1.0.9"},
	}
	for _, d := range primaryDeps {
		runOK(
			"manifest", "add", "--primary",
			"--name", d.name,
			"--version", d.version,
			"--purl", d.purl,
		)
	}

	got := mustRead(t, filepath.Join(tmp, ".primary.json"))
	want := mustRead(t, filepath.Join(repoRoot, ".primary.json"))
	if string(got) != string(want) {
		t.Fatalf(".primary.json drift:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	got = mustRead(t, filepath.Join(tmp, ".components.json"))
	want = mustRead(t, filepath.Join(repoRoot, ".components.json"))
	if string(got) != string(want) {
		t.Fatalf(".components.json drift:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// findRepoRootFromCWD walks up from the test CWD until it hits a
// go.mod. Mirrors the helper in internal/regfetch's isolation test.
func findRepoRootFromCWD(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", dir)
		}
		dir = parent
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
