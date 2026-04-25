# Usage

`bomtique` reads Component Manifest v1 files and emits one SBOM per
primary manifest. Three commands ship today:

- `scan` / `validate` — consume existing manifests, read-only.
- `manifest {schema,init,add,remove,update,patch}` — scaffold and
  edit the manifest files.

## `bomtique scan [paths...]`

Parses the supplied manifests (file paths, globs, or directories — see
[discovery.md](discovery.md)), runs the full validator, builds the
shared components pool, resolves per-primary reachability, and emits
one SBOM per primary.

```
bomtique scan                              # walk CWD for discoverable manifests
bomtique scan ./my-service                 # walk a specific dir
bomtique scan .primary.json .components.json
bomtique scan "pkg-manifests/*.json"
```

### Flags

- Default output: NDJSON on stdout (one compact JSON per primary).
  Suitable for piping to another tool or redirecting with `>`.
- `--out <dir>` — write per-primary files into `<dir>` instead.
  Filenames: `<name>-<version>.cdx.json` for CycloneDX,
  `<name>-<version>.spdx.json` for SPDX; missing version drops the
  hyphen (`<name>.cdx.json`).
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
- `--verbose` — log each manifest file as it's parsed, plus each file
  silently skipped for lacking a schema marker (§12.5).
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
the same path / discovery semantics as `scan` and honours
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

## `bomtique manifest init`

Scaffolds a fresh `.primary.json` in the current directory. Refuses
to overwrite an existing file without `--force`. Does NOT create a
`.components.json` (§5.2 forbids an empty `components[]`; the first
`manifest add` creates it on demand).

```
bomtique manifest init \
  --name acme-app --version 1.0.0 \
  --license Apache-2.0 \
  --purl pkg:github/acme/app@1.0.0 \
  --supplier "Acme Corp" --website https://acme.example
```

Flags: `--name` (required), `--version`, `--type` (default
`application`), `--license`, `--purl`, `--cpe`, `--description`,
`--supplier`/`--supplier-email`/`--supplier-url`,
`--website`/`--vcs`/`--distribution`/`--issue-tracker`,
`-C`/`--chdir <dir>`, `--force`.

`--force` preserves unknown top-level keys and unknown fields on the
primary component across re-init.

## `bomtique manifest add`

Adds a component to the pool (default) or appends a ref to the
primary's `depends-on` list (`--primary`). The target components
manifest is auto-located (nearest `.components.json` walking up from
CWD; else created alongside the primary). Override with
`--into <path>`.

```
# Flag-driven
bomtique manifest add \
  --name libx --version 1.0 --license MIT \
  --purl pkg:generic/acme/libx@1.0

# Read a Component from a JSON file
bomtique manifest add --from ./incoming/libx.json

# Read from stdin (tee from another tool)
cat libx.json | bomtique manifest add --from -

# Fetch metadata from a registry via --ref. Accepts both purl and
# URL forms for github, gitlab, npm, pypi, and crates.io.
bomtique manifest add --ref pkg:npm/express@4.18.2
bomtique manifest add --ref https://www.npmjs.com/package/express/v/4.18.2

# Record a repo-local vendored component (§9.3) with a directory
# hash directive (digest computed at scan time) and an upstream
# ancestor whose metadata is fetched from GitHub.
bomtique manifest add \
  --name vendor-libx --version 2.4.0 \
  --vendored-at ./src/vendor-libx --ext c,h \
  --upstream-ref https://github.com/upstream-org/libx/releases/tag/v2.4.0

# Append to the primary's depends-on instead of the pool
bomtique manifest add --primary \
  --name libx --version 1.0 --purl pkg:generic/acme/libx@1.0
```

Component-field flags mirror `init`. Pool-only flags:
`--scope required|optional|excluded`, repeatable `--depends-on <ref>`
and `--tag <t>`, and repeatable `--external type=url` for arbitrary
external references.

Identity collisions against the existing pool (§11) are rejected
hard.

CSV target files reject components that can't be represented in CSV
(external references, structured license texts, directory-form
hashes, pedigree, lifecycles). The error message tells you to rerun
with `--into <json-path>`.

### Registry importers

`--ref <purl-or-url>` triggers a registry fetch. The ref must match
one of the registered importers below; otherwise `add` fails with
`ErrUnsupportedRef`. Without `--ref`, no fetch happens and the
component is built purely from flags.

