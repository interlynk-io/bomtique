// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package mutate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/interlynk-io/bomtique/internal/diag"
	"github.com/interlynk-io/bomtique/internal/manifest"
	"github.com/interlynk-io/bomtique/internal/manifest/validate"
	"github.com/interlynk-io/bomtique/internal/pool"
	"github.com/interlynk-io/bomtique/internal/purl"
	"github.com/interlynk-io/bomtique/internal/safefs"
)

// ExternalRefSpec captures a single `--external <type>=<url>` flag
// value. The full shorthand flags (--website, --vcs, --distribution,
// --issue-tracker) collapse into this shape before reaching Add.
type ExternalRefSpec struct {
	Type string
	URL  string
}

// AddOptions configures one `bomtique manifest add` invocation.
type AddOptions struct {
	// FromDir is the starting directory for target auto-discovery.
	// Empty defaults to CWD. Relative --into paths resolve against it.
	FromDir string

	// Into is the explicit target components manifest path. Empty
	// triggers LocateOrCreateComponents.
	Into string

	// Primary, when true, redirects the add to the primary manifest's
	// depends-on list instead of the components pool. The synthesised
	// ref is `purl` (preferred, canonicalised) or `name@version`.
	Primary bool

	// FromPath is an optional path to a JSON file containing a bare
	// Component object. "-" reads from FromReader (stdin in the CLI).
	FromPath   string
	FromReader io.Reader

	// Component field overrides. Empty strings mean "unset"; the
	// caller passes flag values verbatim.
	Name          string
	Version       string
	Type          string
	Description   string
	License       string
	Purl          string
	CPE           string
	Scope         string
	Supplier      string
	SupplierEmail string
	SupplierURL   string
	Website       string
	VCS           string
	Distribution  string
	IssueTracker  string

	ExternalRefs []ExternalRefSpec
	DependsOn    []string
	Tags         []string

	// Vendored-component shortcut (M14.6). When VendoredAt is set,
	// Add synthesises three extra fields on the emitted Component:
	//   - a repo-local purl derived from the primary's purl
	//     (unless Purl was set explicitly);
	//   - a single directory-form hash directive (digest computed
	//     at scan time per §15.4, not here);
	//   - pedigree.ancestors[0] built from the Upstream* fields.
	VendoredAt       string
	Extensions       []string
	UpstreamName     string
	UpstreamVersion  string
	UpstreamPurl     string
	UpstreamSupplier string
	UpstreamWebsite  string
	UpstreamVCS      string
}

// AddResult reports the outcome of an Add call.
type AddResult struct {
	// Path is the absolute path of the target file that was written.
	Path string
	// Created is true when Add created the target file in this call.
	Created bool
	// ToPrimary is true when the add was directed at a primary's
	// depends-on rather than the pool.
	ToPrimary bool
	// Ref is the reference string appended to the primary's
	// depends-on, when ToPrimary is true.
	Ref string
	// AlreadyPresent is true when ToPrimary and the ref was already in
	// the primary's depends-on (no change written).
	AlreadyPresent bool
	// Component is the pool component written, when ToPrimary is false.
	Component *manifest.Component
	// Overrides lists every field the flag layer replaced on the
	// --from base.
	Overrides []FieldOverride
}

// ErrIdentityCollision is returned when the component being added
// shares an identity (§11) with an existing pool entry.
type ErrIdentityCollision struct {
	Existing string // identity string of the colliding entry
	Incoming string // identity string of the new component
	Path     string // target manifest path
}

func (e *ErrIdentityCollision) Error() string {
	return fmt.Sprintf("%s: identity collision: %s already present (incoming: %s)", e.Path, e.Existing, e.Incoming)
}

// ErrInvalidRef is returned when --primary is set but the constructed
// component has neither a purl nor both a name and a version, so no
// §10.2 reference can be derived.
var ErrInvalidRef = errors.New("cannot derive depends-on ref: need --purl, or both --name and --version")

