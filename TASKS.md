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
- [x] Self-validate emitted JSON against the vendored CycloneDX 1.7
      schema when `--output-validate` is passed. Bundle lives in
      `schemas/cyclonedx/` (bom-1.7.schema.json plus the four `$ref`
      siblings: spdx, jsf-0.82, cryptography-defs). Validation runs
      via `internal/schema` using `github.com/santhosh-tekuri/jsonschema/v6`.

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
- [x] Post-emit JSON Schema validation against the vendored SPDX 2.3
      schema (`schemas/spdx/spdx-schema.json`) under `--output-validate`.
      Shares `internal/schema` with the CycloneDX path so both formats
      go through the same santhosh-tekuri-backed validator.

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

## M14 — Hand-authored manifest mutation (`bomtique manifest init|add|remove|update|patch`)

A CRUD surface over primary and components manifests. Extends the
existing `bomtique manifest` subcommand group. Pure metadata: no
`git clone`, no tarball fetch, no archive extraction. Metadata-only
registry lookups (HTTPS GET against well-known JSON endpoints) are
opt-in per command and are the ONLY network access added by this
milestone.

### M14.0 — Mutation primitives (`internal/manifest/mutate`)

- [x] New package `internal/manifest/mutate` holding the parse-edit-
      rewrite engine shared by every M14 subcommand.
- [x] `WriteJSON(w, *Manifest)` lands with 2-space indent + trailing
      newline. The custom `MarshalJSON` methods already in
      `internal/manifest/json.go` now append Unknown keys in sorted
      order (additive change; fixtures with no unknowns still round-
      trip identically). `extractUnknown` canonicalises values with
      `json.Compact` so stored bytes survive `json.Indent` reflow and
      a second parse observes byte-equal unknowns. Round-trip tested
      against every Appendix B JSON fixture plus unknown-preservation
      cases at primary, components, and component scope.
- [x] `WriteCSV(w, *Manifest)`: emits the `#component-manifest/v1`
      marker, the frozen §4.5 column header, one row per component,
      LF, RFC 4180 quoting. Fails closed on components CheckFitsCSV
      rejects (no partial writes). Round-trip tested against b8.csv.
- [x] `LocatePrimary(fromDir string) (path string, err error)`: walks
      up from `fromDir` for `.primary.json` whose schema marker equals
      `primary-manifest/v1`. Honours the `cmd/bomtique/discovery`
      exclusion set (`.git`, `node_modules`, `vendor`, `.venv`,
      `testdata`, plus any `.`-prefixed directory) and refuses
      symlinks. Returns `ErrPrimaryNotFound` on miss.
- [x] `LocateOrCreateComponents(fromDir, flagInto string) (path string, created bool, err error)`:
      `flagInto` wins (relative resolved against fromDir); else
      nearest `.components.json` (preferred) or `.components.csv`
      walking up; else falls back to `<primaryDir>/.components.json`
      with `created=true`. Returns `ErrPrimaryNotFound` when no
      primary is reachable and no `--into` was supplied.
- [x] `MergeComponent(base, overrides *Component) (*Component, []FieldOverride)`:
      returns a clone of base with overrides layered on top and the
      ordered list of fields the override layer replaced. Pointer
      scalars override when non-nil; slices override wholesale when
      non-nil; nested objects (supplier, license, pedigree) replace
      wholesale. Does not mutate either input.
- [x] `CheckFitsCSV(c *Component) error`: returns a clear error
      naming the first field (bom-ref, external_references,
      structured license texts, directory-form hashes, multiple
      hashes, pedigree, lifecycles, unknown fields) that CSV §4.5
      can't carry.
- [x] Unit tests cover Unknown preservation (including RawMessage
      splice for nested objects), struct-order stability, CSV column
      order and quoting, locate walk semantics, create-alongside-
      primary, merge precedence and input immutability, CSV-fit
      rejection per field class. Full `go test ./...` + `go vet` +
      `gofmt -l` green; `go test -race ./internal/manifest/...` green.

### M14.1 — `bomtique manifest init`

