// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/interlynk-io/bomtique/internal/jcs"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

const toolCreator = "Tool: bomtique-0.1.0"

// ReachableComponent mirrors cyclonedx.ReachableComponent — a pool
// component paired with the manifest directory that sourced it for
// path-bearing fields (license.texts[].file, etc.).
type ReachableComponent struct {
	Component   *manifest.Component
	ManifestDir string
}

// EmitInput is the full input to one SPDX emission.
type EmitInput struct {
	Primary    *manifest.Component
	PrimaryDir string
	Reachable  []ReachableComponent
}

// Options controls emitter behaviour. Field names mirror the CycloneDX
// emitter's Options so callers can share Options values across formats.
type Options struct {
	MaxFileSize     int64
	Indent          bool
	SourceDateEpoch *int64
}

// Emit produces SPDX 2.3 JSON bytes for one primary. Determinism-wise
// the emitter matches the CycloneDX side: struct-per-field ordering,
// deterministic documentNamespace when SOURCE_DATE_EPOCH is set, and
// stable package / relationship ordering.
func Emit(in EmitInput, opts Options) ([]byte, error) {
	if in.Primary == nil {
		return nil, fmt.Errorf("spdx: nil primary")
	}
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = safefs.DefaultMaxFileSize
	}

	epoch, err := resolveSourceDateEpoch(opts)
	if err != nil {
		return nil, err
	}

	drops := newDroppedCounter()

	spdxIDs := newIDAssigner()
	primaryID := spdxIDs.assign(in.Primary.Name)
	primaryPkg, err := buildPackage(in.Primary, in.PrimaryDir, primaryID, drops, opts)
	if err != nil {
		return nil, err
	}

	packages := make([]spdxPackage, 0, 1+len(in.Reachable))
	packages = append(packages, primaryPkg)

	// Assign SPDXIDs for every reachable first so DEPENDS_ON lookup
	// works regardless of processing order.
	poolIDs := make([]string, len(in.Reachable))
	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		poolIDs[i] = spdxIDs.assign(rc.Component.Name)
	}
	for i, rc := range in.Reachable {
		if rc.Component == nil {
			continue
		}
		pkg, err := buildPackage(rc.Component, rc.ManifestDir, poolIDs[i], drops, opts)
		if err != nil {
			return nil, err
		}
		packages = append(packages, pkg)
	}

	if len(in.Primary.Lifecycles) > 0 {
		drops.lifecycles()
	}

	relations := buildRelationships(in, primaryID, poolIDs)

	doc := &spdxDocument{
		SPDXVersion:       spdxVersion,
		DataLicense:       dataLicenseCC0,
		SPDXID:            documentSPDXID,
		Name:              in.Primary.Name,
		DocumentNamespace: documentNamespace(in, epoch),
		CreationInfo: &spdxCreation{
			Created:  timestamp(epoch),
			Creators: []string{toolCreator},
		},
		Packages:      packages,
		Relationships: relations,
	}

	drops.emitWarnings()

	if opts.Indent {
		return json.MarshalIndent(doc, "", "  ")
	}
	return json.Marshal(doc)
}

// timestamp returns the ISO 8601 UTC-second form for the provided
// epoch, or the current wall-clock UTC timestamp when epoch is nil.
// Spec §15.3 wording applies equally to SPDX via §14.2's projection.
func timestamp(epoch *int64) string {
	if epoch != nil {
		return time.Unix(*epoch, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// documentNamespace synthesises a stable SPDX DocumentNamespace URI.
// When SOURCE_DATE_EPOCH is set we SHA-256 the JCS-canonicalised
// primary identifiers and feed the hex into a UUIDv5 (DNS namespace)
// so rerunning against the same inputs produces the same namespace.
// When SDE is unset we fall back to a UUIDv4 — non-deterministic, but
// honest about it.
func documentNamespace(in EmitInput, epoch *int64) string {
	base := "https://interlynk.io/bomtique/spdx/"
	if epoch == nil {
		return base + uuid.NewString()
	}
	// Seed: primary name + version + every reachable's name + version.
	seed := map[string]any{
		"primary": primaryKey(in.Primary),
		"pool":    reachableKeys(in.Reachable),
	}
	raw, _ := json.Marshal(seed)
	canonical, err := jcs.Canonicalize(raw)
	if err != nil {
		// Failsafe: reachable data is all Go-originated strings, so
		// canonicalisation should never fail; fall back to uuid.
		return base + uuid.NewString()
	}
	sum := sha256.Sum256(canonical)
	name := "component-manifest/v1/document-namespace/" + hex.EncodeToString(sum[:])
	ns := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(name))
	return base + ns.String()
}

func primaryKey(c *manifest.Component) string {
	name := c.Name
	if c.Version != nil {
		name += "@" + *c.Version
	}
	return name
}

func reachableKeys(rcs []ReachableComponent) []string {
	out := make([]string, 0, len(rcs))
	for _, rc := range rcs {
		if rc.Component == nil {
			continue
		}
		out = append(out, primaryKey(rc.Component))
	}
	return out
}

const sourceDateEpochEnv = "SOURCE_DATE_EPOCH"

func resolveSourceDateEpoch(opts Options) (*int64, error) {
	if opts.SourceDateEpoch != nil {
		if *opts.SourceDateEpoch < 0 {
			return nil, fmt.Errorf("spdx: SourceDateEpoch must be non-negative, got %d", *opts.SourceDateEpoch)
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
		return nil, fmt.Errorf("spdx: %s %q: %w", sourceDateEpochEnv, raw, err)
	}
	if n < 0 {
		return nil, fmt.Errorf("spdx: %s must be non-negative, got %d", sourceDateEpochEnv, n)
	}
	return &n, nil
}
