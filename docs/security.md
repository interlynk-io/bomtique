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

bomtique makes **no network requests** during normal operation. URLs
in `external_references[].url`, `pedigree.patches[].diff.url` (when
`http://` or `https://`), and `pedigree.commits[].url` are recorded
in the output SBOM verbatim and never dereferenced.

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