- [x] Command: `bomtique manifest init --name … [--version …] [--type …] [--license …] [--purl …] [--cpe …] [--supplier …] [--supplier-email …] [--supplier-url …] [--description …] [--website …] [--vcs …] [--distribution …] [--issue-tracker …]`.
- [x] `--force` overwrites existing `.primary.json`; default refuses
      with `mutate.ErrPrimaryExists` (exit 1).
- [x] `-C`/`--chdir <dir>` runs in a specific directory; default CWD.
- [x] Default `--type` is `application` (primary is the buildable
      output). `library` default from §7.1 applies to pool components;
      primaries are the exception.
- [x] Builds `PrimaryManifest` from flags, runs `validate.Manifest`
      with `SkipFilesystem: true` (no hashes yet so no filesystem
      reads), writes `.primary.json` atomically (tmp file + rename).
- [x] Does NOT create `.components.json`: §5.2 forbids empty
      `components[]`. First `add` creates it.
- [x] Exit codes: 0 success; 1 validation or ErrPrimaryExists; 2 usage
      (cobra MarkFlagRequired on `--name`); 3 I/O.
- [x] Tests: happy path; default-type application; custom-type;
      refuse existing without `--force`; accept `--force`; `--force`
      preserves unknown top-level AND unknown component fields;
      validation error surfaces on stderr; missing-name is usage
      error; non-existent dir rejected; Dir=file rejected; canonical
      indented output; whitespace trimmed; RawMessage invariant held
      for nested JSON values. End-to-end cobra tests in
      `cmd/bomtique/manifest_init_test.go` including a
      `init -> validate` round-trip.

### M14.2 — `bomtique manifest add` (offline paths)

- [x] Command: `bomtique manifest add` with component-field flags
      (`--name`, `--version`, `--type`, `--license`, `--purl`,
      `--supplier`, `--supplier-email`, `--supplier-url`,
      `--description`, `--cpe`, `--scope`, repeatable `--depends-on`,
      repeatable `--tag`, shorthand external refs `--website`,
      `--vcs`, `--distribution`, `--issue-tracker`, repeatable
      `--external type=url`).
- [x] `--from <path|->` reads a bare Component JSON object (stdin when
      `-`); flag values override file values with one stderr note per
      override via `internal/diag`.
- [x] `--into <path>` picks the target components manifest; default
      uses `LocateOrCreateComponents`.
- [x] `--primary` writes the resolved ref (`purl` preferred; else
      `name@version`) to the primary's `depends-on`; dedup surfaces
      as `AlreadyPresent=true` / "unchanged" stdout, no write.
- [x] Identity collision against existing pool (§11) rejected hard as
      `*ErrIdentityCollision`; error names the colliding entry.
- [x] CSV target: refuses with `CheckFitsCSV`-derived message
      ("…; rerun with --into <json-path>").
- [x] `--scope` ignored with `diag.Warn` when `--primary` set (§5.3);
      the warning routes to stderr and counts for
      `--warnings-as-errors`.
- [x] Post-mutation pool validation via `validate.Manifest` with
      `SkipFilesystem: true` surfaces malformed components before the
      file is written; atomic write through tmp + rename.
- [x] Exit codes: 0 success; 1 collision / validation / missing
      primary / ErrInvalidRef; 2 usage (cobra flag errors, malformed
      `--external` values); 3 I/O. Race-detector green.
- [x] Tests cover: pool happy path with .components.json creation,
      pool append, primary purl/name-version ref derivation, primary
      dedup, primary scope warning, primary missing manifest, stdin
      `--from`, file `--from`, flag override emits diag, identity
      collision, CSV pedigree rejection, `--external type=url` parse,
      malformed `--external`, end-to-end `init → add → validate`.

### M14.3 — `bomtique manifest remove`

- [x] Command: `bomtique manifest remove <ref>` where `<ref>` is a
      `pkg:` purl or `name@version`. Parsed via `graph.ParseRef` so
      every comparison uses the canonical form.
- [x] Locates the component across every `.components.json` /
      `.components.csv` reachable by walking *down* from the primary
      directory (new `discoverComponentsManifests` helper mirrors the
      `cmd/bomtique/discovery` exclusion set). Multi-match is a hard
      error (`*ErrRemoveMultiMatch`); user disambiguates with
      `--into <path>`.