// Add is the entry point for `bomtique manifest add`. It routes to the
// pool-add or primary-depends-on path based on opts.Primary.
func Add(opts AddOptions) (*AddResult, error) {
	base, err := readFromSource(opts)
	if err != nil {
		return nil, err
	}
	flagLayer := buildFlagComponent(opts)
	merged, overrides := MergeComponent(base, flagLayer)
	if merged == nil {
		return nil, fmt.Errorf("manifest add: no component data supplied (use --name and --version or --from)")
	}
	for _, fo := range overrides {
		diag.Warn("flag --%s overrode field %s from --from input", flagNameFor(fo.Field), fo.Field)
	}

	if opts.Primary {
		if opts.Scope != "" {
			diag.Warn("--scope has no meaning on a primary's depends-on ref (§5.3); ignoring")
		}
		if opts.VendoredAt != "" {
			return nil, errors.New("--vendored-at cannot be combined with --primary")
		}
		return addToPrimaryDeps(opts, merged)
	}
	return addToPool(opts, merged)
}

// addToPool handles the components-manifest path. It resolves the
// target, parses or seeds the components manifest, checks identity
// against existing entries (§11), appends the new component, and
// writes canonical output.
func addToPool(opts AddOptions, c *manifest.Component) (*AddResult, error) {
	fromDir := opts.FromDir
	if fromDir == "" {
		fromDir = "."
	}
	targetPath, created, err := LocateOrCreateComponents(fromDir, opts.Into)
	if err != nil {
		return nil, err
	}

	if opts.VendoredAt != "" {
		if err := applyVendoredAt(c, opts, targetPath, fromDir); err != nil {
			return nil, err
		}
	}

	isCSV := strings.EqualFold(filepath.Ext(targetPath), ".csv")
	if isCSV {
		if err := CheckFitsCSV(c); err != nil {
			return nil, fmt.Errorf("%s: %w; rerun with --into <json-path>", targetPath, err)
		}
	}

	var cm *manifest.ComponentsManifest
	if created {
		cm = &manifest.ComponentsManifest{
			Schema:     manifest.SchemaComponentsV1,
			Components: nil,
		}
	} else {
		existing, err := parseComponentsFile(targetPath)
		if err != nil {
			return nil, err
		}
		cm = existing
	}

	if err := checkPoolCollision(cm.Components, c, targetPath); err != nil {
		return nil, err
	}

	cm.Components = append(cm.Components, *c)

	// Validate the resulting manifest shape. Filesystem checks are off:
	// hashes and license texts may reference paths that don't exist in
	// the CWD but will exist at scan time.
	m := &manifest.Manifest{
		Path:       targetPath,
		Kind:       manifest.KindComponents,
		Format:     formatFor(targetPath),
		Components: cm,
	}
	if errs := validate.Manifest(m, validate.Options{SkipFilesystem: true}); len(errs) > 0 {
		return nil, &ErrInitValidation{Errors: errs}
	}

	if err := writeManifest(targetPath, m); err != nil {
		return nil, err
	}

	return &AddResult{
		Path:      targetPath,
		Created:   created,
		Component: c,
		Overrides: nil,
	}, nil
}

