# Changelog

All notable changes to this project are documented here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Changed (breaking)

- `bomtique manifest add`: removed `--online` and `--offline` flags;
  added `--ref <purl-or-url>`. A registry fetch happens when (and
  only when) `--ref` is supplied and matches a registered importer
  (github, gitlab, npm, pypi, crates.io); URL form is now first-class
  for every importer (e.g.
  `--ref https://github.com/libressl/portable/releases/tag/v3.9.0`).
  When `--ref` is omitted, no fetch happens. When `--ref` is supplied
  but no importer matches, `add` errors with `ErrUnsupportedRef`.
  `--purl` no longer drives the importer match — it's purely the
  literal purl recorded on the component. Migration: rename `--online
  --purl pkg:npm/foo@1` to `--ref pkg:npm/foo@1`; drop `--offline`
  (no fetch is now the default).
- `bomtique manifest update`: removed `--online` and `--offline`
  flags; added `--refresh`. Use `--refresh` to refetch metadata from
  the importer matching the target component's existing purl.
  Migration: rename `--online` to `--refresh`; drop `--offline`.
- `--name` no longer doubles as a URL/ref carrier. URL inputs go
  through `--ref` exclusively; passing a URL via `--name` now stores
  it verbatim as the component's name field.
- New `BOMTIQUE_OFFLINE=1` env var: hard kill switch that validates
  `--ref` / `--refresh` purls against the importer registry but skips
  the HTTP call. Useful for air-gapped CI driving `add`/`update` from
  scripted ref values.

### Added (v0.2.0 preview — M14 hand-authored mutation surface)

**Mutation engine (M14.0).**
- `internal/manifest/mutate` package with the parse-edit-rewrite
  primitives: `WriteJSON` / `WriteCSV` with Unknown-key
  preservation (sorted + `json.Compact`-canonicalised so values
  survive `json.Indent` reflow), `LocatePrimary`,
  `LocateOrCreateComponents`, `MergeComponent`, `CheckFitsCSV`.

**`bomtique manifest init`** — scaffolds `.primary.json` atomically
(tmp + rename), preserves Unknown fields on `--force`. Does not
create `.components.json`; the first `add` does.

**`bomtique manifest add`** — flag-driven plus `--from file|-`
plus registry auto-fetch. Pool or primary depends-on targets.
`--vendored-at <dir>` synthesises a §9.3 repo-local purl, a §8.3
directory hash directive (digest computed at scan time, not add
time, per §15.4), and a `pedigree.ancestors[0]` from `--upstream-*`
flags.

**`bomtique manifest remove <ref>`** — drops a component and
scrubs `depends-on` edges across the pool and primary with
`diag.Warn` per scrubbed edge. `--dry-run`, `--primary`, `--into`.

**`bomtique manifest update <ref>`** — field replace, `--to
<version>` with lockstep purl update, ten `--clear-*` flags,
`pedigree.patches` preserved by default, optional `--online`
registry refresh.

**`bomtique manifest patch <ref> <diff-path>`** — §7.4 patch
registration under `pedigree.patches[]` with `--resolves
"key=value,..."` (repeatable), `--notes` append / `--replace-notes`.
Diff path validated against §4.3; diff content read by scan, not
add.

**Registry importer framework (`internal/regfetch`, M14.7).**
- `Importer` interface, process-global `Registry`, shared `Client`
  with 30 s timeout and 1 MiB body cap.
- Sentinels: `ErrNetwork`, `ErrNotFound`, `ErrRateLimited`,
  `ErrUnsupportedRef`, `ErrResponseTooLarge`, `ErrOffline`.
- `--offline` / `--online` flags on `manifest add` and
  `manifest update`. Mutually exclusive.
- Consumer-path network invariant enforced by
  `TestNoNetworkImportsOutsideRegfetch`.

**Registry importers.**
- GitHub (M14.8): URL + `pkg:github`, tag confirmation, default-
  branch fallback, `GITHUB_TOKEN` auth, license SPDX ID, nested-
  purl rejection. Live-smoke verified against
  `pkg:github/google/uuid@v1.6.0`.
- GitLab (M14.9): URL + `pkg:gitlab` including nested namespaces,
  URL-encoded project path, `/-/tree|tags|commits/` delimiter
  parsing, self-hosted via env, `GITLAB_TOKEN` `PRIVATE-TOKEN`,
  license key → SPDX mapping. Live-smoke on
  `pkg:gitlab/gitlab-org/cli@v1.47.0`.
