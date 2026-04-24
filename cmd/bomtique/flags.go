// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/spf13/cobra"

	"github.com/interlynk-io/bomtique/internal/safefs"
)

// commonFlags is the flag bundle genuinely shared between `generate`
// and `validate`. Both commands do filesystem reads (for hash targets,
// license texts, patch diffs) and both route warnings through the
// same diag channel.
//
// Anything that only matters for SBOM emission — tag filtering,
// deterministic timestamps, post-emit schema validation — lives on
// generateFlags instead. Help output stays tight.
type commonFlags struct {
	MaxFileSize      int64
	WarningsAsErrors bool
	FollowSymlinks   bool // accepted but not yet wired — safefs always refuses symlinks today
}

func (f *commonFlags) attach(cmd *cobra.Command) {
	cmd.Flags().Int64Var(&f.MaxFileSize, "max-file-size", safefs.DefaultMaxFileSize,
		"maximum bytes read from any single file referenced by a manifest (spec §8)")
	cmd.Flags().BoolVar(&f.WarningsAsErrors, "warnings-as-errors", false,
		"treat any warning emitted during the run as an error (exit code 4)")
	cmd.Flags().BoolVar(&f.FollowSymlinks, "follow-symlinks", false,
		"follow symlinks during filesystem reads (opt-in, outside spec §18.2)")
}

// emitFlags layers generate-only flags on top of commonFlags. These
// only make sense when an SBOM is actually being produced:
//
//   - --tag filters the pool before reachability.
//   - --source-date-epoch drives deterministic timestamps + serial.
//   - --output-validate checks the emitted document against its
//     vendored schema.
type emitFlags struct {
	commonFlags
	Tags               []string
	SourceDateEpochSet bool
	SourceDateEpoch    int64
	OutputValidate     bool
}

func (f *emitFlags) attachEmit(cmd *cobra.Command) {
	f.commonFlags.attach(cmd)
	cmd.Flags().StringSliceVar(&f.Tags, "tag", nil,
		"filter pool components to those whose tags include any listed value (§6.2)")
	cmd.Flags().Int64Var(&f.SourceDateEpoch, "source-date-epoch", 0,
		"override the SOURCE_DATE_EPOCH environment variable (seconds since Unix epoch)")
	cmd.Flags().BoolVar(&f.OutputValidate, "output-validate", false,
		"validate emitted JSON against the vendored CycloneDX 1.7 or SPDX 2.3 schema")
	cmd.PreRunE = chainPreRun(cmd.PreRunE, func(cmd *cobra.Command, _ []string) error {
		f.SourceDateEpochSet = cmd.Flags().Changed("source-date-epoch")
		return nil
	})
}

// chainPreRun appends next onto an existing PreRunE, preserving any
// earlier hook cobra already attached.
func chainPreRun(prev, next func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	if prev == nil {
		return next
	}
	return func(cmd *cobra.Command, args []string) error {
		if err := prev(cmd, args); err != nil {
			return err
		}
		return next(cmd, args)
	}
}