// addToPrimaryDeps appends a §10.2 reference to the primary's
// depends-on array. Dedup: if the ref is already present, Add returns
// AlreadyPresent=true and does not write.
func addToPrimaryDeps(opts AddOptions, c *manifest.Component) (*AddResult, error) {
	fromDir := opts.FromDir
	if fromDir == "" {
		fromDir = "."
	}
	primaryPath, err := LocatePrimary(fromDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(primaryPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", primaryPath, err)
	}
	m, err := manifest.ParseJSON(data, primaryPath)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", primaryPath, err)
	}
	if m.Kind != manifest.KindPrimary || m.Primary == nil {
		return nil, fmt.Errorf("%s is not a primary manifest", primaryPath)
	}

	ref, err := derivePrimaryRef(c)
	if err != nil {
		return nil, err
	}

	for _, existing := range m.Primary.Primary.DependsOn {
		if existing == ref {
			return &AddResult{
				Path:           primaryPath,
				ToPrimary:      true,
				Ref:            ref,
				AlreadyPresent: true,
			}, nil
		}
	}
	m.Primary.Primary.DependsOn = append(m.Primary.Primary.DependsOn, ref)

	// Don't revalidate the whole primary here — the user may have
	// existing depends-on refs that only resolve against a components
	// pool we're not reading. Refs are validated by scan/validate at
	// run time.

	if err := writeManifest(primaryPath, m); err != nil {
		return nil, err
	}
	return &AddResult{
		Path:      primaryPath,
		ToPrimary: true,
		Ref:       ref,
	}, nil
}

// derivePrimaryRef picks the §10.2 reference form for the component
// being added: canonical purl if present, else name@version. The
// helper canonicalises the purl via pool.Identify so we store the
// same byte form the consumer compares.
func derivePrimaryRef(c *manifest.Component) (string, error) {
	id, err := pool.Identify(c)
	if err != nil {
		return "", err
	}
	switch id.Kind {
	case pool.KindPurl:
		return id.Purl, nil
	case pool.KindNameVersion:
		return id.Name + "@" + id.Version, nil
	default:
		return "", ErrInvalidRef
	}
}

// checkPoolCollision compares the incoming component's identity
// against every existing pool entry. The first collision wins.
func checkPoolCollision(existing []manifest.Component, incoming *manifest.Component, path string) error {
	idIn, err := pool.Identify(incoming)
	if err != nil {
		return fmt.Errorf("identify incoming: %w", err)
	}
	keyIn := idIn.Key()
	for i := range existing {
		idEx, err := pool.Identify(&existing[i])
		if err != nil {
			// An existing entry that can't be identified is the
			// validator's problem — don't block the add over it.
			continue
		}
		if idEx.Key() == keyIn {
			return &ErrIdentityCollision{
				Existing: idEx.String(),
				Incoming: idIn.String(),
				Path:     path,
			}
		}
	}
	return nil
}

// readFromSource reads the optional --from input and returns the
// parsed base Component, or nil when no source was supplied.
func readFromSource(opts AddOptions) (*manifest.Component, error) {
	path := strings.TrimSpace(opts.FromPath)
	if path == "" {
		return nil, nil
	}
	var raw []byte
	var err error
	switch path {
	case "-":
		if opts.FromReader == nil {
			return nil, fmt.Errorf("--from - requires a reader (no stdin provided)")
		}
		raw, err = io.ReadAll(opts.FromReader)
	default:
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read --from %s: %w", path, err)
	}
	var c manifest.Component
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse --from %s: %w", path, err)
	}
	return &c, nil
}

// buildFlagComponent assembles a Component from AddOptions flag
// values. Unset fields stay nil so MergeComponent can tell them apart
// from explicit overrides.
func buildFlagComponent(opts AddOptions) *manifest.Component {
	c := &manifest.Component{}
	hasContent := false

	if v := strings.TrimSpace(opts.Name); v != "" {
		c.Name = v
		hasContent = true
	}
	if v := strings.TrimSpace(opts.Version); v != "" {
		c.Version = &v
		hasContent = true
	}
	if v := strings.TrimSpace(opts.Type); v != "" {
		c.Type = &v
		hasContent = true
	}
	if v := strings.TrimSpace(opts.Description); v != "" {
		c.Description = &v
		hasContent = true
	}
	if v := strings.TrimSpace(opts.License); v != "" {
		c.License = &manifest.License{Expression: v}
		hasContent = true
	}
	if v := strings.TrimSpace(opts.Purl); v != "" {
		c.Purl = &v
		hasContent = true
	}
	if v := strings.TrimSpace(opts.CPE); v != "" {
		c.CPE = &v
		hasContent = true
	}
	if v := strings.TrimSpace(opts.Scope); v != "" {
		c.Scope = &v
		hasContent = true
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
		hasContent = true
	}

	var refs []manifest.ExternalRef
	refs = appendExternalRef(refs, "website", opts.Website)
	refs = appendExternalRef(refs, "vcs", opts.VCS)
	refs = appendExternalRef(refs, "distribution", opts.Distribution)
	refs = appendExternalRef(refs, "issue-tracker", opts.IssueTracker)
	for _, spec := range opts.ExternalRefs {
		refs = appendExternalRef(refs, spec.Type, spec.URL)
	}
	if len(refs) > 0 {
		c.ExternalReferences = refs
		hasContent = true
	}

	if len(opts.DependsOn) > 0 {
		c.DependsOn = append([]string(nil), opts.DependsOn...)
		hasContent = true
	}
	if len(opts.Tags) > 0 {
		c.Tags = append([]string(nil), opts.Tags...)
		hasContent = true
	}

	if !hasContent {
		return nil
	}
	return c
}