- npm (M14.10): URL + `pkg:npm` + `npm:` shorthands with scoped
  `@scope/name` support. Two-endpoint split (`/<name>/<version>`
  per-version doc, abbreviated metadata for latest) keeps even
  `@types/node` under the 1 MiB cap. SRI integrity decoded to
  SHA-512 literal hash. Live-smoke on `express@4.18.2` AND
  `@types/node@20.10.0`.
- PyPI (M14.11): URL + `pkg:pypi` + `pypi:` with PEP 503 name
  normalisation; license precedence through PEP 639
  `license_expression`, free-text mapping, SPDX-ID shape check,
  and `License :: OSI Approved :: …` classifier table; sdist
  SHA-256 over first wheel. Live-smoke on
  `pkg:pypi/requests@2.31.0`.
- Cargo (M14.12): URL + `pkg:cargo`, two-GET flow for crate +
  version, SPDX expression passthrough, per-version SHA-256
  checksum, UA satisfies crates.io ToS (tested). Live-smoke on
  `pkg:cargo/serde@1.0.193`.

**Docs.**
- `docs/usage.md` expanded with every mutation command, importer
  matrix, and environment-variable catalogue.
- `docs/security.md` gains an "importer network model" section
  (host allowlist, response cap, token handling, `--offline`).
- `docs/discovery.md` notes the mutation walk semantics.

## v0.1.0 — 2026-04-24

First tagged release. `bomtique` is now a working reference consumer
for Component Manifest v1: it parses, validates, pool-builds,
resolves reachability, and emits both CycloneDX 1.7 and SPDX 2.3
SBOMs under a deterministic `SOURCE_DATE_EPOCH` regime.

### Added

**Manifest layer (M1).**
- Go type surface for every Component Manifest v1 object.
- JSON parser with strict UTF-8, duplicate-key rejection via a token
  pre-pass, and unknown-field sidecar maps per spec §5.1, §5.2, §6.2.
- CSV parser covering BOM strip, CRLF/LF, the fixed 15-column header
  per §4.5, `hash_value` XOR `hash_file` enforcement, and RFC 4180
  quoting for `depends_on` / `tags`.
- Schema-marker classification with family-aware rejection of
  unknown versions (§4.4).
- Appendix B.1–B.8 fixtures + round-trip tests.

**Filesystem + path layer (M2).**
- `internal/safefs.ResolveRelative` rejects POSIX-absolute, UNC,
  Windows drive-letter, and traversal paths cross-platform, NFC-
  normalises per §4.6.
- Symlink-safe open via segment-by-segment `Lstat`; streaming size
  cap (`DefaultMaxFileSize = 10 MiB`) with `ErrFileTooLarge`.

**Hashing (M3).**
- Algorithm allowlist (SHA-256 / SHA-384 / SHA-512 / SHA-3-256 /
  SHA-3-512) via stdlib `crypto/sha3`. MD5, SHA-1, and case-varianted
  names rejected.
- Literal / file / directory forms per §8.1–§8.4 with a spec-literal
  reference implementation in tests so the impl is measured against
  the spec rather than itself.

**Validation (M4).**
- `internal/manifest/validate` enforcing every §13.2 rule: name +
  identity (§6.1), enums (§7), license expression + texts (§6.3),
  hash forms + algorithm + literal hex (§8), filesystem resolvability
  (§8.2 / §8.3 / §8.4), patched-purl rule (§9.3) via `purl.CanonEqual`,
  multi-primary depends-on (§10.4), at-least-one-primary (§12.1).
- Error type with JSON pointer (JSON) or row + CSV column (CSV),
  `Kind` classifier, offending value, and spec-section references.

**Pool (M5).**
- Identity precedence (purl → name+version → name) with canonical purl
  storage. Four direct-pass dedup warnings + secondary mixed purl /
  no-purl merge per §11. Primary-vs-pool distinctness check.

**Graph (M6).**
- `graph.ParseRef` for §10.2 depends-on entries. `PoolIndex` lookup,
  `TransitiveClosure` BFS with cycle tolerance, `PerPrimary` +
  `ForProcessingSet` for the §10.4 reachability rules and the
  once-per-run orphan-across-all warning.

**CycloneDX emitter (M7).**
- CycloneDX 1.7 JSON with struct-per-field types, §15.1 bom-ref
  derivation (explicit → purl → `pkg:generic/<pct-name>@<version>`),
  §14.1 field mapping end-to-end including license expression + texts
  (file-backed → base64 attachment), hashes with algorithm-name
  translation, pedigree + patch diff per §9.2.