- [x] Drops the entry from `components[]` and writes the
      corresponding manifest atomically.
- [x] Scrubs `depends-on` references to the removed component from
      every other pool component AND from the primary. One
      `diag.Warn` stderr line per scrubbed edge naming the referring
      component's identity (§10.2 ref semantics).
- [x] `--dry-run` prints the planned mutation ("would remove …")
      without writing. Tested byte-equality of before/after disk
      state.
- [x] `--primary` form: scrubs only from the primary's `depends-on`;
      pool untouched; missing edge is a hard error so scripts don't
      silently succeed.
- [x] Exit codes: 0 success; 1 validation / not-found / multi-match /
      missing primary / parse error; 2 cobra usage (wrong arg count).
      Race-detector green.
- [x] Tests cover: pool happy path (purl ref AND name@version ref);
      sibling depends-on scrubbed; primary depends-on scrubbed;
      multi-match across files; `--into` disambiguates; `--primary`
      pool-untouched; `--primary` missing-edge error; `--dry-run`
      writes nothing; `DependsOn` slice nils out on full scrub;
      not-found exits 1; end-to-end `init → add → add → remove →
      validate`.

### M14.4 — `bomtique manifest update`

- [x] Command: `bomtique manifest update <ref> [flags]`. Field flags
      identical to `add`; each supplied flag replaces that field.
      Unset flags preserved.
- [x] `--to <version>` bumps `version`. If `purl` is present AND its
      version segment equals the old version (parsed via
      `internal/purl`), the purl is bumped in lockstep and
      `UpdateResult.PurlVersionBumped` is true; otherwise the purl
      stays as-is with a `diag.Warn` stderr line telling the user to
      update manually.
- [x] `--clear-license`, `--clear-description`, `--clear-supplier`,
      `--clear-purl`, `--clear-cpe`, `--clear-scope`,
      `--clear-external-refs`, `--clear-depends-on`, `--clear-tags`,
      `--clear-pedigree-patches` null-out flags.
- [x] `pedigree.patches[]` and all other pedigree sub-fields
      preserved across updates unless `--clear-pedigree-patches` is
      supplied.
- [x] Directory-form hashes preserved verbatim: §15.4 recomputes at
      `scan` time; `update` does not touch hashes.
- [x] Identity after update stays distinct from every other pool
      entry; collision returns `*ErrIdentityCollision`.
- [x] `--dry-run` reports planned mutation and reverts in-memory
      state so the on-disk file is untouched (tested with
      byte-equality of pool file before/after).
- [x] Exit codes: 0 success; 1 not-found / collision / validation /
      parse / missing primary; 2 cobra usage.
- [x] Tests cover: field replace; `--to` with purl lockstep; `--to`
      with mismatched purl (warning, no purl change); pedigree
      patches preserved across version bump; collision after bump;
      `--clear-license`; `--clear-pedigree-patches`; not-found;
      no-changes error; dry-run no-write; name@version ref;
      `--into` narrowing.

### M14.5 — `bomtique manifest patch`

- [x] Command: `bomtique manifest patch <ref> <diff-path> --type unofficial|monkey|backport|cherry-pick [--resolves "key=val,key=val"]* [--notes <text>] [--replace-notes] [-C <dir>] [--into <path>] [--dry-run]`.
- [x] Appends a `Patch` entry (§7.4) under the target component's
      `pedigree.patches[]`. The `diff` field is emitted as
      `{ url: <relative-path> }` pointing at the on-disk diff; bomtique
      does not read or copy the diff file (safefs reads it at `scan`
      time per §9.2).
- [x] Absolute diff paths rejected via `safefs.ResolveRelative`
      (§4.3). UNC, drive-letter, and `..` traversal escapes all
      rejected uniformly. Error message names "diff path" so users
      know which argument failed.
- [x] Creates `pedigree` when absent; appends to `pedigree.patches`
      when present; preserves existing ancestors / commits / notes.
- [x] Repeatable `--resolves` takes key=value,key=value syntax
      (type, name, id, url, description). Each entry MUST carry at
      least `name=` or `id=`. Unknown keys and missing required keys
      raise usage errors (exit 2).
