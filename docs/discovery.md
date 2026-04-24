# Discovery

Spec §12.5 reserves manifest discovery as **non-normative** and
implementation-defined. A SHOULD-level clause asks implementations to
document their semantics and produce deterministic output given a fixed
input tree. This document describes what `bomtique` does.

## When discovery runs

`bomtique generate` and `bomtique validate` both trigger discovery
in two situations:

- **Zero positional arguments**: walk the current working directory.
- **A positional argument that is a directory**: walk that directory.

Explicit file or glob arguments bypass discovery — `bomtique generate
internal/manifest/testdata/appendix/b1.json` parses just that file.

## Conventional filenames

Discovery matches files whose **basename is exactly one of**:

- `.primary.json` — primary manifest (JSON).
- `.components.json` — components manifest (JSON).
- `.components.csv` — components manifest (CSV).

The leading dot mirrors typical "hidden metadata file" naming (think
`.gitignore`, `.env`). Prefix variants like `myproject.primary.json`
are **not** matched — pass them explicitly if you prefer that naming.

## Directory exclusions

The walk refuses to descend into:

- Any directory whose name starts with `.` (covers `.git`, `.venv`,
  `.vscode`, `.cache`, and so on).
- Any directory named `node_modules`, `vendor`, `.venv`, or `testdata`.
  `testdata/` follows the Go toolchain convention: it holds test
  fixtures — often *deliberately* malformed ones — which must not
  poison a legitimate `bomtique` invocation.

This is deliberately conservative. You rarely want to mine manifests
out of build caches, package-manager caches, committed third-party
source trees, or test-fixture directories; when you do, pass the
specific path.

## Symbolic links

The walk refuses to follow symbolic links, whether they point at a
directory or a file. This matches spec §18.2's "no symlinks by default"
rule: the discovery layer does not opt out.

## Determinism

`filepath.WalkDir` internally delegates to `os.ReadDir`, which returns
entries sorted lexicographically. Two runs against the same input tree
produce the same sequence of discovered paths — before the pool-
construction step imposes its own dedup order (§11).

## Ignoring files without a schema marker

Per spec §12.5, any file reaching the parser that carries no schema
marker is silently ignored. This applies equally to files found via
discovery and files supplied explicitly on the command line. A file
with a malformed marker (for example, `primary-manifest/v2`) is a
hard error — discovery finds the file, the parser rejects the
unsupported version, and the run fails with the specific error message.
