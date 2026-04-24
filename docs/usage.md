# Usage

`bomtique` reads Component Manifest v1 files and emits one SBOM per
primary manifest. Three subcommands cover the surface: `generate`,
`validate`, `manifest schema`.

## `bomtique generate [paths...]`

Parses the supplied manifests (file paths, globs, or directories — see
[discovery.md](discovery.md)), runs the full validator, builds the
shared components pool, resolves per-primary reachability, and emits
one SBOM per primary.

```
bomtique generate                              # walk CWD for discoverable manifests
bomtique generate ./my-service                 # walk a specific dir
bomtique generate .primary.json .components.json
bomtique generate "pkg-manifests/*.json"
```

### Flags

- `--out <dir>` (default `./sbom`) — output directory. Per-primary
  filenames: `<name>-<version>.cdx.json` for CycloneDX,
  `<name>-<version>.spdx.json` for SPDX; missing version drops the
  hyphen (`<name>.cdx.json`).
- `--stdout` — write NDJSON (one compact JSON line per primary) to
  stdout instead of files.
- `--format cyclonedx|spdx` (default `cyclonedx`).
- `--source-date-epoch <n>` — epoch seconds. When set, the output
  carries a deterministic ISO 8601 UTC-second `metadata.timestamp` (CDX)
  or `creationInfo.created` (SPDX), and a deterministic UUIDv5 serial
  / documentNamespace derived from the JCS-canonicalised components.
  Without this flag (and without `SOURCE_DATE_EPOCH` in the env), the
  emitter omits the timestamp and uses a non-deterministic UUIDv4 for
  SPDX documentNamespace.
- `--max-file-size <bytes>` (default 10 MiB, per spec §8) — per-read
  cap for license texts, hashed files, and patch diffs.
- `--tag <t>` (repeatable) — filter pool components whose `tags`
  include any listed value (§6.2). Applied before reachability, so
  filtered-out pool components look unreachable.
- `--warnings-as-errors` — exit with code 4 if any `diag.Warn` line
  fires during the run.
- `--output-validate` — validate every emitted document against the
  vendored CycloneDX 1.7 or SPDX 2.3 schema. Bundled schemas are
  embedded in the binary (no network access), so a schema failure
  aborts the run with exit code 1.
- `--follow-symlinks` — accepted for forward compatibility; currently a
  no-op with a one-line warning. Symbolic links are still refused.

### Exit codes

| Code | Meaning |
|------|---------|
| 0    | Success. |
| 1    | Validation / semantic error in a manifest. |
| 2    | CLI usage error (unknown flag, invalid format). |
| 3    | I/O error (missing file, read/write failure). |
| 4    | `--warnings-as-errors` triggered. |

## `bomtique validate [paths...]`

Runs structural + semantic validation without emitting any SBOM. Takes
the same path / discovery semantics as `generate` and honours
`--max-file-size`, `--tag`, and `--warnings-as-errors`.

```
bomtique validate                    # validate everything discoverable in CWD
bomtique validate ./team-a ./team-b  # two directory scopes
```

## `bomtique manifest schema`

Prints the JSON Schema (draft 2020-12) for Component Manifest v1 to
stdout. The current document is a placeholder — it validates the two
top-level schema markers and leaves field-level checks to the Go
validator. The canonical schema referenced by spec Appendix A is still
being authored.

```
bomtique manifest schema | jq .
```

## Environment variables

- `SOURCE_DATE_EPOCH` — seconds since Unix epoch. Overridden by
  `--source-date-epoch` when both are set. Drives deterministic
  timestamps and serial numbers as described in §15.3.