- [x] `--notes` appends to `pedigree.notes` with `\n\n` separator
      when notes already present; `--replace-notes` overwrites.
      Blank `--notes` is a no-op.
- [x] Exit codes: 0 success; 1 validation / not-found / invalid
      type / resolves parse / missing primary; 2 cobra usage (wrong
      arg count, missing --type, malformed --resolves).
- [x] Tests cover: first-patch-creates-pedigree; second-patch-
      appends; invalid `--type`; absolute path rejected; traversal
      path rejected; resolves entries produced with type+name+url;
      invalid resolves type; resolves without name/id rejected;
      notes append; notes replace; dry-run no-write; not-found;
      empty diff path.

### M14.6 — `bomtique manifest add --vendored-at <dir>`

- [x] New flag on `add`: `--vendored-at <rel-dir>`. Dir MUST resolve
      under the target components manifest per §4.3 (validated via
      `safefs.ResolveRelative`) and MUST exist on disk.
- [x] `--ext c,h,...` (comma-separated or repeatable) sets the
      directory-hash extension filter (§8.3); lands in the emitted
      `hashes[0].extensions`.
- [x] `--vendored-at` cannot combine with `--primary` (hard error).
- [x] When set, `add` synthesises three fields on the emitted
      Component:
  - [x] `purl`: repo-local form per §9.3 pattern 1. Derived from the
        nearest primary's purl when that type is in
        `{github, gitlab, bitbucket}`; otherwise hard error asking
        the user to pass `--purl`. If `--purl` is set explicitly,
        derivation is skipped. Uses `internal/purl.PackageURL` to
        produce canonical form (e.g.
        `pkg:github/acme/device-firmware/src/vendor-libx@2.4.0`).
  - [x] `hashes[0]`: `{ algorithm: "SHA-256", path: "./<dir>/", extensions: … }`.
        Digest NOT computed at `add` time — §15.4 defers to scan.
        Only installed when the incoming component has no hashes
        already (so `--from` input with hashes survives).
  - [x] `pedigree.ancestors[0]`: built from `--upstream-name`,
        `--upstream-version`, `--upstream-purl`, `--upstream-supplier`,
        `--upstream-website`, `--upstream-vcs`. Name is required when
        any `--upstream-*` flag is set (§9.1), plus at least one of
        version/purl. Zero upstream flags → no ancestor emitted
        (pedigree stays nil).
- [x] If the resulting component purl (derived OR explicit) equals
      the canonicalised upstream purl, reject per §9.3 with a
      message citing the section.
- [x] Tests cover: github primary derivation end-to-end; npm primary
      requires `--purl` (error mentions `--purl`); explicit `--purl`
      skips derivation; §9.3 purl==upstream-purl rejection; absolute
      path rejection; traversal path rejection; missing directory
      rejection; `--upstream-version` without `--upstream-name`
      rejection; zero upstream flags produce hash+purl but no
      pedigree; `--vendored-at`+`--primary` combination rejected.
      Plus end-to-end `init → add --vendored-at → validate` via the
      cobra surface, asserting the emitted purl matches the §9.3
      pattern-1 canonical form.

### M14.7 — Registry importer framework (`internal/regfetch`)

- [x] New package `internal/regfetch` with an `Importer` interface
      (`Name`, `Matches`, `Fetch`). Process-global `Registry` with
      first-match-wins dispatch via `regfetch.Fetch` /
      `regfetch.Match`; isolated `NewRegistry()` for tests.
- [x] Shared `Client` with 30 s total request timeout, 1 MiB
      response cap via `io.LimitReader(body, max+1)`, `Accept:
      application/json`, User-Agent
      `bomtique/<version> (+https://github.com/interlynk-io/bomtique)`.
      `Client.SetUserAgentVersion` swaps the version at startup. No
      retries.
- [x] `--offline` flag on `add` and `update` skips `regfetch` entirely
      (never opens a socket). Enforced by unit tests that fail
      immediately if the fake importer's `Fetch` is called under
      `--offline`.