// parseComponentsFile reads a JSON or CSV components manifest file.
// Returns the ComponentsManifest ready for mutation.
func parseComponentsFile(path string) (*manifest.ComponentsManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m *manifest.Manifest
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		m, err = manifest.ParseCSV(data, path)
	} else {
		m, err = manifest.ParseJSON(data, path)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Kind != manifest.KindComponents || m.Components == nil {
		return nil, fmt.Errorf("%s is not a components manifest", path)
	}
	return m.Components, nil
}

// writeManifest dispatches to the JSON or CSV writer based on the
// target path's extension, writing atomically through a tmp file.
func writeManifest(target string, m *manifest.Manifest) error {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp for %s: %w", target, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	switch formatFor(target) {
	case manifest.FormatCSV:
		err = WriteCSV(tmp, m)
	default:
		err = WriteJSON(tmp, m)
	}
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod tmp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, target, err)
	}
	return nil
}

func formatFor(path string) manifest.Format {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		return manifest.FormatCSV
	}
	return manifest.FormatJSON
}

// repoHostPurlTypes are the purl types §9.3 pattern 1 supports for
// repo-local derivation. Other types (npm, pypi, etc.) don't map
// onto "a path inside the same repo" cleanly.
var repoHostPurlTypes = map[string]struct{}{
	"github":    {},
	"gitlab":    {},
	"bitbucket": {},
}

