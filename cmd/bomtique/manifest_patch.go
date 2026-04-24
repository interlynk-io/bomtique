// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest/mutate"
)

type patchFlags struct {
	Dir          string
	Into         string
	DryRun       bool
	Type         string
	Resolves     []string // each value is key=val[,key=val]*
	Notes        string
	ReplaceNotes bool
}

func newManifestPatchCmd() *cobra.Command {
	f := &patchFlags{}
	cmd := &cobra.Command{
		Use:   "patch <ref> <diff-path>",
		Short: "Register a pedigree patch on a component",
		Long: `patch records a pedigree.patches[] entry (spec §9.2) on the component
matching <ref> (pkg:<type>/<name>[@<version>] or <name>@<version>).

<diff-path> is stored as-is and interpreted relative to the components
manifest the entry lands in. bomtique does NOT read, copy, or hash the
diff here — scan reads it later via safefs. Absolute paths and '..'
traversal are rejected per spec §4.3.

--type is required: one of unofficial, monkey, backport, cherry-pick.

--resolves is a repeatable flag; each value is a comma-separated set of
key=value pairs. Recognised keys:
  type         security | defect | enhancement
  name         free-form identifier (e.g. CVE-2024-1, BUG-42)
  id           tracker id
  url          reference URL
  description  free-form prose
At least one of name= or id= MUST be supplied per entry.

--notes appends to pedigree.notes with a blank-line separator. Pair with
--replace-notes to overwrite existing notes instead.

Existing pedigree.patches[] entries on the target component are preserved;
this command only appends. To drop them, run 'manifest update <ref>
--clear-pedigree-patches' first.

Multi-file match is a hard error; disambiguate with --into <path>.

On success prints (to stdout):
  registered <type> patch on <component> (<ref> → <path>)
    N resolves entries                 when --resolves was supplied
    pedigree.notes {appended|replaced} when --notes fired
'registered' reads 'would register' under --dry-run.

Examples:
  # security backport with a CVE reference
  bomtique manifest patch pkg:generic/libx@1.0 ./patches/fix-cve.patch \
    --type backport \
    --resolves "type=security,name=CVE-2024-1,url=https://example/cve/2024-1" \
    --notes "Backported from upstream main @ abc1234"

  # cherry-pick referencing two issues
  bomtique manifest patch libx@1.0 ./patches/cherry.patch \
    --type cherry-pick \
    --resolves "type=defect,name=BUG-42" \
    --resolves "type=enhancement,name=FEAT-7"

  # replace rather than append to notes
  bomtique manifest patch pkg:generic/libx@1.0 ./patches/local.patch \
    --type unofficial --replace-notes --notes "Local-only fix; do not upstream"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestPatch(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, args[0], args[1])
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().StringVar(&f.Into, "into", "", "force the target components manifest path")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().StringVar(&f.Type, "type", "",
		"patch type: unofficial|monkey|backport|cherry-pick (required)")
	cmd.Flags().StringArrayVar(&f.Resolves, "resolves", nil,
		"resolves entry as key=val,...; keys: type (security|defect|enhancement), "+
			"name, id, url, description; one of name= or id= is required (repeatable)")
	cmd.Flags().StringVar(&f.Notes, "notes", "", "append to pedigree.notes (blank-line separator)")
	cmd.Flags().BoolVar(&f.ReplaceNotes, "replace-notes", false, "with --notes, replace pedigree.notes instead of appending")

	if err := cmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}

	return cmd
}

func runManifestPatch(stdout, stderr io.Writer, f *patchFlags, ref, diffPath string) error {
	resolves, err := parseResolvesFlags(f.Resolves)
	if err != nil {
		return newExitErr(exitUsageError, err)
	}

	diag.SetSink(stderr)
	diag.Reset()
	defer func() { diag.SetSink(nil) }()

	res, err := mutate.Patch(mutate.PatchOptions{
		FromDir:      f.Dir,
		Into:         f.Into,
		Ref:          ref,
		DryRun:       f.DryRun,
		DiffPath:     diffPath,
		Type:         f.Type,
		Resolves:     resolves,
		Notes:        f.Notes,
		ReplaceNotes: f.ReplaceNotes,
	})
	if err != nil {
		return mapPatchError(err)
	}

	verb := "registered"
	if res.DryRun {
		verb = "would register"
	}
	_, _ = fmt.Fprintf(stdout, "%s %s patch on %s (%s → %s)\n",
		verb, res.PatchType, res.ComponentName, res.Ref, res.Path)
	if res.ResolvesCount > 0 {
		_, _ = fmt.Fprintf(stdout, "  %d resolves entr%s\n", res.ResolvesCount, pluralY(res.ResolvesCount))
	}
	if res.NotesReplaced {
		_, _ = fmt.Fprintln(stdout, "  pedigree.notes replaced")
	} else if res.NotesAppended {
		_, _ = fmt.Fprintln(stdout, "  pedigree.notes appended")
	}
	return nil
}

// parseResolvesFlags parses each --resolves value ("key=val,key=val").
// Keys other than type|name|id|url|description are a usage error.
func parseResolvesFlags(raw []string) ([]mutate.ResolvesSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]mutate.ResolvesSpec, 0, len(raw))
	for _, entry := range raw {
		spec := mutate.ResolvesSpec{}
		for _, part := range strings.Split(entry, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			eq := strings.IndexByte(part, '=')
			if eq <= 0 || eq == len(part)-1 {
				return nil, fmt.Errorf("--resolves %q: expected key=value pairs", entry)
			}
			key := strings.TrimSpace(part[:eq])
			val := strings.TrimSpace(part[eq+1:])
			switch key {
			case "type":
				spec.Type = val
			case "name":
				spec.Name = val
			case "id":
				spec.ID = val
			case "url":
				spec.URL = val
			case "description":
				spec.Description = val
			default:
				return nil, fmt.Errorf("--resolves %q: unknown key %q (allowed: type, name, id, url, description)", entry, key)
			}
		}
		out = append(out, spec)
	}
	return out, nil
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func mapPatchError(err error) error {
	if errors.Is(err, mutate.ErrPatchInvalidType) ||
		errors.Is(err, mutate.ErrPatchInvalidResolvesType) {
		return newExitErr(exitValidationError, err)
	}
	var mm *mutate.ErrRemoveMultiMatch
	if errors.As(err, &mm) {
		return newExitErr(exitValidationError, err)
	}
	var ve *mutate.ErrInitValidation
	if errors.As(err, &ve) {
		return newExitErr(exitValidationError, err)
	}
	if errors.Is(err, mutate.ErrUpdateNotFound) ||
		errors.Is(err, mutate.ErrPrimaryNotFound) {
		return newExitErr(exitValidationError, err)
	}
	return newExitErr(exitValidationError, err)
}
