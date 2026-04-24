# Security

bomtique treats manifest files as **untrusted input**, matching spec
§18's threat model. The trust boundary is the manifest file itself
plus the files it references by relative path. This document lists
the mechanisms, where they live, and which invariants they maintain.

## Path resolution (§4.3, §18.1)

`internal/safefs.ResolveRelative(manifestDir, relPath)` is the single
entry point for every manifest-referenced path. It rejects:

- Empty paths.
- Paths containing a NUL byte.
- POSIX absolute paths (`/etc/passwd`).
- Windows UNC paths (`\\server\share`, `//server/share`).
- Windows drive-letter paths (`C:\foo`, `C:/foo`, `C:foo`).
- Any path that, after lexical cleaning, escapes `manifestDir` via
  `..` — there is no opt-in toggle to permit this.

Both `manifestDir` and `relPath` are NFC-normalised (§4.6) before join.

## Symbolic links (§18.2)

`internal/safefs.CheckNoSymlinks(manifestDir, absPath)` walks every
path component from `manifestDir` outward with `os.Lstat` and refuses
any segment that is a symbolic link — file or directory. `safefs.Open`
chains resolve → no-symlink → regular-file check → `os.Open`. Discovery
(M11) applies the same rule: symlink entries encountered during the
walk are skipped regardless of target type.

The `--follow-symlinks` flag is accepted by `bomtique scan` /
`validate` for forward compatibility but is not yet wired; symlinks
are refused unconditionally today.

## File-size cap (§8)

Every file read flows through a `safefs.CappedReader`-equivalent that
returns `ErrFileTooLarge` on overrun rather than silently truncating.
Default is 10 MiB, configurable via `--max-file-size`. The cap applies
to:

- License text files (`license.texts[].file`).
- Hashed file targets (`hashes[].file`).
- Path targets resolving to regular files (`hashes[].path` under §8.3's
  fallback).
- Every regular file encountered during a directory-hash walk (§8.4).
- Patch diff files (`pedigree.patches[].diff.url` when local).

## Network refusal (§18.3)

`bomtique scan`, `bomtique validate`, and the SBOM emitters make
**no network requests**. URLs in `external_references[].url`,
`pedigree.patches[].diff.url` (when `http://` or `https://`), and
`pedigree.commits[].url` are recorded in the output SBOM verbatim
and never dereferenced.

The only network surface in the binary is the `internal/regfetch`
package, used exclusively by `bomtique manifest add` and
`bomtique manifest update` under `--online` (opt-in on update,
default-on-match on add). A consumer-path lint test
(`TestNoNetworkImportsOutsideRegfetch`) walks every production `.go`
file and fails if `net/http`, `net.Dial`, or `net.DefaultResolver`
is referenced outside `cmd/bomtique/` and `internal/regfetch/`.

### Importer network model

Each importer speaks to one well-known JSON endpoint per registry.
No tarballs, no clones, no archive extraction.

| Importer | Endpoints | Auth |
|----------|-----------|------|
| GitHub | `api.github.com/repos/{o}/{r}`, `.../git/ref/tags/{tag}` | `GITHUB_TOKEN` → `Authorization: Bearer` |
| GitLab | `gitlab.com/api/v4/projects/{encoded}`, `.../repository/tags/{tag}` | `GITLAB_TOKEN` → `PRIVATE-TOKEN` |
| npm | `registry.npmjs.org/{name}` (abbreviated metadata) or `/{name}/{version}` | — |
| PyPI | `pypi.org/pypi/{name}/json` or `.../{name}/{version}/json` | — |
| crates.io | `crates.io/api/v1/crates/{name}`, `.../{version}` | — |

The shared `regfetch.Client` enforces:

- 30 s total request timeout.
- 1 MiB response-body cap via `io.LimitReader` (`ErrResponseTooLarge`
  on overrun). Larger-than-cap responses indicate an unexpected
  registry shape; the code path rejects rather than truncating.
- `Accept: application/json` on every request.
- `User-Agent: bomtique/<version> (+https://github.com/interlynk-io/bomtique)`
  — satisfies crates.io ToS (which requires a contact URL) and
  identifies the client in registry access logs.
- No retries. A transient failure surfaces as `ErrNetwork`; the
  caller re-runs the command.

### Host allowlist

Each importer's base URL is fixed to its registry host by default.
Overrides via `BOMTIQUE_{GITHUB,GITLAB,NPM,PYPI,CARGO}_BASE_URL`
env vars exist for tests, mirrors, air-gapped deployments, and
self-hosted GitLab. The overrides are evaluated only when the
corresponding importer already matches the input ref, so an env
var cannot redirect, say, a pkg:github ref to a non-GitHub host.

### Token handling

- Tokens are read from the environment at Fetch time, not stored in
  the Client struct. Verbose output scrubs them; error messages
  never format request headers. Dedicated per-importer tests
  (`Test*_FetchTokenNotLeakedInErrors`) pin that invariant.
- The Client carries no credential material; a fresh `NewClient()`
  is safe to log.

### Opt-out: `--offline`

Users who need absolute network silence pass `--offline` to
`manifest add` / `manifest update`. The code path that would touch
`regfetch` is bypassed entirely; no DNS lookup, no TCP connect.
`--offline` and `--online` are mutually exclusive.

## Hash algorithm allowlist (§8.1, §18.5)

`internal/hash.Parse` accepts only SHA-256, SHA-384, SHA-512,
SHA-3-256, and SHA-3-512 — spelled exactly as §8.1 requires. MD5,
SHA-1, and lowercase / CycloneDX-form variants are rejected with
`ErrUnsupportedAlgorithm`. The allowlist applies uniformly to literal,
file, and directory hash forms.

## Manifest trust (§18.4)

No field in a manifest directs the consumer to execute code, spawn
processes, or modify filesystem state outside the `--out` directory.
The emitter writes only to `--out` (or stdout). The reader only reads
files referenced by the manifest, under the path / symlink / size
rules above.

## What bomtique does NOT protect against

- An attacker with filesystem write access to the host running
  bomtique. Per spec §18, "at that point all bets are off, and no
  spec-level mechanism can restore the invariants."
- A manifest that is valid but semantically wrong — e.g. a component
  claiming the wrong license. The consumer passes the manifest's
  declarations through; it does not audit them.
- Timing side channels or resource-exhaustion attacks beyond the
  per-file size cap. Running bomtique against an adversarial input
  in a sandbox (cgroup limit, timeout) is recommended for fully
  untrusted pipelines.
