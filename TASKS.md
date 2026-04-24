# bomtique consumer — implementation tasks

A Go CLI that consumes Component Manifest v1 (`spec/component-manifest-v1.md`)
and emits one CycloneDX (and optionally SPDX) SBOM per primary manifest.

Milestones are roughly ordered by dependency. Each task ends in a verifiable
state (tests green, flag works, spec rule enforced). "[§N]" refers to the spec
section a task implements.

## M0 — Project scaffolding

- [x] Initialize Go module `github.com/interlynk-io/bomtique`, target Go 1.26+. (PR #4)
- [x] Layout: `cmd/bomtique/` (main), `internal/manifest/`, `internal/hash/`,
      `internal/pool/`, `internal/graph/`, `internal/emit/cyclonedx/`,
      `internal/emit/spdx/`, `internal/jcs/`, `internal/purl/`,
      `internal/safefs/`, `internal/diag/`. Every package has at minimum a
      `doc.go` declaring scope; `diag`, `safefs`, and `emit/cyclonedx`
      carry working stubs (warning channel, NFC helper, UUIDv5 serial).
- Dependency choices:
  - [x] PURL parsing/canonicalization — in-repo `internal/purl` package,
        forked from `interlynk-io/lynkctl/pkg/purl`, modularized into
        purl/encoding/chars/qualifiers/subpath/types/normalize/validate,
        extended with `Equal` / `CanonEqual`. Full package-url/purl-spec
        suite green. (PR #4)
  - [x] `github.com/google/uuid` for UUIDv5 serial numbers (used by
        `internal/emit/cyclonedx.deterministicSerial`).
  - [x] `github.com/spf13/cobra` for the CLI surface (`cmd/bomtique`).
  - [x] `golang.org/x/text/unicode/norm` for NFC path normalization
        (`internal/safefs.ToNFC`).
  - [x] No CycloneDX library dependency; hand-roll the writer until the
        output shape is locked.
- [x] Makefile with `build`, `test`, `test-race`, `vet`, `fmt`, `fmt-check`,
      `lint`, `cover`, `fuzz`, `tidy`, `tools`, `ci`, `clean` targets.
- [x] CI (GitHub Actions): `go test ./...`, `go vet`, fmt check,
      race detector, coverage artifact, staticcheck, golangci-lint,
      Go 1.26 matrix, Linux + macOS + Windows.
- [x] SPDX short-form license headers on all Go sources; `CONTRIBUTING.md`,
      `CHANGELOG.md`, `SECURITY.md`.

## M1 — Manifest model and parsing

- [x] Go types: `Manifest`, `PrimaryManifest`, `ComponentsManifest`, `Component`,
      `Supplier`, `License`, `LicenseText`, `Hash`, `ExternalRef`, `Pedigree`,
      `Ancestor`, `Patch`, `Diff`, `Attachment`, `Commit`, `Resolves`, `Lifecycle`.
      Use pointer fields where absence is semantically distinct from zero value.
- [x] JSON parser [§4, §5]: strict UTF-8 check, reject invalid sequences; reject
      duplicate keys via a token-walk pre-pass; capture unknown top-level and
      component fields into sidecar `Unknown` maps per §5.1, §5.2, §6.2;
      accept string-shorthand `license` and string-or-attachment `diff.text`.
- [x] CSV parser [§4.5, §4.5.1]: BOM strip; accept CRLF + LF; first non-blank
      line is the `#...` marker; second is fixed column header (exact match);
      skip blank/whitespace-only lines; enforce `hash_value` XOR `hash_file`;
      comma-split `depends_on` and `tags` with RFC4180 quoting.
      `#primary-manifest/v1` in CSV is rejected (§4.1 — primary manifests are JSON-only).
- [x] Schema marker detection [§4.4]: route to primary vs components path;
      reject `primary-manifest/*` or `component-manifest/*` that is not exactly
      `v1`; files without any marker return `ErrNoSchemaMarker` so discovery
      (M11) can silently ignore them.
- [x] Round-trip tests for every Appendix B example (B.1–B.8).

## M2 — Path and security primitives (`internal/safefs`)

- [x] `ResolveRelative(manifestDir, p)` [§4.3]: reject absolute POSIX, Windows
      drive-letter (incl. drive-relative `X:foo`), UNC (`\\…`, `//…`), and
      rooted (`\foo`) paths; reject any post-resolution path escaping
      `manifestDir` via `..`; NFC-normalize both sides before use [§4.6];
      reject empty and NUL-bearing paths.
- [x] Symlink-safe open [§18.2]: `CheckNoSymlinks` walks each path component
      from `manifestDir` and `Lstat`s per segment; `Open` / `ReadFile` chain
      resolve → no-symlink → regular-file check → open.
- [x] File-size cap (default 10 MiB via `DefaultMaxFileSize`) [§8]. Streaming
      `cappedFile` reader returns `ErrFileTooLarge` on overrun rather than
      silently truncating; `ReadFile` and `Open` share the cap logic.
- [x] Tests covering: `..` traversal, POSIX absolute, UNC (both slashes),
      drive-letter (upper/lower/forward/relative), rooted, empty, NUL byte,
      NFC-vs-NFD input, symlink as target, symlink as intermediate directory,
      oversize (one-shot and streaming), exact-cap, missing file, directory
      as file.

## M3 — Hashing (`internal/hash`)

- [x] Algorithm allowlist: SHA-256, SHA-384, SHA-512, SHA-3-256, SHA-3-512 [§8.1].
      `Parse` rejects MD5, SHA-1, and every other name (including case
      variants and CycloneDX's `SHA3-*` form) with `ErrUnsupportedAlgorithm`.
      `Algorithm.New()` pulls constructors from stdlib `crypto/sha256`,
      `crypto/sha512`, and `crypto/sha3` (Go 1.25+ stdlib).
- [x] Literal form passthrough (lowercase hex) [§8.1]: `ValidateLiteralValue`
      checks length matches the algorithm's hex width and every byte is in
      `[0-9a-f]`; M7 passes the value through without recomputation.
- [x] File form [§8.2]: open via `safefs.Open` (symlink refusal + size cap),
      stream into the hash, emit lowercase hex.
- [x] Directory digest [§8.3, §8.4]: `filepath.WalkDir`-based walk, skip
      dirs starting with `.` (returns `fs.SkipDir`), skip every symlink
      (dir and file), skip hidden files at walked levels, filter by
      case-insensitive extensions (strip leading `*.` or `.`, NFC-normalise
      both sides per §4.6), reject empty result with `ErrEmptyDirectory`,
      per-file digest with per-file size cap, NFC-normalise the forward-slash
      relative path before adding to the manifest string, sort lines
      byte-wise, final digest over UTF-8 bytes. Regular-file target falls
      back to File form (§8.3 first bullet).
- [x] Per-file size cap applied during the walk via `io.LimitReader(f, max+1)`
      and byte-count check; overrun returns `safefs.ErrFileTooLarge`.
- [x] Tests using `t.TempDir` fixtures (no committed binaries) with a
      spec-literal reference reconstruction of the §8.4 algorithm, covering:
      known FIPS 180-4 SHA-256 vector ("abc"), empty-file vector, nested
      dirs, hidden-dir skip (`.git/`, `.venv/`), hidden-file skip, symlink
      file and symlink directory skip, extension filter in bare / `.ext` /
      `*.ext` forms with mixed-case basenames, forward-slash relative paths
      across nested dirs, deterministic ordering from non-alphabetical
      write order, SHA-256 vs SHA-3-256 distinctness, regular-file
      fallback, per-file oversize rejection, missing target, empty
      directory and empty-after-filter rejection, and hash package's own
      negative cases for MD5/SHA-1 and invalid literal hex.

## M4 — Validation (`internal/manifest/validate`)

- [x] Structural / semantic validator implemented in Go (`internal/manifest/validate`).
      The canonical JSON Schema 2020-12 document that Appendix A reserves
      is still outstanding — M9's `bomtique manifest schema` will emit
      it once authored.
- [x] Semantic validator [§13.2]:
  - [x] name non-empty; at least one of version/purl/hashes [§6.1].
  - [x] each hash is exactly one form (literal / file / path).
  - [x] file/dir hashes resolve and directory produces ≥ 1 file under the
        §8.4 walk; filesystem checks skippable via `Options.SkipFilesystem`.
  - [x] algorithms in the permitted set (SHA-256, SHA-384, SHA-512,
        SHA-3-256, SHA-3-512); literal values validated for lowercase hex
        and algorithm-correct length.
  - [x] path traversal, absolute-form, and symlink rules delegated to safefs.
  - [x] patched-purl rule [§9.3] via `internal/purl.CanonEqual`, skipping
        silently when either side fails to parse (ErrPurlParse already raised).
  - [x] enumerations for `type`, `scope`, `external_references[].type`,
        `patches[].type`, `lifecycles[].phase` (all case-sensitive per §7).
  - [x] license object: `expression` required; every `texts[].id` is a
        simple SPDX identifier that appears as a bare token in `expression`
        after stripping `AND`/`OR`/`WITH`/parens/`+`; exactly one of
        `text` or `file` per texts entry. Full SPDX grammar reserved for
        `Options.SPDXExpressionStrict` (not yet enabled).
  - [x] multi-primary: every primary has non-empty `depends-on` [§10.4].
  - [x] processing-set is non-empty of primaries [§12.1]; a single primary
        may omit depends-on (convenience rule).
  - [x] empty `components[]` in a components manifest rejected [§5.2].
- [x] Error surface carries: manifest path, JSON pointer (JSON inputs),
      row number + CSV column name (CSV inputs) via `pointerToRowColumn`,
      offending value, and a `Kind` classifier for programmatic filtering.
      No panics.

## M5 — Pool construction, identity, dedup (`internal/pool`)

- [x] Canonical-form purl comparison [§9.3]: `Identify` canonicalises via
      `internal/purl.Parse` + `String`, so identity keys compare byte-exact
      on the canonical form.
- [x] Identity extraction [§11]: `pool.Identity` + `Identify()` with Kind
      precedence purl → name+version → name-only; name-only is the §11
      fallback rung, emptied of version and purl.
- [x] Primary vs pool distinctness check within a single emission [§11]:
      `pool.CheckPrimaryDistinct(primary, pool)` returns a hard error on
      same-Kind identity collision.
- [x] Pool dedup passes [§11]:
  - [x] Direct-identity pass (`directIdentityPass`): four warning cases —
        duplicate purl (drop), same (name, version) with differing purls
        (keep both, warn "likely upstream collision"), no-purl dup (drop),
        name-only dup (drop). First occurrence wins.
  - [x] Secondary mixed purl / no-purl pass (`secondaryPass`): builds
        `(name, version) → purl-bearing` index; merges no-purl matches via
        `mergeNoPurlInto`, keeping purl-bearing scalars / arrays and
        warning per field conflict. Scalar pointer fields merge via
        `mergePtrString`; slice fields via generic `mergeSlice` (for
        comparable elements) or `mergeStringSlice` (for plain strings).
        Object-valued fields (supplier, license, pedigree) compared via
        dedicated `*Equal` helpers.
- [x] Every warning routes through `internal/diag.Warn` (the §13.3 stderr
      `warning:` channel); messages cite source-manifest provenance as
      `path.json#/components/<index>` so operators can locate the
      offending entries.

## M6 — Dependency resolution and reachability (`internal/graph`)

- [x] `depends-on` parsing [§10.2] via `graph.ParseRef`: `pkg:` prefix →
      canonical purl match (invalid purl is `ErrInvalidPurlReference`);
      else last-`@` split handles scoped identifiers like
      `@angular/core@1.0.0`; whitespace, empty, bare-name-without-`@`,
      and trailing-`@` all return `ErrInvalidReference`.
- [x] Pool lookup via `graph.PoolIndex`: `byPurl` (canonical) for `RefPurl`,
      `byNameVersion` (byte-exact) for `RefNameVersion`. `Resolve(ref)`
      returns `(idx, ok)`; misses drive §10.3 warnings and edge drops.
- [x] Transitive closure via `graph.TransitiveClosure`: BFS with visited
      set; unresolved intra-closure edges warn through `internal/diag` and
      drop the edge while preserving the referring component (§10.3);
      unresolved root edges returned separately so the caller can attribute
      them to the primary.
- [x] Reachability rules [§10.4] via `graph.PerPrimary` and
      `graph.ForProcessingSet`:
  - [x] single-primary + empty depends-on → whole pool is direct deps
        (convenience rule).
  - [x] single-primary + non-empty → closure only; per-primary
        "not reachable" warning for every omitted pool component.
  - [x] multi-primary → `ErrMultiPrimaryMissingDepsOn` on any empty
        depends-on; otherwise per-primary closure + unreachable warnings.
  - [x] orphan-across-all: one warning per pool component unreached from
        every primary, emitted once per run, not per SBOM.
- [x] Cycle tolerance: visited set on both root seeding and BFS queue so
      A→B→A and self-edges terminate naturally.

## M7 — CycloneDX emitter (`internal/emit/cyclonedx`)

- [x] Hand-rolled CycloneDX 1.7 writer in struct-per-field form
      (`internal/emit/cyclonedx/types.go`). No map-based emission —
      `json.Marshal` walks structs in declaration order so the serialised
      output is byte-stable across runs.
- [x] `metadata.component` ← primary; `components[]` ← reachable pool;
      emitter takes the pool + per-manifest directories via
      `EmitInput.Reachable` so path-bearing fields resolve against the
      source manifest (§12.4).
- [x] Field mapping table in §14.1 implemented end-to-end:
  - [x] `license` → `licenses[]`: a single `{ expression }` entry always
        when non-empty, plus one `{ license: { id, text } }` per
        `texts[]`. Inline `text` → `{ content, contentType: "text/plain" }`;
        `file` → read via safefs, base64-encode, emit
        `{ content, encoding: "base64", contentType: "text/plain" }`.
  - [x] `hashes`: literal pass-through; file-form computed via
        `internal/hash.File`; path-form via `internal/hash.Directory`
        (handles §8.3 regular-file fallback). Algorithm names translated
        to CycloneDX form (`SHA-3-256` → `SHA3-256`).
  - [x] `pedigree` including `patches[].diff` per §9.2: string-form text
        → `{content, contentType: "text/plain"}`; attachment-form preserved
        field-by-field; local `url` → read via safefs, base64, emit
        `text = {content, encoding: "base64"}`, url field dropped as
        consumed; http(s) url → preserved verbatim, no fetch.
  - [x] `scope` omitted on primary (§5.3); `tags` never serialized (§14.1).
  - [x] `dependencies[]` computed from primary + each reachable pool
        component's `depends-on`. Ref resolution via `graph.ParseRef` +
        both canonical-purl and (name, version) lookup tables over the
        reachable set. Unresolved edges are silently dropped — M6's
        §10.3 warnings have already fired.
  - [x] `metadata.lifecycles` defaults to `[{phase: "build"}]` when the
        primary omits it, per §7.5.
  - [x] `bom-ref` derivation per §15.1 precedence: explicit → canonical
        purl → `pkg:generic/<pct-name>@<version>` RFC 3986 §2.3
        unreserved; `@version` dropped when absent; collisions rejected
        hard via `assignBOMRefs`.
- [ ] Self-validate emitted JSON against a bundled copy of the CycloneDX 1.7
      schema when `--validate-output` is passed (deferred — schema vendor
      lands with M9's CLI flag).

## M8 — Determinism (`internal/emit/cyclonedx` + `internal/jcs`)

- [x] `bom-ref` derivation [§15.1] landed with M7; retained here for the
      §15 checklist. Explicit → canonical purl → `pkg:generic/<pct>@<ver>`
      (RFC 3986 §2.3 unreserved), `@version` dropped when absent,
      collisions rejected via `assignBOMRefs`.
- [x] Sorting [§15.2] in `internal/emit/cyclonedx/sort.go`:
      `components[]` by bom-ref; `dependencies[]` and every
      `dependsOn` list by ref; per-component `hashes` by (alg, content)
      and `externalReferences` by (type, url); pedigree sub-components
      recursively. Applied before Marshal so the serialised output is
      byte-stable without a post-pass.
- [x] `SOURCE_DATE_EPOCH` handling [§15.3]:
      `internal/emit/cyclonedx/determinism.go` resolves the override
      precedence (`Options.SourceDateEpoch` → env var); negative values
      rejected with a named error. Timestamp formatted ISO 8601 UTC
      second precision via `time.Format("2006-01-02T15:04:05Z")`.
      Serial number derived via the JCS path below when SDE is set;
      otherwise `serialNumber` is omitted (consumer MAY generate a
      UUIDv4 at the CLI layer, per §15.3 last paragraph).
- [x] JCS (RFC 8785) [§15.3] in `internal/jcs/jcs.go`:
  - [x] Number canonicalisation implements ECMA-262 toString: integer
        form for `k ≤ n ≤ 21`, decimal form for `0 < n ≤ 21`, leading-
        zero decimal for `-6 < n ≤ 0`, scientific otherwise. Exponent
        normalised to mandatory sign + no-leading-zero digits. `-0`
        collapses to `0`; NaN/Inf rejected.
  - [x] String escaping: `\"`, `\\`, `\b`/`\f`/`\n`/`\r`/`\t`, and
        `\u00NN` (lowercase hex) for any other C0 control char. Other
        characters including the solidus `/` pass through unescaped;
        non-ASCII is emitted as UTF-8 bytes.
  - [x] Key sorting in UTF-16 code-unit order via `utf16.Encode`,
        matching RFC 8785 §3.2.3 (BMP-only inputs sort identically to
        byte order; non-BMP surrogate-pair sequences covered by the
        explicit UTF-16 encoding).
  - [x] Whitespace stripped; arrays preserve input order.
  - [x] Tests cover keys (flat + nested), numbers (`10e3`, `1e21`,
        `1e-6`, `1e-7`, `-0`, `-2.5e10`, etc.), string escapes (incl.
        control char → ``), compound documents, idempotence,
        and parse/trailing-content errors.
- [x] UUIDv5 over `component-manifest/v1/serial/<sha256-of-jcs-components>`
      in the RFC 4122 DNS namespace [§15.3] via
      `computeDeterministicSerial`. `components[]` is already §15.2-
      sorted by the time the serial is computed; output form is
      `urn:uuid:<UUID>`.
- [x] Determinism harness test: `TestDeterminism_ByteIdentical` runs
      `Emit` twice against a rich fixture with SDE set and asserts
      `bytes.Equal`. Companion tests cover component/hashes/extref/
      dependsOn sorting, timestamp formatting, UUIDv5 stability
      across runs and reshaped `components[]`, env-vs-opts
      equivalence, negative-SDE rejection, and absence of timestamp/
      serialNumber when SDE is unset.

## M9 — CLI surface (`cmd/bomtique`)

- [x] `bomtique generate [paths...]`: accepts file paths and globs;
      parses every input, partitions primary vs components, runs
      validator, builds the pool (with `--tag` filter), does per-primary
      reachability, and emits one `<name>-<version>.cdx.json` per
      primary under `--out` (default `./sbom/`). `--stdout` emits NDJSON
      (one compact JSON per line). `--format spdx` returns
      ErrNotImplemented (M10); unknown formats are a usage error.
      Directory-argument discovery is deferred to M11.
- [x] `bomtique validate [paths...]`: structural + semantic via
      `validate.ProcessingSet`; exit 0 on clean, 1 on any error,
      3 on missing-file I/O errors, 2 on cobra usage errors.
- [x] `bomtique manifest schema`: prints a draft-2020-12 placeholder
      document with the two schema-marker constants wired into a
      `oneOf`. Field-level detail is still TODO per spec Appendix A —
      the Go validator is authoritative today.
- [x] Flags:
  - [x] `--max-file-size` (default `safefs.DefaultMaxFileSize` = 10 MiB),
        threaded through `validate.Options` and `cyclonedx.Options`.
  - [x] `--tag <t>` (repeatable) filters pool components whose `tags`
        include any listed value; applied before reachability (§6.2).
  - [x] `--warnings-as-errors` promotes any `diag.Warn` to exit 4.
  - [x] `--source-date-epoch <n>` overrides the env var when set; both
        routes feed `cyclonedx.Options.SourceDateEpoch`.
  - [x] `--follow-symlinks` accepted; warns that safefs's opt-in path
        isn't yet wired (symlinks still refused).
  - [x] `--output-validate` accepted; warns that schema vendor is
        deferred (no-op today).
- [x] Exit codes: 0 ok, 1 validation/semantic error, 2 usage error,
      3 I/O error, 4 warnings-as-errors triggered. Commands signal via
      `*exitErr` wrapping; main unwraps to set `os.Exit`.

## M10 — SPDX emitter (`internal/emit/spdx`)

- [x] SPDX 2.3 JSON output in `internal/emit/spdx` — struct-per-field
      document types so `json.Marshal` produces byte-stable ordering.
- [x] DESCRIBES from `SPDXRef-DOCUMENT` to the primary's SPDXID;
      DEPENDS_ON per resolved depends-on edge (§14.2 table). Unresolved
      edges silently dropped — M6 already warned.
- [x] License texts merged into `licenseComments` under `=== <id> ===`
      heading blocks; inline text passed through, file-backed text read
      via safefs under the `--max-file-size` cap.
- [x] `externalRefs` mapping per §14.2:
      - purl → PACKAGE-MANAGER/purl
      - cpe → SECURITY/cpe23Type
      - `external_references[type=vcs]` → OTHER/vcs
      - any other type → OTHER/other with `comment: "original type: X"`
      - `website` → `package.homepage` (not an externalRef)
      - `distribution` → `package.downloadLocation` (not an externalRef)
- [x] Dropped-field warnings emitted exactly once per class per run via
      `droppedCounter`: scope, pedigree.variants, pedigree.descendants,
      metadata.lifecycles. Warnings route through `internal/diag`.
- [x] Supplier rendered as `Organization: <name>[ (<email>)]`; type
      mapped to SPDX `primaryPackagePurpose` (library/application/
      framework/container/operating-system/device/firmware/file
      lossless; platform/device-driver/machine-learning-model/data →
      OTHER per §14.2); hash algorithm names translated to SPDX form
      (`SHA-256` → `SHA256`, `SHA-3-256` → `SHA3-256`, etc.). Pedigree
      ancestors/commits rendered into `sourceInfo`; notes into
      `package.comment`; patches into `package.annotations`.
- [x] SPDXID derivation sanitises to the `[A-Za-z0-9.\-+]` charset the
      SPDX spec allows, with collision-safe index suffixing.
- [x] Determinism: when SOURCE_DATE_EPOCH is set the document
      timestamp uses ISO 8601 UTC seconds and `documentNamespace` is
      derived from a SHA-256-over-JCS-canonicalised primary-plus-pool
      identifier seed → UUIDv5. Byte-identical SBOM output across two
      runs verified in tests.
- [x] `--format spdx` wired in `cmd/bomtique/generate.go`; per-primary
      `<name>-<version>.spdx.json` filenames; tests cover the end-to-end
      path (exit 0, file present, `spdxVersion`/`DESCRIBES` sanity).
- [ ] Post-emit JSON Schema validation (vendored SPDX 2.3 schema) under
      `--validate-output` — deferred with the CycloneDX schema validation
      to a follow-up PR.

## M11 — Discovery (non-normative, documented)

- [x] `bomtique generate` / `validate` with no positional arguments
      trigger a `discover(".")` walk; a directory argument walks that
      directory. `filepath.WalkDir` matches basenames `.primary.json`,
      `.components.json`, `.components.csv`. `.`-prefixed directories
      and the hardcoded set (`.git`, `node_modules`, `vendor`, `.venv`)
      are skipped with `fs.SkipDir`. Symbolic-link entries (file or
      dir) are skipped — §18.2 defaults hold at the discovery layer
      too.
- [x] Deterministic traversal order via `filepath.WalkDir`'s sorted
      `ReadDir` — two runs against the same tree yield the same path
      sequence (tested).
- [x] Files without a schema marker are silently skipped (§12.5) in
      both discovery and explicit-path flows — `readManifests` catches
      `manifest.ErrNoSchemaMarker` and continues.
- [x] `docs/discovery.md` documents the conventional filenames, the
      exclusion set, symlink handling, determinism, and the
      silently-ignore rule per §12.5 SHOULD.

## M12 — Conformance test suite

- [x] `cmd/bomtique/testdata/conformance/` split into `positive/` and
      `negative/` fixture directories. Each fixture is a standalone
      processing set; positive fixtures carry a `golden/` subdir with
      byte-exact expected CycloneDX output; negative fixtures carry
      `expect-errors.txt` with the substrings that must appear in
      stderr. Regeneration via `BOMTIQUE_REGENERATE_GOLDEN=1 go test`.
- [x] Positive fixtures (4 so far): b1-minimal-primary,
      multi-primary-shared-pool (§10.4 multi-primary closure),
      dual-licensed-native (compound license), sub-component-bomref
      (explicit bom-ref precedence). B.5 / B.8 / CSV / vendored-dir-
      hash expansions are out of scope for M12 as structured and
      should land as additional fixtures in a follow-up.
- [x] Negative fixtures (7): absolute-path, path-traversal,
      unknown-schema-version, bad-type-enum, invalid-purl,
      mixed-hash-form, forbidden-algorithm. Symlink + oversize
      negatives are covered by `internal/safefs/` and
      `internal/hash/` unit tests and deferred at the conformance
      layer (both need runtime fixture materialisation).
- [x] `TestConformance_Positive` byte-compares each fixture's output
      against its `golden/` tree; `TestConformance_Determinism`
      reruns each positive fixture and asserts byte-equal output
      across two invocations with identical SOURCE_DATE_EPOCH.
- [x] `TestConformance_Negative` asserts every substring in
      `expect-errors.txt` appears on stderr and the command exits
      with a non-zero code.
- [x] Fuzz targets added with seed corpora, discovered by
      `go test ./...` (seed-corpus mode) and runnable with
      `go test -fuzz=...`:
      - `internal/manifest.FuzzParseJSON` / `FuzzParseCSV`
      - `internal/purl.FuzzParse` / `FuzzCanonEqual`
      - `internal/safefs.FuzzResolveRelative`
      The directory-walk fuzz target is deferred — it requires a
      temporary FS builder, which is better structured as an M13
      follow-up.

## M13 — Packaging, docs, release

- [x] `docs/` landed: `usage.md`, `discovery.md` (M11), `determinism.md`,
      `security.md`, `compatibility.md` (spec-MUST-vs-SHOULD map with per-
      clause code-path citations).
- [x] `.goreleaser.yaml` building linux/amd64, linux/arm64, darwin/arm64,
      windows/amd64 with -trimpath + `-X main.version={{.Version}}`; SHA-256
      checksums; cosign signing hook (keyless via GitHub OIDC at release
      time).
- [x] `Dockerfile` multi-stage build onto `gcr.io/distroless/static-
      debian12:nonroot`. `.dockerignore` trims tests / SBOM artefacts.
- [x] Dogfood manifests at the repo root (`.primary.json`,
      `.components.json`) plus the committed output
      `sbom/bomtique-0.1.0.cdx.json` — generated under
      `SOURCE_DATE_EPOCH=1700000000` so it reruns byte-identically.
- [x] `CHANGELOG.md` v0.1.0 entry enumerating every M1–M13 milestone
      plus the explicit deferred items (canonical Appendix A schema,
      `--output-validate`, `--follow-symlinks` opt-in, full SPDX grammar,
      directory-walk fuzz corpus).
- [ ] Tag `v0.1.0` once this PR merges and CI is green on all platforms.
      Left to the repo owner to cut the tag; `goreleaser release` is
      wired and ready.

## Cross-cutting invariants (asserted in tests, not just code)

- **Byte-identical determinism** under `SOURCE_DATE_EPOCH` — tested per emitter.
- **No network** at runtime — `net.DefaultResolver`/`http.DefaultTransport`
  overridden to fail in the test binary; any attempted fetch crashes tests.
- **No symlink follow** by default — enforced by `internal/safefs`, never
  bypassed elsewhere.
- **Warning channel is stderr, prefix `warning:`** — a single `diag` package
  is the only writer; vet-rule / import-restriction to keep it that way.
- **Canonical purl comparison everywhere** — a lint test greps for raw
  `==`/`strings.EqualFold` on purl fields outside `internal/purl`.

## Open questions to resolve before M4

- Appendix A JSON Schema is `TODO` in the spec. We will author a draft
  2020-12 schema in M4 and propose it as the canonical appendix content
  upstream.
