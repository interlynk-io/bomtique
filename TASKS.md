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

- [ ] Hand-rolled writer targeting CycloneDX 1.7 JSON. Struct-per-field; no
      map-based emission so field ordering is stable.
- [ ] `metadata.component` ← primary; `components[]` ← reachable pool [§14.1].
- [ ] Field mapping table in §14.1 implemented end-to-end, including:
  - `license` → `licenses[]`: expression form when compound; per-license
    `{ license: { id, text? } }` entries for `texts[]` (text attachment for
    inline, read-and-embed-as-attachment for `file`).
  - `pedigree` including `patches[].diff` attachment rules [§9.2]:
    string-form `text` → `{content, contentType: "text/plain"}`; attachment-form
    preserved; local `url` → read file, base64, emit `{content, encoding: "base64"}`;
    http(s) `url` → keep as-is, no fetch.
  - `scope` omitted on primary; `tags` never serialized.
  - `dependencies[]` computed from primary + each pool component's `depends-on`.
- [ ] Self-validate emitted JSON against a bundled copy of the CycloneDX 1.7
      schema when `--validate-output` is passed (fetched once, vendored).

## M8 — Determinism (`internal/emit/cyclonedx` + `internal/jcs`)

- [ ] `bom-ref` derivation [§15.1]: explicit → purl → `pkg:generic/<pct-name>@<version>`
      using RFC 3986 §2.3 unreserved set; drop `@version` if no version;
      reject collisions.
- [ ] Sorting [§15.2]: `components[]` by bom-ref; `dependencies[].dependsOn`
      by ref; `hashes` by (algorithm, value); `externalReferences` by (type, url);
      `tags` lexicographic (not serialized but sort early so any downstream
      user sees stable values).
- [ ] `SOURCE_DATE_EPOCH` handling [§15.3]: parse; set `metadata.timestamp`
      ISO 8601 UTC second precision; UUIDv5 serial.
- [ ] JCS (RFC 8785) [§15.3] `internal/jcs`: number canonicalization (ECMA-404
      double → shortest round-trip), string escaping, key sorting, no
      whitespace. Test against the RFC 8785 test vectors.
- [ ] UUIDv5 over `component-manifest/v1/serial/<sha256-of-jcs-components>` in
      DNS namespace [§15.3]; emit `urn:uuid:<uuid>` for serialNumber.
- [ ] Determinism harness test: run twice, `diff` bytes, must be identical.

## M9 — CLI surface (`cmd/bomtique`)

- [ ] `bomtique generate [paths...]`: glob/dir arguments; writes one SBOM
      per primary to `--out <dir>` (default `./sbom/`); filename is
      `<primary-name>-<primary-version>.cdx.json` or
      `<primary-name>.cdx.json` if no version; `--format cyclonedx|spdx`;
      `--stdout` to concatenate as NDJSON for CI.
- [ ] `bomtique validate [paths...]`: structural + semantic validation only;
      exit code 0 (ok), 1 (validation error), 2 (usage).
- [ ] `bomtique manifest schema`: prints the JSON Schema draft 2020-12 [§A].
- [ ] Flags: `--max-file-size`, `--tag <t>` (per-build filter over pool tags [§6.2]),
      `--warnings-as-errors`, `--source-date-epoch <n>` (overrides env),
      `--follow-symlinks` (opt-in, documented as outside spec [§18.2]),
      `--output-validate`.
- [ ] Exit codes: 0 ok, 1 validation/semantic error, 2 usage, 3 I/O error,
      4 warnings-as-errors triggered.

## M10 — SPDX emitter (`internal/emit/spdx`)

- [ ] SPDX 2.3 JSON output per §14.2 projection table.
- [ ] DESCRIBES for primary; DEPENDS_ON between primary and reachable pool.
- [ ] License texts merged into `licenseComments` with headings.
- [ ] `externalRefs` mapping for purl/cpe/website/distribution/vcs/other.
- [ ] Dropped-field warnings: one per field class (scope, variants,
      descendants, lifecycles). Counter so we only warn once per class per run.
- [ ] Post-emit JSON Schema validation (vendored SPDX 2.3 schema) under `--validate-output`.

## M11 — Discovery (non-normative, documented)

- [ ] `bomtique generate` without path arguments discovers manifests by
      walking the CWD, globbing `**/.primary.json`, `**/.components.json`,
      `**/.components.csv`. Skip `.git/`, `node_modules/`, `vendor/`,
      `.venv/`, any dir starting with `.` by default.
- [ ] Deterministic traversal order (sorted entries per directory).
- [ ] Files without a schema marker are silently ignored [§12.5].
- [ ] Document discovery semantics in `docs/discovery.md` per §12.5 SHOULD.

## M12 — Conformance test suite

- [ ] `testdata/conformance/` directory of fixture processing sets:
  - B.1–B.8 from the spec appendix.
  - multi-primary shared pool (B.3 expanded).
  - vendored + patched + directory hash (B.5 expanded).
  - CSV components manifest (B.8).
  - path-traversal attempt → rejection.
  - symlink in manifest path → rejection.
  - oversize file → rejection.
  - unknown schema version → rejection.
  - invalid purl, invalid SPDX expression, enum violations → rejection.
  - identity collisions across pool files → first-wins + warning.
  - unreachable pool components → omit + warn.
  - SOURCE_DATE_EPOCH set → byte-identical output across two runs.
- [ ] Each positive fixture has a golden `*.cdx.json` (and optionally
      `*.spdx.json`); tests diff JCS-canonicalized bytes.
- [ ] Each negative fixture has an `expect-errors.txt` listing the substring
      matches that MUST appear on stderr; tests assert presence.
- [ ] Fuzz targets: JSON parser, CSV parser, purl canonicalizer, path
      resolver, directory-walk manifest builder.

## M13 — Packaging, docs, release

- [ ] `docs/`: `usage.md`, `discovery.md`, `determinism.md`, `security.md`,
      `compatibility.md` (which spec MUSTs we enforce vs SHOULD).
- [ ] `goreleaser` config producing static linux/amd64, linux/arm64,
      darwin/arm64, windows/amd64 binaries; checksums; cosign signatures.
- [ ] Dockerfile (distroless static base).
- [ ] Dogfooding: `bomtique generate` on its own repo → commit the emitted
      `bomtique.cdx.json` to the release artifacts.
- [ ] `CHANGELOG.md` v0.1.0 entry enumerating conformance coverage.
- [ ] Tag `v0.1.0` once conformance suite is green on all CI platforms.

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
