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
		Long: `patch records a pedigree patch entry on the component matching <ref>.
<diff-path> is the relative path to the on-disk diff file — bomtique stores
the reference and does not read the file (scan reads it later).

--type is required and must be one of: unofficial, monkey, backport,
cherry-pick (spec §7.4).

--resolves is a repeatable flag taking comma-separated key=value pairs.
Recognised keys: type (security|defect|enhancement), name, id, url,
description. At least one of name= or id= must be supplied per entry.

  --resolves "type=security,name=CVE-2024-1,url=https://example.com"
  --resolves "name=BUG-42,type=defect"

--notes appends to pedigree.notes with a blank line separator; pair with
--replace-notes to overwrite existing notes.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestPatch(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, args[0], args[1])
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().StringVar(&f.Into, "into", "", "force the target components manifest path")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().StringVar(&f.Type, "type", "", "patch type: unofficial|monkey|backport|cherry-pick (required)")
	cmd.Flags().StringArrayVar(&f.Resolves, "resolves", nil, "resolves entry as key=val,key=val (repeatable)")
	cmd.Flags().StringVar(&f.Notes, "notes", "", "append to pedigree.notes")
	cmd.Flags().BoolVar(&f.ReplaceNotes, "replace-notes", false, "replace rather than append to pedigree.notes")

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
