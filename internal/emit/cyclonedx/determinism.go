// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package cyclonedx

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/interlynk-io/bomtique/internal/jcs"
)

// sourceDateEpochEnv is the variable spec §15.3 cites (per
// [SOURCE-DATE-EPOCH]) for reproducible timestamps. An explicit
// Options.SourceDateEpoch overrides the env.
const sourceDateEpochEnv = "SOURCE_DATE_EPOCH"

// resolveSourceDateEpoch returns the resolved Unix-epoch seconds for
// this emission. Precedence is Options.SourceDateEpoch → env var.  A
// nil return means "no deterministic timestamp was requested"; the
// caller falls back to no timestamp (M7) or wall-clock (M9's CLI).
func resolveSourceDateEpoch(opts Options) (*int64, error) {
	if opts.SourceDateEpoch != nil {
		if *opts.SourceDateEpoch < 0 {
			return nil, fmt.Errorf("cyclonedx: SourceDateEpoch must be non-negative, got %d", *opts.SourceDateEpoch)
		}
		v := *opts.SourceDateEpoch
		return &v, nil
	}
	raw := os.Getenv(sourceDateEpochEnv)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("cyclonedx: %s %q: %w", sourceDateEpochEnv, raw, err)
	}
	if n < 0 {
		return nil, fmt.Errorf("cyclonedx: %s must be non-negative, got %d", sourceDateEpochEnv, n)
	}
	return &n, nil
}

// formatTimestamp renders epoch seconds as an ISO 8601 UTC string with
// second precision, matching §15.3's requirement for
// `metadata.timestamp`.
func formatTimestamp(epoch int64) string {
	return time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05Z")
}

// computeDeterministicSerial produces the §15.3 `urn:uuid:<UUIDv5>`
// form by:
//
//  1. json.Marshal'ing the already-sorted components array.
//  2. Canonicalizing via RFC 8785 (internal/jcs.Canonicalize).
//  3. SHA-256 of the canonical bytes, hex-encoded (lowercase).
//  4. UUIDv5 over (DNS namespace, prefix+hex) — emitted as urn:uuid:<u>.
//
// Errors come only from json.Marshal / jcs.Canonicalize, neither of
// which should fail on a well-formed components slice.
func computeDeterministicSerial(components []cdxComponent) (string, error) {
	if components == nil {
		components = []cdxComponent{}
	}
	raw, err := json.Marshal(components)
	if err != nil {
		return "", fmt.Errorf("cyclonedx: marshal components for serial: %w", err)
	}
	canonical, err := jcs.Canonicalize(raw)
	if err != nil {
		return "", fmt.Errorf("cyclonedx: canonicalise components for serial: %w", err)
	}
	return deterministicSerial(canonical), nil
}
