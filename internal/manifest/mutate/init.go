// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
)

// primaryFilename is the conventional on-disk name for a primary
// manifest. Kept in step with LocatePrimary's expectations.
const primaryFilename = ".primary.json"

// InitOptions configures a single `bomtique manifest init` run. All
// fields except Name are optional.
type InitOptions struct {
	// Dir is the target directory; the primary manifest is written at
	// filepath.Join(Dir, ".primary.json"). Empty means CWD.
	Dir string

	// Force, when true, overwrites an existing .primary.json. Unknown
	// top-level and primary-component fields from the existing file
	// are carried over to the new manifest.
	Force bool

	// Primary Component fields. Name is required.
	Name        string
	Version     string
	Type        string // defaults to "application" when empty
	Description string
	License     string // SPDX expression (string shorthand)
	Purl        string
	CPE         string

	Supplier      string
	SupplierEmail string
	SupplierURL   string

	// External reference shorthands. Each populates a single entry in
	// the component's external_references array when non-empty.
	Website      string
	VCS          string
	Distribution string
	IssueTracker string
}

// InitResult reports the outcome of a successful Init call.
type InitResult struct {
	// Path is the absolute path of the written .primary.json.
	Path string
	// Manifest is the parsed manifest as written to disk.
	Manifest *manifest.Manifest
	// Overwrote is true when Init replaced an existing file via Force.
	Overwrote bool
}

// ErrPrimaryExists is returned when a .primary.json already exists at
// the target path and Force was not set.
var ErrPrimaryExists = errors.New("primary manifest already exists at target path (use --force to overwrite)")

// ErrInitValidation wraps the validator errors that kept a primary
// manifest from being written. Callers can inspect .Errors via
// errors.As to report per-field messages.
type ErrInitValidation struct {
	Errors []validate.Error
}

func (e *ErrInitValidation) Error() string {
	if len(e.Errors) == 0 {
		return "init validation failed"
	}
	first := e.Errors[0].Error()
	if len(e.Errors) == 1 {
		return "init validation failed: " + first
	}
	return fmt.Sprintf("init validation failed (%d errors; first: %s)", len(e.Errors), first)
}

// Init scaffolds a primary manifest from opts. It validates the
// constructed manifest in §6.1 / §6.2 / §6.3 terms before writing,
// preserves unknown top-level and primary-component fields on --force
// re-init, and writes canonical JSON via WriteJSON.
//
// Does not create .components.json — §5.2 forbids an empty
// components[] array, and the first `manifest add` call creates the
// file on demand.
func Init(opts InitOptions) (*InitResult, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	if info, err := os.Stat(absDir); err != nil {
		return nil, fmt.Errorf("stat %s: %w", absDir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", absDir)
	}

	target := filepath.Join(absDir, primaryFilename)
	overwrote := false
	var carriedPrimaryUnknown map[string]json.RawMessage
	var carriedManifestUnknown map[string]json.RawMessage

	if existing, err := os.ReadFile(target); err == nil {
		if !opts.Force {
			return nil, fmt.Errorf("%s: %w", target, ErrPrimaryExists)
		}
		overwrote = true
		carriedManifestUnknown, carriedPrimaryUnknown = liftUnknown(existing, target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", target, err)
	}

	pm := buildPrimary(opts, carriedManifestUnknown, carriedPrimaryUnknown)
	m := &manifest.Manifest{
		Path:    target,
		Kind:    manifest.KindPrimary,
		Format:  manifest.FormatJSON,
		Primary: pm,
	}

	if errs := validate.Manifest(m, validate.Options{SkipFilesystem: true}); len(errs) > 0 {
		return nil, &ErrInitValidation{Errors: errs}
	}

	if err := writeAtomicJSON(target, m); err != nil {
		return nil, err
	}
	return &InitResult{Path: target, Manifest: m, Overwrote: overwrote}, nil
}

// buildPrimary assembles the PrimaryManifest from opts, carrying over
// any unknowns captured from an existing file on --force.
func buildPrimary(opts InitOptions, manifestUnknown, primaryUnknown map[string]json.RawMessage) *manifest.PrimaryManifest {
	c := manifest.Component{Name: strings.TrimSpace(opts.Name)}

	if v := strings.TrimSpace(opts.Version); v != "" {
		c.Version = &v
	}
	t := strings.TrimSpace(opts.Type)
	if t == "" {
		t = "application"
	}
	c.Type = &t
	if v := strings.TrimSpace(opts.Description); v != "" {
		c.Description = &v
	}
	if v := strings.TrimSpace(opts.License); v != "" {
		c.License = &manifest.License{Expression: v}
	}
	if v := strings.TrimSpace(opts.Purl); v != "" {
		c.Purl = &v
	}
	if v := strings.TrimSpace(opts.CPE); v != "" {
		c.CPE = &v
	}

	if sn := strings.TrimSpace(opts.Supplier); sn != "" || opts.SupplierEmail != "" || opts.SupplierURL != "" {
		s := &manifest.Supplier{Name: sn}
		if e := strings.TrimSpace(opts.SupplierEmail); e != "" {
			s.Email = &e
		}
		if u := strings.TrimSpace(opts.SupplierURL); u != "" {
			s.URL = &u
		}
		c.Supplier = s
	}

	c.ExternalReferences = appendExternalRef(c.ExternalReferences, "website", opts.Website)
	c.ExternalReferences = appendExternalRef(c.ExternalReferences, "vcs", opts.VCS)
	c.ExternalReferences = appendExternalRef(c.ExternalReferences, "distribution", opts.Distribution)
	c.ExternalReferences = appendExternalRef(c.ExternalReferences, "issue-tracker", opts.IssueTracker)

	if len(primaryUnknown) > 0 {
		c.Unknown = primaryUnknown
	}

	pm := &manifest.PrimaryManifest{
		Schema:  manifest.SchemaPrimaryV1,
		Primary: c,
	}
	if len(manifestUnknown) > 0 {
		pm.Unknown = manifestUnknown
	}
	return pm
}

func appendExternalRef(refs []manifest.ExternalRef, refType, url string) []manifest.ExternalRef {
	u := strings.TrimSpace(url)
	if u == "" {
		return refs
	}
	return append(refs, manifest.ExternalRef{Type: refType, URL: u})
}

// liftUnknown parses the existing primary manifest and returns its
// top-level PrimaryManifest.Unknown and the primary-component Unknown
// maps. A parse failure is swallowed because --force is a blunt
// overwrite and preserving unknowns is best-effort.
func liftUnknown(data []byte, path string) (manifestUnknown, primaryUnknown map[string]json.RawMessage) {
	m, err := manifest.ParseJSON(data, path)
	if err != nil || m == nil || m.Primary == nil {
		return nil, nil
	}
	return m.Primary.Unknown, m.Primary.Primary.Unknown
}

// writeAtomicJSON writes the manifest to a tmp file in the target
// directory and renames it into place. Keeps crash-time partial
// writes off the source tree.
func writeAtomicJSON(target string, m *manifest.Manifest) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".primary.json.tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if err := WriteJSON(tmp, m); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, target, err)
	}
	return nil
}