// applyVendoredAt synthesises the three fields §9.3 pattern 1 wants
// on a vendored component: a repo-local purl, a directory-form hash
// directive, and a pedigree.ancestors[0] entry. The input Component
// is mutated in place. Filesystem checks (dir exists) and path
// legality (§4.3) are enforced here; digest computation happens at
// scan time per §15.4.
func applyVendoredAt(c *manifest.Component, opts AddOptions, targetPath, fromDir string) error {
	manifestDir := filepath.Dir(targetPath)

	// §4.3: the directory path must resolve under the components
	// manifest's directory. This rejects absolute paths, UNC, Windows
	// drive-letter, and any `..` traversal.
	resolved, err := safefs.ResolveRelative(manifestDir, opts.VendoredAt)
	if err != nil {
		return fmt.Errorf("--vendored-at %q: %w", opts.VendoredAt, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("--vendored-at %q: directory does not exist at %s", opts.VendoredAt, resolved)
		}
		return fmt.Errorf("--vendored-at %q: stat: %w", opts.VendoredAt, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--vendored-at %q: %s is not a directory", opts.VendoredAt, resolved)
	}

	// Build the upstream ancestor first so we can compare its purl to
	// any explicit --purl the user supplied (§9.3 forbids equality).
	ancestor, err := buildUpstreamAncestor(opts)
	if err != nil {
		return err
	}
	upstreamPurl := ""
	if ancestor != nil && ancestor.Purl != nil {
		if p, err := purl.Parse(*ancestor.Purl); err == nil {
			upstreamPurl = p.String()
		}
	}

	// Derive the repo-local purl if the user didn't supply one. If
	// both were supplied, §9.3 forbids them from being equal.
	if strings.TrimSpace(opts.Purl) == "" && (c.Purl == nil || strings.TrimSpace(*c.Purl) == "") {
		derived, err := deriveRepoLocalPurl(fromDir, opts.VendoredAt, c)
		if err != nil {
			return err
		}
		c.Purl = &derived
	}

	if c.Purl != nil && upstreamPurl != "" {
		if canon, err := purl.Parse(*c.Purl); err == nil && canon.String() == upstreamPurl {
			return fmt.Errorf("component purl equals upstream purl (%q): §9.3 forbids sharing the upstream identity on a vendored component", upstreamPurl)
		}
	}

	// Directory-form hash directive. We do NOT compute the digest
	// here — §15.4 says hashes are computed at scan time over the
	// on-disk bytes; computing now would stale the moment the user
	// patches the vendored source.
	dirPath := normaliseVendoredPath(opts.VendoredAt)
	h := manifest.Hash{
		Algorithm: "SHA-256",
		Path:      strPtrNew(dirPath),
	}
	if len(opts.Extensions) > 0 {
		h.Extensions = cleanExtensions(opts.Extensions)
	}
	// Only install the hash directive when the caller hasn't supplied
	// hashes already (via --from). Otherwise preserve the explicit
	// input.
	if len(c.Hashes) == 0 {
		c.Hashes = []manifest.Hash{h}
	}

	if ancestor != nil {
		if c.Pedigree == nil {
			c.Pedigree = &manifest.Pedigree{}
		}
		// Don't overwrite pre-existing ancestors; prepend ours so the
		// synthesis appears first per §9.1 producer convention.
		c.Pedigree.Ancestors = append([]manifest.Ancestor{*ancestor}, c.Pedigree.Ancestors...)
	}
	return nil
}

// buildUpstreamAncestor constructs the §9.1 Ancestor entry from the
// --upstream-* flags. Returns nil when no upstream metadata was
// supplied (the caller then emits no ancestor).
func buildUpstreamAncestor(opts AddOptions) (*manifest.Component, error) {
	name := strings.TrimSpace(opts.UpstreamName)
	version := strings.TrimSpace(opts.UpstreamVersion)
	upstreamPurl := strings.TrimSpace(opts.UpstreamPurl)
	supplier := strings.TrimSpace(opts.UpstreamSupplier)
	website := strings.TrimSpace(opts.UpstreamWebsite)
	vcs := strings.TrimSpace(opts.UpstreamVCS)

	if name == "" && version == "" && upstreamPurl == "" && supplier == "" && website == "" && vcs == "" {
		return nil, nil
	}
	// §9.1: ancestor follows Component identity rules — name required,
	// plus one of version/purl/hashes.
	if name == "" {
		return nil, errors.New("--upstream-name is required when --vendored-at is used with any --upstream-* flag")
	}
	if version == "" && upstreamPurl == "" {
		return nil, errors.New("--upstream-version or --upstream-purl is required to identify the upstream ancestor (§9.1)")
	}

	a := &manifest.Component{Name: name}
	if version != "" {
		a.Version = &version
	}
	if upstreamPurl != "" {
		a.Purl = &upstreamPurl
	}
	if supplier != "" {
		a.Supplier = &manifest.Supplier{Name: supplier}
	}
	if website != "" {
		a.ExternalReferences = append(a.ExternalReferences,
			manifest.ExternalRef{Type: "website", URL: website})
	}
	if vcs != "" {
		a.ExternalReferences = append(a.ExternalReferences,
			manifest.ExternalRef{Type: "vcs", URL: vcs})
	}
	return a, nil
}

// deriveRepoLocalPurl builds §9.3 pattern 1 form
// "pkg:<type>/<primary-namespace>/<primary-name>/<vendored-path>@<version>"
// from the nearest primary's purl. Returns a clear error when the
// primary isn't reachable, doesn't carry a purl, or carries a purl
// whose type isn't a repo-host ecosystem.
func deriveRepoLocalPurl(fromDir, vendoredPath string, c *manifest.Component) (string, error) {
	primaryPath, err := LocatePrimary(fromDir)
	if err != nil {
		return "", fmt.Errorf("repo-local purl derivation: %w", err)
	}
	data, err := os.ReadFile(primaryPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", primaryPath, err)
	}
	m, err := manifest.ParseJSON(data, primaryPath)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", primaryPath, err)
	}
	if m.Kind != manifest.KindPrimary || m.Primary == nil {
		return "", fmt.Errorf("%s is not a primary manifest", primaryPath)
	}
	primary := m.Primary.Primary
	if primary.Purl == nil || strings.TrimSpace(*primary.Purl) == "" {
		return "", fmt.Errorf("primary manifest has no purl; pass --purl explicitly to avoid deriving a repo-local purl")
	}
	pp, err := purl.Parse(*primary.Purl)
	if err != nil {
		return "", fmt.Errorf("primary purl %q failed to parse: %w", *primary.Purl, err)
	}
	if _, ok := repoHostPurlTypes[pp.Type]; !ok {
		return "", fmt.Errorf("cannot derive repo-local purl from primary type %q (supported: github, gitlab, bitbucket); pass --purl explicitly", pp.Type)
	}

	pathSegments := vendoredPathSegments(vendoredPath)
	if len(pathSegments) == 0 {
		return "", errors.New("--vendored-at resolved to an empty path")
	}

	// Build the path list: primary namespace (may be empty), primary
	// name, plus every segment of the vendored path.
	segments := []string{}
	if pp.Namespace != "" {
		segments = append(segments, strings.Split(pp.Namespace, "/")...)
	}
	segments = append(segments, pp.Name)
	segments = append(segments, pathSegments...)
	if len(segments) < 2 {
		return "", fmt.Errorf("cannot derive repo-local purl: not enough path segments (primary=%q, vendored=%q)", *primary.Purl, vendoredPath)
	}
	newName := segments[len(segments)-1]
	newNamespace := strings.Join(segments[:len(segments)-1], "/")

	// Pick the version: component's own version when set; else the
	// primary's version (for a vendored-at-repo-root corner case).
	version := ""
	if c.Version != nil && strings.TrimSpace(*c.Version) != "" {
		version = *c.Version
	} else if primary.Version != nil {
		version = *primary.Version
	}

	derived := purl.PackageURL{
		Type:      pp.Type,
		Namespace: newNamespace,
		Name:      newName,
		Version:   version,
	}
	return derived.String(), nil
}

// normaliseVendoredPath renders the vendored path as the spec's
// hash-directive form: "./<cleaned>/". Backslashes become forward
// slashes; leading "./" and trailing "/" are trimmed, then the prefix
// and suffix are reapplied canonically.
func normaliseVendoredPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "./"
	}
	return "./" + p + "/"
}

// vendoredPathSegments returns the non-empty, non-"." path segments
// of a vendored-at input, suitable for splicing into a purl namespace.
func vendoredPathSegments(p string) []string {
	p = strings.ReplaceAll(p, `\`, "/")
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" || s == "." {
			continue
		}
		out = append(out, s)
	}
	return out
}

func cleanExtensions(exts []string) []string {
	out := make([]string, 0, len(exts))
	for _, raw := range exts {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func strPtrNew(s string) *string { return &s }

// flagNameFor maps a Component JSON field name to the CLI flag name
// that populates it. Used in --from override warnings.
func flagNameFor(field string) string {
	switch field {
	case "bom-ref":
		return "bom-ref"
	case "depends-on":
		return "depends-on"
	case "external_references":
		return "external/website/vcs/distribution/issue-tracker"
	default:
		return field
	}
}