- [x] `--online` forces a fetch attempt; errors with
      `ErrUnsupportedRef` when no importer matches the ref, when
      `--online` is paired with a ref that isn't a `pkg:` purl or
      URL, or when the target of `update --online` has no purl.
- [x] Default behaviour on `add`: auto-fetches when a registered
      importer matches `opts.Purl` (pkg: prefix) or URL-shaped
      `opts.Name`; skeleton path otherwise. Default behaviour on
      `update`: skip regfetch unless `--online` is supplied, since
      plain field rewrites shouldn't do network I/O.
- [x] `--offline` and `--online` are mutually exclusive (hard error).
- [x] All outbound HTTP routed through the shared `Client` so tests
      stub it via `httptest.Server`. Per-importer env-var base-URL
      overrides are documented as an M14.8+ surface and will land
      alongside the first real importer.
- [x] Structured errors: `ErrNetwork`, `ErrNotFound`,
      `ErrRateLimited`, `ErrUnsupportedRef`, `ErrResponseTooLarge`,
      plus `ErrOffline` for callers that need to distinguish the
      skeleton fallback path.
- [x] Consumer-path network invariant enforced by
      `TestNoNetworkImportsOutsideRegfetch` in `internal/regfetch/`:
      walks every production `.go` file and fails if `net/http`,
      `net.Dial`, or `net.DefaultResolver` is referenced outside
      `cmd/bomtique/` and `internal/regfetch/` (comments allowed).
- [x] `add` and `update` compose: importer produces a Component,
      existing `--from` base is merged over the fetched one, flag
      overrides layer on top via M14.0's `MergeComponent`. Update's
      fetched-metadata merge is reported as a `regfetch` entry in
      `UpdateResult.FieldsChanged`.
- [x] Tests cover: registry dispatch + first-match-wins,
      `ErrUnsupportedRef`, `Client.Get` happy path + auth header
      pass-through + status-code pass-through (404 not wrapped) +
      1 MiB cap triggering `ErrResponseTooLarge` + transport-error
      wrapping as `ErrNetwork`, `SetUserAgentVersion` no-op on
      empty, Add `--offline` skip + Add `--online` missing-importer
      error + Add default auto-fetch merges below flag layer + Add
      URL-shaped name routing, Update `--online` refresh + flag
      override precedence + missing-purl error + default-skip.
      Full `go test ./...` + `golangci-lint run ./...` green.

### M14.8 — GitHub importer

- [x] Recognises:
      - `https://github.com/<owner>/<repo>[.git]`
      - `https://github.com/<owner>/<repo>/tree/<ref>`
      - `https://github.com/<owner>/<repo>/releases/tag/<ref>`
      - `https://github.com/<owner>/<repo>/commit[s]/<ref>`
      - `http://` counterparts (normalised)
      - `pkg:github/<owner>/<repo>[@<ref>]`
      Nested `pkg:github` namespaces (§9.3 repo-local form) are
      explicitly rejected with `ErrUnsupportedRef` — those purls
      aren't importable from the real API.
- [x] `GET https://api.github.com/repos/<owner>/<repo>` for repo
      metadata: name, description, homepage, html_url,
      default_branch, `license.spdx_id`.
- [x] When `<ref>` is supplied: follow-up `GET
      /repos/<owner>/<repo>/git/ref/tags/<ref>` confirms the tag
      exists (404 → `ErrNotFound`). Absent ref falls back to the
      repo's default branch with a `diag.Warn` suggesting the user
      pin.
- [x] Extracted fields: `name` = repo name; `version` = tag or
      default branch; `license` = SPDX ID (NOASSERTION and null
      are both dropped); `description`; `purl` =
      `pkg:github/<owner>/<repo>@<ref>`; `external_references` with
      website (homepage), vcs (html_url), issue-tracker
      (html_url + "/issues").
- [x] `GITHUB_TOKEN` env var, when set, applied as
      `Authorization: Bearer`. Token never appears in any
      user-facing error string (belt-and-braces tests assert the
      negative).
- [x] 404 → `ErrNotFound` with a "did you typo owner/repo?" hint.
- [x] 403 with `X-RateLimit-Remaining: 0` → `ErrRateLimited`
      mentioning `GITHUB_TOKEN`.
