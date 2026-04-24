# Spec compatibility

This document enumerates which Component Manifest v1 MUSTs and SHOULDs
bomtique enforces as of v0.1.0. For each clause we cite the spec
section, indicate enforcement status, and name the code path.

## MUSTs enforced

| Clause | Rule | Where |
|--------|------|-------|
| §4.2   | UTF-8 encoding; invalid sequences rejected | `internal/manifest.ParseJSON`, `ParseCSV` |
| §4.3   | Relative paths only; no `..` escape; no absolute / UNC / drive-letter | `internal/safefs.ResolveRelative` |
| §4.4   | Schema marker matches family+version; unknown versions in known families rejected | `internal/manifest.classifySchemaMarker` |
| §4.5   | CSV BOM strip, CRLF/LF acceptance, fixed column header | `internal/manifest.ParseCSV` |
| §4.5   | `hash_value` XOR `hash_file` per row | `internal/manifest.componentFromCSVRow` |
| §4.6   | NFC normalisation for paths + extension filter | `internal/safefs.ToNFC`, `internal/hash.normalizeExtensions` |
| §5.1/§5.2 | Primary-manifest / components-manifest shape enforcement | `internal/manifest.ParseJSON` routing |
| §5.2   | Empty `components[]` rejected | `internal/manifest/validate.Manifest` |
| §6.1   | Name non-empty + at-least-one-of version/purl/hashes | `internal/manifest/validate.validateComponent` |
| §6.2   | Supplier.name non-empty when supplier present | ibid. |
| §6.3   | License expression required; texts[].id appears in expression; text XOR file | `internal/manifest/validate.validateLicense` |
| §6.4   | Purl valid per purl-spec | `internal/manifest/validate.validateComponent` via `internal/purl.Parse` |
| §7.1   | Component type enum | `internal/manifest/validate/enum.go` |
| §7.2   | Scope enum | ibid. |
| §7.3   | External reference type enum | ibid. |
| §7.4   | Patch type enum | ibid. |
| §7.5   | Lifecycle phase enum | ibid. |
| §8     | Exactly one hash form per entry | `internal/manifest/validate.validateHashEntry` |
| §8.1   | Permitted algorithm set; MD5/SHA-1 rejected; lowercase hex literal values | `internal/hash.Parse`, `internal/hash.ValidateLiteralValue` |
| §8.2   | File form target exists + regular file | `internal/manifest/validate.checkFilesystemFile` |
| §8.3   | Path form target exists + regular file / directory | `internal/manifest/validate.checkFilesystemPath` |
| §8.4   | Directory digest produces ≥ 1 file after hidden/symlink/extension filter | `internal/manifest/validate.validateDirectoryHasEligibleFiles`, `internal/hash.Directory` |
| §9.1   | Ancestor identity rules (name + one-of version/purl/hashes) | `internal/manifest/validate.validateAncestor` |
| §9.2   | Patch `diff.text` string / attachment forms; local `url` → base64 embed; http `url` preserved | `internal/emit/cyclonedx.buildDiff` |
| §9.3   | Patched component's purl does not canon-equal any ancestor purl | `internal/manifest/validate.checkPatchedPurl` via `internal/purl.CanonEqual` |
| §10.2  | `depends-on` parse: `pkg:` → canonical purl; else last-`@` split; whitespace/bare-name rejected | `internal/graph.ParseRef` |
| §10.3  | Unresolved depends-on → warning; edge dropped; referring component preserved | `internal/graph.TransitiveClosure` |
| §10.4  | Single-primary whole-pool convenience; single-primary closure + unreachable warnings; multi-primary requires non-empty depends-on; orphan-across-all warning once per run | `internal/graph.PerPrimary` / `ForProcessingSet` |
| §11    | Identity precedence (purl → name+version → name); four direct-pass dedup warnings; secondary mixed purl/no-purl merge; primary-vs-pool distinctness | `internal/pool` |
| §12.1  | Processing set must contain ≥ 1 primary | `internal/manifest/validate.ProcessingSet` |
| §12.2  | Pool concatenation in input order, dedup, then emit | `internal/pool.Build` |
| §12.4  | Per-manifest directory scope for path resolution | `cmd/bomtique/pipeline.go` provenance index |
| §13.1/§13.2 | Structural + semantic validation surface | `internal/manifest/validate` |
| §13.3  | Warning channel = stderr with `warning:` prefix | `internal/diag.Warn` |
| §14.1  | CycloneDX 1.7 field mapping incl. license, hashes, pedigree, scope (pool only), tags (never serialized) | `internal/emit/cyclonedx` |
| §14.2  | SPDX 2.3 field projection incl. dropped-field warnings (one per class per run) | `internal/emit/spdx` |
| §15.1  | bom-ref precedence (explicit → purl → pkg:generic fallback); collision rejection | `internal/emit/cyclonedx.assignBOMRefs` |
| §15.2  | Array ordering rules | `internal/emit/cyclonedx.sortBOM` |
| §15.3  | SOURCE_DATE_EPOCH-driven timestamp + UUIDv5 serial derivation | `internal/emit/cyclonedx.computeDeterministicSerial` |
| §15.4  | File-based hashes computed at generation time | `internal/hash.File`, `internal/hash.Directory` |
| §18.1  | Path-traversal refusal | `internal/safefs.ResolveRelative` |
| §18.2  | Symbolic-link refusal at file and directory layers | `internal/safefs.CheckNoSymlinks`, discovery, directory-walk |
| §18.3  | No network fetches | grep-able: the code never calls `net/http` or `net.Dial` |
| §18.5  | Hash algorithm allowlist | `internal/hash.Parse` |

## SHOULDs surfaced

| Clause | Rule | Treatment |
|--------|------|-----------|
| §8     | Default 10 MiB cap | default `--max-file-size`; overridable |
| §12.5  | Discovery SHOULD be documented and deterministic | [discovery.md](discovery.md) + lexicographic walk order |
| §15.3  | Wall-clock timestamp / UUIDv4 when SDE unset | we omit both rather than inventing non-deterministic values; consider this a stricter-than-SHOULD behaviour |

## Known deferrals

These are tracked in [TASKS.md](../TASKS.md) or the respective
milestone PRs.

- **Appendix A JSON Schema.** `bomtique manifest schema` prints a
  draft-2020-12 placeholder today; the canonical schema is still being
  authored. The Go validator is authoritative.
- **`--output-validate`.** Accepted for forward compatibility but does
  nothing yet. Vendoring the CycloneDX 1.7 and SPDX 2.3 schemas and
  wiring post-emit validation is a follow-up.
- **`--follow-symlinks` opt-in path.** Accepted but always falls back
  to refusal. The spec (§18.2) allows this as opt-in; we haven't
  implemented it yet.
- **Full SPDX expression grammar check.** §6.3 allows but does not
  require grammar validation. We do a cheap "id appears in expression"
  check; `Options.SPDXExpressionStrict` is reserved for a future
  grammar parser.
- **Directory-walk fuzz target.** Covered by unit tests in
  `internal/hash/` but not by a corpus-seeded fuzz harness yet.
