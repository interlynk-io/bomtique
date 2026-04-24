// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest/mutate"
)

type removeFlags struct {
	Dir     string
	Into    string
	Primary bool
	DryRun  bool
}

func newManifestRemoveCmd() *cobra.Command {
	f := &removeFlags{}
	cmd := &cobra.Command{
		Use:   "remove <ref>",
		Short: "Remove a component from a components manifest or scrub a ref from the primary's depends-on",
		Long: `remove takes a single reference — pkg:<type>/<name>[@<version>] or
<name>@<version> — and deletes the matching component from the reachable
components pool. Every depends-on edge that pointed at the removed entry
is scrubbed from every other pool component and from the primary's
depends-on list; each scrubbed edge is logged on stderr.

Use --primary to scrub only the primary's depends-on, leaving the pool
untouched.

Multi-file match (same ref in several components manifests) is a hard
error; disambiguate with --into <path>.

--dry-run reports the planned mutation without writing.

On success prints (to stdout):
  removed <name> from <path>                   pool delete
  removed depends-on entry from <primary>      primary scrub
  also scrubbed depends-on entry in <primary>  pool delete that also touched primary
  also scrubbed <ref> from <path> (<identity>) per-edge line for each scrubbed edge
'removed' reads 'would remove' under --dry-run.

Examples:
  bomtique manifest remove pkg:generic/acme/libx@1.0
  bomtique manifest remove libx@1.0                    # name@version form
  bomtique manifest remove --primary pkg:generic/x@1   # scrub primary only
  bomtique manifest remove --dry-run pkg:npm/foo@1     # preview, no write`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestRemove(cmd.OutOrStdout(), cmd.ErrOrStderr(), f, args[0])
		},
	}

	cmd.Flags().StringVarP(&f.Dir, "chdir", "C", "", "run in this directory (default: CWD)")
	cmd.Flags().StringVar(&f.Into, "into", "", "disambiguate multi-match by forcing a single components manifest path")
	cmd.Flags().BoolVar(&f.Primary, "primary", false, "scrub only the primary manifest's depends-on")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "report what would change without writing")

	return cmd
}

func runManifestRemove(stdout, stderr io.Writer, f *removeFlags, ref string) error {
	diag.SetSink(stderr)
	diag.Reset()
	defer func() { diag.SetSink(nil) }()

	res, err := mutate.Remove(mutate.RemoveOptions{
		FromDir: f.Dir,
		Into:    f.Into,
		Primary: f.Primary,
		DryRun:  f.DryRun,
		Ref:     ref,
	})
	if err != nil {
		return mapRemoveError(err)
	}

	verb := "removed"
	if res.DryRun {
		verb = "would remove"
	}

	switch {
	case res.PoolPath != "":
		_, _ = fmt.Fprintf(stdout, "%s %s from %s\n", verb, res.PoolComponentName, res.PoolPath)
	case res.PrimaryEdgeScrubbed:
		_, _ = fmt.Fprintf(stdout, "%s depends-on entry from %s\n", verb, res.PrimaryPath)
	}
	if res.PoolPath != "" && res.PrimaryEdgeScrubbed {
		_, _ = fmt.Fprintf(stdout, "also scrubbed depends-on entry in %s\n", res.PrimaryPath)
	}
	for _, e := range res.ScrubbedEdges {
		_, _ = fmt.Fprintf(stdout, "also scrubbed %s from %s (%s)\n", e.Ref, e.ManifestPath, e.FromIdentity)
	}
	return nil
}

func mapRemoveError(err error) error {
	var mm *mutate.ErrRemoveMultiMatch
	if errors.As(err, &mm) {
		return newExitErr(exitValidationError, err)
	}
	if errors.Is(err, mutate.ErrRemoveNotFound) ||
		errors.Is(err, mutate.ErrPrimaryNotFound) {
		return newExitErr(exitValidationError, err)
	}
	return newExitErr(exitValidationError, err)
}