- [x] `BaseURL` field + `BOMTIQUE_GITHUB_BASE_URL` env-var override
      (tests inject `httptest.Server` URLs without touching global
      state).
- [x] `init()` registers a `GitHubImporter{}` on the process-global
      registry so `mutate.Add` auto-fetches on pkg:github refs by
      default.
- [x] Tests via `httptest.Server`: URL + purl parsing, nested-purl
      rejection, happy-path purl fetch, happy-path URL fetch, 404
      repo, rate-limited, `license: null`, `NOASSERTION` skipped,
      default-branch fallback with stderr warning, 404 on unknown
      tag, Authorization header applied when `GITHUB_TOKEN` is
      set, token scrubbed from 404 error, token scrubbed from
      generic error (418), `BOMTIQUE_GITHUB_BASE_URL` override
      respected, global registration reachable via `Default().Match`,
      produced Component satisfies §6.1. Full
      `go test ./...` + `go test -race ./...` +
      `golangci-lint run ./...` green.
- [x] Live smoke against api.github.com verified with
      `pkg:github/google/uuid@v1.6.0` — name, version, license
      `BSD-3-Clause`, description, and external_references all
      resolved correctly.

### M14.9 — GitLab importer

- [ ] Recognises `https://gitlab.com/<group>/.../<repo>[/-/tree/<ref>]`
      and `pkg:gitlab/<namespace>/<project>[@<ref>]`.
- [ ] GitLab API v4: `GET /api/v4/projects/<url-encoded-path>` for
      metadata; `/repository/tags/<ref>` for version confirmation.
- [ ] Self-hosted support: `--gitlab-base-url <host>` flag AND
      `BOMTIQUE_GITLAB_BASE_URL` env var override the host.
- [ ] Extracted fields mirror GitHub; purl type `pkg:gitlab/...`.
- [ ] `GITLAB_TOKEN` env var → `PRIVATE-TOKEN` header when set.
      Never logged.
- [ ] Tests via httptest: happy path; self-hosted URL; 404;
      rate-limit; token scrubbing.

### M14.10 — npm importer

- [ ] Recognises:
      - `https://www.npmjs.com/package/<name>`
      - `https://www.npmjs.com/package/<name>/v/<version>`
      - `npm:<name>[@<version>]`
      - `pkg:npm/<name>[@<version>]`
      - Scoped names `@scope/name` handled in URL escaping AND purl
        namespace.
- [ ] `GET https://registry.npmjs.org/<encoded-name>` for metadata; no
      version specified → `dist-tags.latest`.
- [ ] Extracted fields: `name`, `version`, `license` (SPDX string from
      `license`), `description`, `purl`, `supplier` from `author`
      object (`{ name, email, url }`), `external_references` from
      `homepage`, `repository.url`, `bugs.url`.
- [ ] Integrity: when `dist.integrity` present (SRI format
      `sha512-<base64>`), decode to bytes and emit a literal-form hash
      entry `{ algorithm: "SHA-512", value: <lowercase-hex> }` per
      §8.1.
- [ ] Tests via httptest: unscoped; scoped; no-version → latest;
      integrity-hash decode; 404; non-SPDX license string falls through
      with warning.

### M14.11 — PyPI importer

- [ ] Recognises `https://pypi.org/project/<name>[/<version>]`,
      `pypi:<name>[@<version>]`, `pkg:pypi/<name>[@<version>]`.
- [ ] `GET https://pypi.org/pypi/<name>/json` or
      `/pypi/<name>/<version>/json`.
- [ ] Extracted fields: `name` (PEP 503 normalised: lowercased,
      runs of `[-_.]+` collapsed to `-`), `version`, `license` best-
      effort from `info.license` (if not a recognised SPDX ID, warn
      and leave empty, telling the user to pass `--license`),
      `description` = `info.summary`, `purl`, `supplier` from
      `info.author` / `info.author_email`, `external_references` from
      `info.home_page` / `info.project_urls["Source"]` /
      `info.project_urls["Bug Tracker"]`.