| Ref shape | Importer | Auth env var | Fields lifted |
|-----------|----------|--------------|---------------|
| `https://github.com/o/r[/tree\|releases/tag/ref]`, `pkg:github/o/r[@ref]` | GitHub | `GITHUB_TOKEN` | name, version (ref or default branch), license (SPDX ID), description, homepage, html_url+issues |
| `https://gitlab.com/.../proj[/-/tree\|tags/ref]`, `pkg:gitlab/.../proj[@ref]` | GitLab | `GITLAB_TOKEN` | name, version, license (mapped), description, web_url, repo URL, issues |
| `https://www.npmjs.com/package/<name>[/v/<version>]`, `npm:<name>[@<ver>]`, `pkg:npm/<name>[@<ver>]` | npm | — | name, version, license (SPDX-shape check), description, author (supplier), repository, bugs, SHA-512 integrity |
| `https://pypi.org/project/<name>[/<version>]`, `pypi:<name>[@<ver>]`, `pkg:pypi/<name>[@<ver>]` | PyPI | — | name (PEP 503), version, license (mapping + classifiers), summary, author, project_urls, sdist SHA-256 |
| `https://crates.io/crates/<name>[/<version>]`, `pkg:cargo/<name>[@<ver>]` | crates.io | — | name, version, license (SPDX passthrough), description, homepage, repository, documentation, SHA-256 checksum |

Non-defaults:

- Self-hosted GitLab: `BOMTIQUE_GITLAB_BASE_URL=<host>` env var.
- Mirror overrides (tests / air-gapped): `BOMTIQUE_GITHUB_BASE_URL`,
  `BOMTIQUE_NPM_BASE_URL`, `BOMTIQUE_PYPI_BASE_URL`,
  `BOMTIQUE_CARGO_BASE_URL`.
- `BOMTIQUE_OFFLINE=1`: validates `--ref` against the importer set
  but skips the HTTP call. Useful for air-gapped CI that drives
  `add`/`update` from scripted `--ref` values.

Network policy: one HTTPS GET per importer call (two for GitHub
tag-confirmation and for Cargo), 30 s total timeout, 1 MiB response
cap enforced client-side via `io.LimitReader`, no retries. Tokens
are sent once via the standard auth header and never appear in error
output; a dedicated test pins that behaviour per importer.

Flag values always override fields lifted from a `--from` file or an
importer response. Each override emits one `warning:` line on
stderr.

## `bomtique manifest remove <ref>`

Drops a component from the pool and scrubs any `depends-on` edges
that pointed at it. The scrub runs across every reachable components
manifest and the primary, with one stderr line per scrubbed edge.

```
bomtique manifest remove pkg:generic/acme/libx@1.0
bomtique manifest remove libx@1.0               # name@version form
bomtique manifest remove --primary pkg:generic/x@1   # scrub primary only
bomtique manifest remove --dry-run pkg:npm/foo@1     # preview, no write
```

Multi-file match is a hard error (§11 invariant) — disambiguate with
`--into <path>`.

## `bomtique manifest update <ref>`

Replaces fields on an existing pool component. Unset flags preserve
current values; `--clear-<field>` explicitly nulls an optional
field.

```
# Field replace
bomtique manifest update pkg:generic/libx@1.0 --license Apache-2.0

# Version bump with lockstep purl update
bomtique manifest update pkg:generic/libx@1.0 --to 2.0

# Null out a field without setting it to empty string
bomtique manifest update pkg:generic/libx@1.0 --clear-license

# Refresh metadata from the importer matching the existing purl,
# and re-apply flag overrides on top
bomtique manifest update pkg:npm/express@4.18.2 --refresh
```

`--to <version>` also syncs the `purl` version segment when it
matches the old version. `pedigree.patches[]` is preserved by
default; pass `--clear-pedigree-patches` to drop it. `--dry-run`
previews without writing.

## `bomtique manifest patch <ref> <diff-path>`

Registers a §9.2 pedigree patch entry on a component. bomtique does
not read or copy the diff file — scan reads it later via safefs.

```
bomtique manifest patch pkg:generic/libx@1.0 ./patches/fix-cve.patch \
  --type backport \
  --resolves "type=security,name=CVE-2024-1,url=https://example/cve/2024-1" \
  --resolves "type=defect,name=BUG-42" \
  --notes "Backported from upstream to fix CVE-2024-1"
```

Types: `unofficial | monkey | backport | cherry-pick` (§7.4).
`--resolves` takes repeatable `key=value,key=value` entries
(`type=...`, `name=...`, `id=...`, `url=...`,
`description=...`). Absolute paths and `..` traversal are rejected
per §4.3. Use `--replace-notes` to overwrite rather than append to
existing pedigree.notes.

## Environment variables

- `SOURCE_DATE_EPOCH` — seconds since Unix epoch. Overridden by
  `--source-date-epoch` when both are set. Drives deterministic
  timestamps and serial numbers as described in §15.3.
- `GITHUB_TOKEN` — optional `Authorization: Bearer` for the GitHub
  importer; raises rate-limit caps.
- `GITLAB_TOKEN` — optional `PRIVATE-TOKEN` for the GitLab
  importer.
- `BOMTIQUE_GITHUB_BASE_URL`, `BOMTIQUE_GITLAB_BASE_URL`,
  `BOMTIQUE_NPM_BASE_URL`, `BOMTIQUE_PYPI_BASE_URL`,
  `BOMTIQUE_CARGO_BASE_URL` — importer base URL overrides for
  tests, mirrors, and self-hosted instances.
