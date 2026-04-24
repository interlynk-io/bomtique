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

- [ ] `ResolveRelative(manifestDir, p)` [§4.3]: reject absolute POSIX, Windows
      drive-letter, and UNC paths; reject any post-resolution path escaping
      `manifestDir` via `..`; NFC-normalize before use [§4.6].
- [ ] Symlink-safe open [§18.2]: walk each path component from `manifestDir`
      and stat without following; reject if any component is a symlink.
      No `os.Open`-then-check; do `Lstat` per segment.
- [ ] File-size cap (default 10 MiB, configurable via `--max-file-size`) [§8].
      Reads use `io.LimitReader` and fail on EOF-before-limit overrun.
- [ ] Tests covering: `..` traversal, drive-letter, UNC, symlink in any
      component, NFC vs NFD input, oversize file, missing file.

## M3 — Hashing (`internal/hash`)

- [ ] Algorithm allowlist: SHA-256, SHA-384, SHA-512, SHA-3-256, SHA-3-512 [§8.1].
      Reject MD5, SHA-1, anything else with a named error.
- [ ] Literal form passthrough (lowercase hex) [§8.1].
- [ ] File form [§8.2]: open via `safefs`, hash, emit lowercase hex.
- [ ] Directory digest [§8.3, §8.4]: recursive walk, skip dirs starting with
      `.`, skip symlinks (dir and file), filter by lowercased `extensions`
      (strip leading `*.` or `.`), reject empty result, per-file digest,
      build `<hex><SP><SP><rel-path-with-slashes><LF>` lines sorted
      byte-wise by relative path, final digest over UTF-8 bytes of that manifest.
- [ ] Per-file size cap applied during the walk.
- [ ] Golden tests with fixtures covering: nested dirs, hidden dirs, symlinks,
      extension filter with and without leading dot, cross-platform paths
      (store fixtures with forward slashes only).

## M4 — Validation (`internal/manifest/validate`)

- [ ] Structural validator matching Appendix A's intended shape (build a Go
      schema: required fields, type predicates, enum membership).
- [ ] Semantic validator [§13.2]:
  - name non-empty; at least one of version/purl/hashes [§6.1].
  - each hash is exactly one form.
  - file/dir hashes resolve and directory produces ≥ 1 file.
  - algorithms in the permitted set.
  - path traversal and symlink rules (covered by safefs).
  - patched-purl rule [§9.3]: canonical-form purl of component differs from
    every `pedigree.ancestors[].purl`.
  - enumerations for `type`, `scope`, `external_references[].type`,
    `patches[].type`, `lifecycles[].phase`.
  - license object: `expression` required; every `texts[].id` is a simple
    SPDX id that appears as a bare identifier within `expression`; exactly
    one of `text` or `file` per texts entry; optional SPDX-expression grammar check.
  - multi-primary: every primary has non-empty `depends-on` [§10.4].
  - processing-set is non-empty of primaries [§12.1].
  - at least one components manifest is allowed to be absent.
- [ ] Error surface carries: manifest path, JSON pointer (when JSON), CSV
      row/column (when CSV), offending value. No panics in validation.

## M5 — Pool construction, identity, dedup (`internal/pool`)

- [ ] Canonical-form purl comparison [§9.3]: already provided by
      `internal/purl.CanonEqual(a, b string) (bool, error)`. M5 just wires
      it into identity and dedup code paths.
- [ ] Identity extraction [§11]: precedence purl → (name, version) → name.
- [ ] Primary vs pool distinctness check within a single emission [§11].
- [ ] Pool dedup passes [§11]:
  - direct identity pass (four warning cases with first-occurrence keep).
  - secondary mixed purl / no-purl pass: build `(name,version) → purl-bearing`
    index; merge no-purl matches; keep purl-bearing values on conflict,
    warn per conflicting field.
- [ ] Each warning goes through `internal/diag` with `warning:` stderr prefix [§13.3].

## M6 — Dependency resolution and reachability (`internal/graph`)

- [ ] `depends-on` parsing [§10.2]: `pkg:` → canonical purl match; else
      split on last `@` into `(name, version)`; reject whitespace-bearing or
      unprefixed-without-`@` strings.
- [ ] Candidate lookup against the deduped pool.
- [ ] Transitive closure from a set of roots; warn on unresolved edges and
      drop the edge (preserve the referring component) [§10.3].
- [ ] Reachability rules [§10.4]:
  - single-primary + empty depends-on → whole pool becomes direct deps.
  - single-primary + non-empty → closure only; warn per unreachable.
  - multi-primary → require non-empty depends-on on each; per-primary closure.
  - orphan-across-all warning emitted once per run [§10.4 last bullet].
- [ ] Cycle tolerance: cycles are legal in dependency graphs; closure uses a
      visited set.

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