**Determinism (M8).**
- `internal/jcs` — RFC 8785 JSON Canonicalisation including ECMA-262
  number formatting (no leading-zero exponent, mandatory sign, `-0`
  collapse), UTF-16-code-unit key sort, minimal string escapes.
- §15.2 array sorting applied before `json.Marshal`.
- `SOURCE_DATE_EPOCH` drives ISO 8601 UTC-second `metadata.timestamp`
  and UUIDv5 `serialNumber` derived from JCS-canonicalised
  `components[]`.
- Determinism harness asserts byte-identical output across two runs.

**CLI (M9).**
- `bomtique generate [paths...]` — full pipeline with `--out`,
  `--stdout` (NDJSON), `--format cyclonedx|spdx`, `--source-date-epoch`,
  `--max-file-size`, `--tag`, `--warnings-as-errors`.
- `bomtique validate [paths...]` — validation only.
- `bomtique manifest schema` — JSON Schema draft-2020-12 placeholder.
- Exit codes: `0` ok, `1` validation, `2` usage, `3` I/O, `4`
  warnings-as-errors.

**SPDX emitter (M10).**
- SPDX 2.3 JSON with SPDXRef- ID sanitisation + collision safety,
  §14.2 field projection including license-texts → `licenseComments`,
  externalRefs → `PACKAGE-MANAGER` / `SECURITY` / `OTHER`, pedigree
  into `sourceInfo` / `comment` / `annotations`. Dropped-field
  warnings fire once per class per run (scope, variants, descendants,
  lifecycles).
- Deterministic `documentNamespace` via JCS + SHA-256 + UUIDv5 when
  `SOURCE_DATE_EPOCH` is set.

**Discovery (M11).**
- Zero-arg and directory-arg invocations walk the target for basenames
  `.primary.json` / `.components.json` / `.components.csv`, skipping
  `.`-prefixed dirs and the hardcoded `.git` / `node_modules` /
  `vendor` / `.venv` set. Symlinks refused at the discovery layer too.
- `docs/discovery.md` documents the convention per §12.5 SHOULD.

**Conformance (M12).**
- `cmd/bomtique/testdata/conformance/` split into `positive/` (byte-
  compared against `golden/`) and `negative/` (stderr-substring
  matching). Initial 4 positive + 7 negative fixtures.
- Fuzz targets with seed corpora for JSON / CSV parsers, purl, and
  `safefs.ResolveRelative`.

**Packaging + docs (M13).**
- `docs/usage.md`, `docs/determinism.md`, `docs/security.md`,
  `docs/compatibility.md` describing the shipped behaviour.
- `Dockerfile` on `gcr.io/distroless/static-debian12:nonroot`.
- `.goreleaser.yaml` producing `linux/amd64`, `linux/arm64`,
  `darwin/arm64`, `windows/amd64` archives with SHA-256 checksums and
  cosign signing hook.
- Dogfood: `.primary.json` + `.components.json` describing bomtique
  itself; generated `sbom/bomtique-0.1.0.cdx.json` committed as the
  release artefact.

### Deferred

Tracked in [TASKS.md](TASKS.md) for a follow-up:

- Canonical JSON Schema draft 2020-12 for Appendix A (`bomtique manifest
  schema` prints a placeholder today).
- `--output-validate` post-emit schema validation (accepts the flag,
  no-op today).
- `--follow-symlinks` opt-in path (accepted, still refuses).
- Full SPDX License Expression grammar validation
  (`Options.SPDXExpressionStrict`).
- Directory-walk fuzz corpus.

## Pre-v0.1.0

### Added
- Component Manifest v1 specification under `spec/`.
- Go module `github.com/interlynk-io/bomtique` targeting Go 1.26.
- `internal/purl` — Package-URL parser forked from
  `interlynk-io/lynkctl/pkg/purl`, passing the full
  package-url/purl-spec conformance suite.
- Package skeletons + stubs for every M1–M10 target.
- Makefile with `build`, `test`, `test-race`, `vet`, `fmt`,
  `fmt-check`, `lint`, `cover`, `fuzz`, `tidy`, `tools`, `ci`, `clean`.
- GitHub Actions CI covering Linux / macOS / Windows on Go 1.26 with
  race detector, coverage, staticcheck, and golangci-lint.
- `CONTRIBUTING.md`, `SECURITY.md`, this `CHANGELOG.md`.
- SPDX short-form license headers on every Go source.

### Changed
- Aligned spec §13.2 with §8.1 so the permitted hash-algorithm set is
  stated in one normative place.