- [ ] When a version is pinned, emit per-release SHA-256 as a literal
      hash entry using the sdist (or first wheel) digest from
      `releases[version][i].digests.sha256`.
- [ ] Tests via httptest: specific version; latest; missing license;
      PEP 503 name normalisation; `digests.sha256` populated.

### M14.12 — Cargo importer

- [ ] Recognises `https://crates.io/crates/<name>[/<version>]` and
      `pkg:cargo/<name>[@<version>]`.
- [ ] `GET https://crates.io/api/v1/crates/<name>` for crate
      metadata; `/crates/<name>/<version>` for version-specific info.
- [ ] Extracted fields: `name`, `version`, `license` (SPDX),
      `description`, `purl`, `external_references` from `homepage`,
      `repository`, `documentation`.
- [ ] Per-version SHA-256 checksum → literal hash entry.
- [ ] User-Agent MUST identify bomtique with a contact URL per
      crates.io ToS; the default UA from M14.7 satisfies this —
      verified by a test asserting the header shape.
- [ ] Tests via httptest: happy path; version pinning; UA assertion.

### M14.13 — Docs, conformance, release

- [ ] `docs/usage.md`: add sections for `manifest init`,
      `manifest add`, `manifest remove`, `manifest update`,
      `manifest patch`; examples for each importer; network policy
      recap (`--offline`, `--online`, token env vars, 1 MiB response
      cap, no retries).
- [ ] `docs/security.md`: add an "importer network model" section
      documenting the allowed hosts per importer, the response cap,
      token handling and scrubbing, and the opt-in/opt-out flag shape.
- [ ] `docs/discovery.md`: note that mutation commands reuse the same
      walk semantics as `scan`/`validate` (same `.git`/`node_modules`/
      etc. exclusions).
- [ ] Conformance fixtures under `cmd/bomtique/testdata/manifest/`:
      one directory per subcommand, with input tree + stdin + golden
      output tree. `TestConformance_Determinism`-style rerun proves
      byte-stable output.
- [ ] `CHANGELOG.md`: v0.2.0 entry covering M14.0–M14.12.
- [ ] Dogfood: regenerate `.primary.json` + every `.components.json`
      entry via `manifest init` + `manifest add` against a throwaway
      dir, byte-compare against the committed files to prove
      round-trip stability.

### M14 cross-cutting invariants (asserted in tests)

- **No git clone, no tarball fetch, no archive extraction.** Mutation
  commands read local files and/or registry JSON metadata endpoints
  only. Enforced by the `internal/regfetch` package boundary and an
  import-path lint.
- **Consumer path stays network-free.** `scan`, `validate`, and the
  emitters still cannot open sockets. Only `cmd/bomtique` (for
  command wiring) and `internal/regfetch` import `net/http`.
- **Secrets never logged.** `GITHUB_TOKEN`, `GITLAB_TOKEN`, and any
  future token env var are read once and held in the client; verbose
  mode scrubs them from request dumps. Tested per importer.
- **Deterministic writes.** Same inputs → byte-identical file.
  Unknown-field preservation is byte-stable once formatting converges
  to the 2-space pretty-print baseline.
- **Formatting expectation.** PR 1 reformats to 2-space indent on
  first mutation. Minimal-diff writer deferred.

### M14 open questions to resolve during M14.0

- Formatting preservation policy: 2-space pretty-print (simple,
  documented reformat on first mutation) vs. minimal-diff writer
  preserving authorial whitespace. Recommendation: 2-space first,
  revisit after user feedback.
- Post-mutation validation scope: standalone-validate the new
  Component only, or run full `validate.ProcessingSet`?
  Recommendation: standalone only, user runs `bomtique validate`
  separately.
- `--interactive` walkthrough for `init` / `add`: deferred out of
  M14; revisit based on user feedback.

## Cross-cutting invariants (asserted in tests, not just code)

- **Byte-identical determinism** under `SOURCE_DATE_EPOCH` — tested per emitter.
- **No network** at runtime — `net.DefaultResolver`/`http.DefaultTransport`
  overridden to fail in the test binary; any attempted fetch crashes tests.
  Exception: `internal/regfetch` (M14.7) for mutation commands only.
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
