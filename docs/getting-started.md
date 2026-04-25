# Getting Started

This guide walks you from zero to a working CycloneDX SBOM, then layers
on the features you need as your project grows. Each section adds one
concept on top of the last. Stop reading at any point and you'll still
have something useful.

## Who this is for

bomtique is a **toolkit** for projects where automated SCA scanners
either don't fit or can't be trusted. Three audiences in particular:

- **Non-manifest-based languages.** C, C++, embedded firmware, and
  legacy native codebases. There's no `package.json` or `Cargo.toml`
  for a scanner to read, so component metadata has to come from
  somewhere else, and that somewhere should be developers rather
  than heuristics.
- **Developer control.** You declare what's in your project; the
  manifest lives next to the code; PR review covers component
  changes the same way it covers code changes. No background scan
  guessing what your code uses, no false positives to triage.
- **Determinism.** The same source tree always produces the same
  SBOM, byte-for-byte. Reproducible builds and the drift-detection
  patterns in [section 10](#10-sbom-drift-over-time) all build on
  that guarantee.

bomtique ships the file format and the consumer. CI integration and
platform tracking are yours to compose.

> **See the artifacts.** Each major section below has a matching
> snapshot directory under
> [`examples/getting-started/`](../examples/getting-started/) showing
> the manifests, vendored sources, patches, and emitted SBOMs at that
> point in the narrative. Use them as a reference while reading.

## The project

The running example is **Acme Corp's IoT gateway firmware**, a small
C codebase. The first prototype shipped as `device-firmware` 0.1.0
with one runtime dependency: OpenBSD's `libtls`, for encrypted
connections. The repo at this point looks like:

```
device-firmware/
  src/
    main.c
  libs/
    libtls/      <-- external OSS submodule (encrypted networking)
```

Each section below adds one new project requirement and one new
bomtique feature. By section 8 we cut a 1.0.0 release: the firmware
has a GUI, MQTT cloud telemetry, JSON parsing, compression, ships in
two variants, and the SBOM tracks every component.

## Contents

- [1. Quick start](#1-quick-start): three commands to a working SBOM.
- [2. What just happened](#2-what-just-happened): read the files you
  just created.
- [3. More dependencies, less typing](#3-more-dependencies-less-typing):
  registry imports, dependency edges, scopes.
- [4. Validate before you ship](#4-validate-before-you-ship): catch
  problems with `validate` and `--output-validate`.
- [5. Splitting the pool across the tree](#5-splitting-the-pool-across-the-tree):
  multiple `.components.json` files merged automatically.
- [6. Vendored and patched code](#6-vendored-and-patched-code):
  `--vendored-at`, directory hashes, pedigree patches.
- [7. Per-build variants](#7-per-build-variants): same source tree,
  different SBOMs.
- [8. Reproducibility and CI](#8-reproducibility-and-ci): deterministic
  runs and a GitHub Actions job.
- [9. Maintaining the manifests](#9-maintaining-the-manifests): version
  bumps and clean removals.
- [10. SBOM drift over time](#10-sbom-drift-over-time): what drifts,
  how hashes catch it, and where to enforce the check (scan, CI,
  platform).
- [Appendix: key concepts](#appendix-key-concepts): purls, refs,
  scopes, and tags in one place.

## 1. Quick start

The firmware has libtls and nothing else. A handful of commands take
us from an empty directory to a working SBOM.

```bash
# 1. Scaffold a primary manifest (.primary.json) for the artifact we
#    ship: device-firmware itself.
bomtique manifest init \
  --name device-firmware \
  --version 0.1.0 \
  --type firmware \
  --license MIT

# 2. Add libtls to the pool. --ref accepts a GitHub URL (or any other
#    registered importer URL/purl) and pulls every field the registry
#    can provide. This creates .components.json on first use and
#    appends to it on every subsequent call.
bomtique manifest add \
  --ref https://github.com/libressl/portable/releases/tag/v3.9.0
```

Open `.components.json` and you'll see what GitHub returned:

```json
{
  "name": "portable",
  "version": "v3.9.0",
  "purl": "pkg:github/libressl/portable@v3.9.0",
  "description": "LibreSSL Portable itself...",
  "external_references": [
    { "type": "website", "url": "https://www.libressl.org" },
    { "type": "vcs", "url": "https://github.com/libressl/portable" },
    { "type": "issue-tracker", "url": "https://github.com/libressl/portable/issues" }
  ]
}
```

Three things to refine before this lands in the SBOM:

- The component's `name` came back as `portable` (the GitHub repo
  name); we ship a TLS dependency, so we'd rather call it `libtls`.
- The `version` carries the git tag's `v` prefix (`v3.9.0`); we
  follow SemVer in our SBOM and want `3.9.0`.
- `license` is missing entirely. GitHub's licenses API can't reduce
  libressl's composite license file to a single SPDX ID, so the
  importer left the field unset. We know libtls itself is ISC.

`manifest update` fixes all three in one call:

```bash
# 3. Refine the libtls component with the project's conventions.
bomtique manifest update pkg:github/libressl/portable@v3.9.0 \
  --name libtls \
  --to 3.9.0 \
  --license ISC
```

`--to` rewrites both the `version` field and the matching purl
version segment in lockstep, so the component's purl becomes
`pkg:github/libressl/portable@3.9.0` after this update.

```bash
# 4. Wire libtls into the primary's top-level dependencies, then
#    generate the SBOM.
bomtique manifest add --primary \
  --name libtls --version 3.9.0 \
  --purl pkg:github/libressl/portable@3.9.0

bomtique scan --out ./sbom
```

You now have [`./sbom/device-firmware-0.1.0.cdx.json`](../examples/getting-started/section1/sbom/device-firmware-0.1.0.cdx.json). Output filenames
follow the pattern `<name>-<version>.cdx.json`. With no `--out`,
`bomtique scan` writes one compact JSON per primary to stdout (NDJSON),
which is handy for piping.

> A primary needs at least `--name` plus one of `--version`, `--purl`,
> or hashes. A pool component is the same. Everything else is optional.

> The fetch-inspect-refine pattern shown above (let `--ref` pull what
> the registry knows, then `manifest update` to fix mismatches with
> your project's conventions) generalises to every importer. For a
> repo where the GitHub name already matches your conventions, step
> 3 disappears.

> **Snapshot:** [`examples/getting-started/section1/`](../examples/getting-started/section1/)

## 2. What just happened

After all four commands ran, [`.primary.json`](../examples/getting-started/section1/.primary.json) looks like this:

```json
{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "device-firmware",
    "version": "0.1.0",
    "type": "firmware",
    "license": { "expression": "MIT" },
    "depends-on": ["pkg:github/libressl/portable@3.9.0"]
  }
}
```

[`.components.json`](../examples/getting-started/section1/.components.json) carries the refined libtls entry:

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libtls",
      "version": "3.9.0",
      "description": "LibreSSL Portable itself. This includes the build scaffold and compatibility layer that builds portable LibreSSL from the OpenBSD source code...",
      "license": { "expression": "ISC" },
      "purl": "pkg:github/libressl/portable@3.9.0",
      "external_references": [
        { "type": "website", "url": "https://www.libressl.org" },
        { "type": "vcs", "url": "https://github.com/libressl/portable" },
        { "type": "issue-tracker", "url": "https://github.com/libressl/portable/issues" }
      ]
    }
  ]
}
```

`description` and the three external references came from the GitHub
importer; `name`, `version`, and `license` reflect the refinements
from step 3.

`bomtique scan` discovered both files (any file basename-matching
`.primary.json`, `.components.json`, or `.components.csv` is picked up
under the current directory), built a shared pool from every
`.components.json`, resolved which pool components the primary's
`depends-on` list reaches, and emitted one SBOM.

Both files are meant to be committed to version control. They are the
source of truth; the SBOM is a derived artifact you regenerate on
demand.

## 3. More dependencies, less typing

Field engineers asked for a small on-device admin UI. The team picks
LVGL (an embedded GUI toolkit, on GitHub) and adds it to `libs/libgui/`.
The repo now looks like:

```
device-firmware/
  libs/
    libtls/
    libgui/      <-- new: LVGL for the admin UI
```

LVGL maps cleanly to a GitHub repo (the repo is even named `lvgl`),
so `--ref` does most of the work without the override flags section
1's libtls add needed:

```bash
bomtique manifest add --ref pkg:github/lvgl/lvgl@v9.2.2
```

For libraries on GitHub, GitLab, npm, PyPI, or crates.io, `--ref`
pulls name, version, license, description, supplier, and the standard
external references from the registry. Both purl and URL forms work,
so the equivalent is:

```bash
bomtique manifest add --ref https://github.com/lvgl/lvgl/releases/tag/v9.2.2
```

Flag values still win when you supply them (`--license Apache-2.0`
would override whatever the registry returned), and each override
prints one `warning:` line on stderr.

The team knows the admin UI will only ship in some firmware variants,
so they tag the new component for filtering later (we'll use this in
[Section 7](#7-per-build-variants)). While we're at it, tag libtls
`core` so it lands in every variant build:

```bash
bomtique manifest update pkg:github/lvgl/lvgl@v9.2.2 --tag display
bomtique manifest update pkg:github/libressl/portable@v3.9.0 --tag core
```

`pkg:generic/...` references (typical when you snapshot source from a
vendor distribution URL) don't match any importer, so `--ref` will
error on them. Don't pass `--ref` for those — supply the fields with
flags directly, the way section 1's libtls add did before we added
`--ref`.

Wire LVGL into the primary's `depends-on`:

```bash
bomtique manifest add --primary \
  --name lvgl --version 9.2.2 \
  --purl pkg:github/lvgl/lvgl@v9.2.2
```

A few flags worth knowing about for future adds:

- `--depends-on <ref>` records a runtime edge from this component to
  another pool entry. Repeatable. The ref is either a purl or
  `name@version`.
- `--scope optional` marks a component whose code is loaded but not
  reached under normal operation (a feature flag, a plugin). Default is
  `required`. `excluded` is for build-only tools (compilers, code
  generators) and is dropped from the emitted SBOM.
- `--tag <t>` (repeatable) attaches a producer-side tag. Tags are *not*
  written to the SBOM; they only filter the pool at scan time. See
  [Section 7](#7-per-build-variants).
- `--external type=url` adds an arbitrary external reference; types are
  `website`, `vcs`, `documentation`, `issue-tracker`, `distribution`,
  `support`, `release-notes`, `advisories`, `other`.

> **Snapshot:** [`examples/getting-started/section3/`](../examples/getting-started/section3/)

## 4. Validate before you ship

`bomtique validate` runs the same structural and semantic checks
`scan` runs, but emits no SBOM. Useful as a fast pre-commit or
pre-PR check.

```bash
bomtique validate
```

It checks schema markers, required fields, identity collisions across
the pool, purl canonical form, SPDX license expression syntax, and
filesystem reachability for any `file:` / `path:` references.

Two flags to know:

- `--warnings-as-errors` exits with code 4 on any warning (useful in
  CI). Available on both `validate` and `scan`.
- `--output-validate` (on `scan`) checks the *emitted* document
  against the vendored CycloneDX 1.7 (or SPDX 2.3) schema. The schema
  is embedded in the binary, so no network access is involved.

```bash
# Two independent gates: input manifests well-formed, output document
# schema-valid.
bomtique validate --warnings-as-errors
bomtique scan --output-validate --out ./sbom
```

## 5. Splitting the pool across the tree

Now device-firmware needs to publish telemetry to Acme's cloud over
MQTT. Acme already has an internal MQTT library shared across the
whole product line (`libmqtt`), so the team adds it as a submodule.
Instead of re-typing libmqtt's metadata in every consumer, the
libmqtt repo ships its own `.components.json` and consumers just pull
it in.

```
device-firmware/
  libs/
    libtls/
    libgui/
    libmqtt/
      .components.json     <-- new: ships in the libmqtt repo
```

Discovery walks the whole tree. `bomtique scan` (with no positional
arguments) starts from the current directory and picks up every file
whose basename is exactly `.primary.json`, `.components.json`, or
`.components.csv`. It refuses to descend into `.git`, `node_modules`,
`vendor`, `.venv`, `testdata`, or any directory whose name starts with
`.`. Symbolic links are not followed.

Every `.components.json` contributes to one shared pool. A `depends-on`
edge declared inside `libs/libmqtt/.components.json` resolves freely
against an entry in the project root's `.components.json`.

The libmqtt submodule's manifest is authored once in the libmqtt repo
and committed there, so every consumer sees the same file:

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libmqtt",
      "version": "4.3.0",
      "description": "Acme MQTT client library",
      "supplier": { "name": "Acme Corp" },
      "license": { "expression": "EPL-2.0" },
      "purl": "pkg:generic/acme/libmqtt@4.3.0",
      "depends-on": ["pkg:github/libressl/portable@v3.9.0"],
      "tags": ["core", "networking"]
    }
  ]
}
```

When Acme clones device-firmware with submodules, this file lands at
`libs/libmqtt/.components.json` and `bomtique scan` picks it up
automatically. Notice libmqtt declares its own dependency on libtls
(it's the transport for the encrypted MQTT connection); that
`depends-on` ref doesn't need to know libtls lives in the root
`.components.json`, because the pool is shared across every manifest
under the walk.

The last step is to wire libmqtt into the firmware's primary
`depends-on`:

```bash
bomtique manifest add --primary \
  --name libmqtt --version 4.3.0 \
  --purl pkg:generic/acme/libmqtt@4.3.0
```

> By default, `bomtique manifest add -C <dir>` walks up from `<dir>`
> looking for the nearest existing components manifest. If you want
> to force a new file at a specific path (for example, when seeding
> a fresh submodule that doesn't yet have one), pass
> `--into <path>` explicitly.

> **Snapshot:** [`examples/getting-started/section5/`](../examples/getting-started/section5/)

## 6. Vendored and patched code

The MQTT payloads now carry JSON-encoded config blobs, and the firmware
needs to compress them on the wire. Two new dependencies, neither one
clean enough to live as a normal external library:

- **cjson** parses the configs. Upstream has a known integer overflow
  bug we need fixed today, and the upstream PR is still in review, so
  we vendor it and apply our own patch.
- **miniz** does the compression. The build system needs custom
  defines, so we vendor it as plain source.

Both go into `src/`:

```
device-firmware/
  src/
    main.c
    cjson/             <-- new: vendored fork with local patch
      patches/
    miniz/             <-- new: vendored source
```

For source you've copied into your repo, `--vendored-at <dir>` does
two things in one call: installs a directory-form SHA-256 hash
directive on the vendored path (the digest is computed at scan time
from on-disk bytes, not now), and adds a `pedigree.ancestors[0]`
entry. The ancestor's metadata comes from `--upstream-ref` (a purl
or URL pointing at the upstream, fetched through the same importer
registry as the component-side `--ref`); `--upstream-name`,
`--upstream-version`, and friends are still available as scalar
overrides.

Run vendored adds from the project root so `--vendored-at <dir>`
resolves against the primary's directory. The component's own `--purl`
points at the local vendored copy: a repo-local purl with the
vendored path spliced into the namespace.

```bash
# cjson — vendored fork of DaveGamble/cJSON, locally patched.
bomtique manifest add \
  --name cjson --version 1.7.17 \
  --license MIT \
  --description "Ultralightweight JSON parser (vendored fork)" \
  --supplier "Dave Gamble" \
  --purl pkg:github/acme/device-firmware/src/cjson@1.7.17 \
  --vendored-at src/cjson --ext c,h \
  --upstream-ref https://github.com/DaveGamble/cJSON/releases/tag/v1.7.17 \
  --tag core

# miniz — vendored, no patches.
bomtique manifest add \
  --name miniz --version 3.0.2 \
  --license MIT \
  --supplier "Rich Geldreich" \
  --purl pkg:github/acme/device-firmware/src/miniz@3.0.2 \
  --vendored-at src/miniz --ext c,h \
  --upstream-ref https://github.com/richgel999/miniz/releases/tag/3.0.2 \
  --tag core
```

The `--upstream-ref` fetch populates the ancestor's name, version,
description, license, purl, and standard external references straight
from GitHub, so the SBOM's pedigree carries the upstream's full
identity rather than a name+version skeleton.

`--ext c,h` is a comma-separated, case-insensitive extension filter
applied to the directory walk. Only files whose name ends in `.c` or
`.h` (and isn't hidden) feed the digest.

The cjson manifest carries a local CVE backport. Register it with
`manifest patch`:

```bash
bomtique manifest patch \
  pkg:github/acme/device-firmware/src/cjson@1.7.17 \
  ./src/cjson/patches/cjson-fix-int-overflow.patch \
  --type backport \
  --resolves "type=security,name=CVE-2024-XXXXX"
```

`<diff-path>` is stored as-is and resolved relative to the components
manifest at scan time. bomtique doesn't read the patch file here; the
contents are read and base64-encoded at scan time per spec §9.2. Patch
types are `unofficial`, `monkey`, `backport`, and `cherry-pick`.

The cjson entry in the root [`.components.json`](../examples/getting-started/section6/.components.json) now looks like:

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "cjson",
      "version": "1.7.17",
      "description": "Ultralightweight JSON parser (vendored fork)",
      "supplier": { "name": "Dave Gamble" },
      "license": { "expression": "MIT" },
      "purl": "pkg:github/acme/device-firmware/src/cjson@1.7.17",
      "hashes": [
        { "algorithm": "SHA-256", "path": "./src/cjson/", "extensions": ["c", "h"] }
      ],
      "pedigree": {
        "ancestors": [
          {
            "name": "cJSON",
            "version": "1.7.17",
            "purl": "pkg:github/DaveGamble/cJSON@1.7.17"
          }
        ],
        "patches": [
          {
            "type": "backport",
            "diff": { "url": "./src/cjson/patches/cjson-fix-int-overflow.patch" },
            "resolves": [
              { "type": "security", "name": "CVE-2024-XXXXX" }
            ]
          }
        ]
      },
      "tags": ["core"]
    }
  ]
}
```

Wire both into the primary's `depends-on`:

```bash
bomtique manifest add --primary \
  --name cjson --version 1.7.17 \
  --purl pkg:github/acme/device-firmware/src/cjson@1.7.17

bomtique manifest add --primary \
  --name miniz --version 3.0.2 \
  --purl pkg:github/acme/device-firmware/src/miniz@3.0.2
```

Commit every `.primary.json`, `.components.json`, and patch file to
version control. The directory hash recomputes on every scan, so if
the vendored files change, the SBOM hash changes with them.

> **Snapshot:** [`examples/getting-started/section6/`](../examples/getting-started/section6/)

## 7. Per-build variants

device-firmware now has every dependency it needs, but Acme actually
ships two SKUs from this codebase: a headless base build, and a
display build that includes the LVGL admin UI. Same source tree, two
different SBOMs. Tags do the filtering:

```bash
# Base variant: only components tagged 'core'.
bomtique scan --tag core --out ./sbom/base

# Display variant: components tagged 'core' OR 'display'.
bomtique scan --tag core --tag display --out ./sbom/display
```

`--tag` is repeatable and matches *any* listed tag. Pool components
without a matching tag look unreachable to that scan and drop out of
the SBOM. The base variant also prints a warning that the primary's
`depends-on` edge to lvgl is unresolved (because lvgl got filtered
out); the edge is dropped, the primary stays. That's the right shape
for a variant build.

> **Snapshot:** [`examples/getting-started/section7/`](../examples/getting-started/section7/) (same source tree as section 6, with `sbom-base/` and `sbom-display/` showing the variant filter at work).

## 8. Reproducibility and CI

Time to cut the 1.0.0 release:

```bash
bomtique manifest update --primary --to 1.0.0
```

`--primary` updates the primary component itself (bomtique's
default `manifest update` operates on pool entries; `--primary`
flips it to the primary, takes no `<ref>` argument, and applies the
same field-replacement and `--to` lockstep behaviour). From the
next scan onward, the emitted file is
[`device-firmware-1.0.0.cdx.json`](../examples/getting-started/section8/sbom/device-firmware-1.0.0.cdx.json).

`SOURCE_DATE_EPOCH` (or `--source-date-epoch`) makes the emitted
document byte-identical across runs. Without it, the timestamp is
omitted and the SPDX namespace is a fresh UUIDv4 each run.

```bash
bomtique scan \
  --source-date-epoch "$(git log -1 --format=%ct)" \
  --out ./sbom
```

That stamps every emitted document with the same UTC timestamp and
derives a UUIDv5 serial from the JCS-canonicalised components, so two
runs on the same input tree produce identical bytes.

Wired into GitHub Actions, the loop is generate, validate output,
score against NTIA, archive, attach to release on tag pushes:

```yaml
jobs:
  sbom:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive

      - name: Install bomtique and sbomqs
        run: |
          go install github.com/interlynk-io/bomtique/cmd/bomtique@latest
          go install github.com/interlynk-io/sbomqs@latest

      - name: Generate SBOM
        run: |
          bomtique scan \
            --warnings-as-errors --output-validate \
            --source-date-epoch "$(git log -1 --format=%ct)" \
            --out ./sbom

      - name: Score against NTIA Minimum Elements
        run: sbomqs score --profile ntia ./sbom/*.cdx.json

      - name: Upload workflow artifact
        uses: actions/upload-artifact@v4
        with:
          name: sbom
          path: ./sbom/

      - name: Attach to GitHub release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: gh release upload "$GITHUB_REF_NAME" ./sbom/*.cdx.json
```

Run order matters: generate first, score second, upload third. If
sbomqs reports a new NTIA gap, the upload step never runs, and you
never ship an SBOM that doesn't meet the regulatory baseline.

[sbomqs](https://github.com/interlynk-io/sbomqs) scores the emitted
document against compliance frameworks (NTIA Minimum Elements, BSI,
FSCT). The goal is full NTIA compliance, not a particular tool score:
every field NTIA mandates (supplier, name, version, hash, unique
identifier, dependency relationships, author, timestamp) needs to be
present and accurate. Anything sbomqs flags is a manifest field that
needs filling in.

```bash
# Score offline against multiple profiles.
sbomqs score --profile ntia ./sbom/device-firmware-1.0.0.cdx.json
sbomqs score --profile bsi  ./sbom/device-firmware-1.0.0.cdx.json
sbomqs score --profile fsct ./sbom/device-firmware-1.0.0.cdx.json
```

> **Snapshot:** [`examples/getting-started/section8/`](../examples/getting-started/section8/) (primary bumped to 1.0.0; emits `sbom/device-firmware-1.0.0.cdx.json`).

## 9. Maintaining the manifests

`manifest update` edits an existing pool entry in place:

```bash
# Bump libmqtt 4.3.0 -> 4.4.0. --to also rewrites the purl version
# segment when it matches the old version.
bomtique manifest update pkg:generic/acme/libmqtt@4.3.0 --to 4.4.0

# Replace a single field.
bomtique manifest update pkg:github/libressl/portable@v3.9.0 --supplier "OpenBSD"

# Refresh metadata from the registry, layering flag overrides on top.
bomtique manifest update --refresh pkg:github/lvgl/lvgl@v9.2.2

# Preview without writing.
bomtique manifest update pkg:github/libressl/portable@v3.9.0 --supplier "OpenBSD" --dry-run
```

`manifest remove` drops a component from the pool and scrubs every
`depends-on` edge that pointed at it (across the whole tree, including
the primary):

```bash
bomtique manifest remove pkg:github/lvgl/lvgl@v9.2.2
```

Each scrubbed edge prints one line on stderr so you can audit the
fallout.

Because directory-form hashes recompute on every scan, vendored files
that silently change land in the next SBOM with a new digest. To
freeze the digest, replace the directive with a literal
`{ "algorithm": "SHA-256", "value": "..." }` form. Section 10 covers
how to use that recompute behaviour deliberately to detect drift.

> **Snapshot:** [`examples/getting-started/section9/`](../examples/getting-started/section9/) (libmqtt bumped to 4.4.0 with the primary depends-on re-wired).

## 10. SBOM drift over time

An SBOM is only accurate the moment it's generated. Over time,
three things can drift even when the manifests look unchanged:

- **Vendored source.** A file in `src/cjson/` or `src/miniz/` gets
  edited (intentional bump, accidental save, sloppy merge conflict,
  rebase mishap). The directory-form hash on the next scan won't
  match the previous one.
- **Patch files.** `patches/cjson-fix-int-overflow.patch` gets
  modified locally, usually because someone "rebased the patch onto
  upstream" without writing it down. The patch's base64 attachment
  in the SBOM changes.
- **Upstream metadata.** A GitHub tag gets force-moved to a different
  commit, an npm version gets republished, a license changes
  upstream, a repo gets transferred. The next `manifest update
  --refresh` returns different fields than the original
  `manifest add --ref` did.

The point of hashes is that drift is impossible to hide from them.
Three hash forms, three roles:

- **Directory-form** (`{"path": "./src/cjson/", "extensions": [...]}`)
  is the live digest. Recomputed every scan, so the SBOM always
  reflects on-disk reality.
- **File-form** (`{"file": "./parser.c"}`) is the live digest of one
  specific file. Same recompute behaviour.
- **Literal** (`{"value": "abc123..."}`) is a frozen receipt.
  Recorded once and passed through verbatim. It's what you saw at
  the moment you committed to depend on this version.

The spec already lets you carry both kinds on a single component
(§8.5). For high-value vendored code, that combination is the
strongest receipt: the live digest tracks reality, the literal
records what reality was supposed to be.

```json
"hashes": [
  { "algorithm": "SHA-256", "path": "./src/cjson/", "extensions": ["c", "h"] },
  { "algorithm": "SHA-256", "value": "<digest captured at vendor time>" }
]
```

bomtique today emits both into the SBOM verbatim; it doesn't enforce
that they have to agree. That's where the rest of this section comes
in: capture the divergence somewhere, even if the consumer is the
one doing the comparison.

### Capturing drift at scan time

The cheapest layer. Two patterns:

- **Live-only directory hash on every vendored component.** Already
  the default in the section 6 examples. The SBOM hash field reflects
  whatever's on disk at scan time, so any source-tree drift moves the
  digest, which moves the SBOM bytes, which makes the drift
  reviewable as a diff against the previous SBOM.
- **Pin a literal hash next to the live one.** When you first vendor
  a component, capture the directory digest from the very first scan
  and add it as a literal hash entry. From then on, both digests
  appear in the SBOM. They should always agree; the moment they
  don't, the SBOM has two different SHA-256 values for the same
  component, which is a glaring review-time signal.

A future bomtique feature worth tracking: a scan-time policy that
*enforces* matching, failing the scan when a literal hash diverges
from a corresponding live one. Until then, the divergence is
informational, but still visible.

### Capturing drift in CI

The practical sweet spot. Two patterns work well together.

**Baseline SBOM as a lockfile.** Generate the SBOM under a fixed
`SOURCE_DATE_EPOCH` and commit it to the repo (Acme keeps it at
`sbom/device-firmware-1.0.0.cdx.json`). On every PR, regenerate and
diff:

```yaml
- name: Detect SBOM drift
  run: |
    bomtique scan \
      --source-date-epoch "$(git log -1 --format=%ct)" \
      --out ./sbom-fresh
    diff -u sbom/device-firmware-1.0.0.cdx.json \
            sbom-fresh/device-firmware-1.0.0.cdx.json
```

The PR diff now carries two pieces side by side: what the
manifests changed, and what the resulting SBOM changed. Reviewers
see both. Anything in the SBOM diff that wasn't motivated by a
manifest change is unintended drift.

This is the same pattern as a `Cargo.lock` or `package-lock.json`,
applied to the SBOM. The only requirement is determinism, which
bomtique guarantees under `SOURCE_DATE_EPOCH`.

**Periodic upstream re-verification.** Every component whose `purl`
matches an importer is one network round-trip away from being
re-checked. A scheduled CI job iterates over them with `manifest
update --refresh --dry-run` and reports anything that changed:

```bash
jq -r '.components[].purl
       | select(startswith("pkg:github/") or
                startswith("pkg:gitlab/") or
                startswith("pkg:npm/")    or
                startswith("pkg:pypi/")   or
                startswith("pkg:cargo/"))' .components.json \
| while read -r p; do
    bomtique manifest update --refresh "$p" --dry-run
  done
```

Catches license changes, repo transfers, tag force-moves, yanked
versions. Doesn't help `pkg:generic/...` components, since they have
no upstream registry to query; for libraries Acme snapshotted from a
vendor distribution URL, the baseline-SBOM diff is the only
backstop.

### Capturing drift at the SBOM platform layer

For longer time horizons (months, years across many releases), an
external SBOM platform is the natural home. Tools like
[Interlynk](https://www.interlynk.io/) and
[Dependency-Track](https://dependencytrack.org/) ingest every SBOM
you emit and remember.

What that buys you on top of CI:

- **History per component.** "libtls's SHA-256 was X on 2026-01-15
  and Y on 2026-04-20; here's the PR that changed it." Useful for
  audit conversations long after the change has scrolled off the
  git log.
- **CVE correlation.** When NVD publishes a new advisory against
  cjson 1.7.17, the platform can flag that your SBOM lists that
  exact purl, plus the pedigree patch you carry against it.
- **Cross-release drift.** "Two firmware variants shipped this
  quarter; their libtls hash differs. Why?"

This layer is downstream of bomtique itself. bomtique's contribution
is producing deterministic, hash-bearing SBOMs that the platform can
ingest reliably.

### How Acme weaves it together

The four layers stack:

1. cjson and miniz carry directory-form hashes. When a developer
   touches `src/cjson/` for any reason, the next scan's digest
   moves.
2. Acme commits `sbom/device-firmware-1.0.0.cdx.json` (under a fixed
   `SOURCE_DATE_EPOCH`). The CI workflow from section 8 has a
   "Detect SBOM drift" step that regenerates and diffs. Any
   uncommitted SBOM change either blocks merge or pings the team for
   review, depending on how strict the project wants to be.
3. A weekly scheduled CI job runs the upstream re-verification loop
   above. If libtls or lvgl's metadata moved, the job surfaces the
   change for review through whichever channel the team uses (a
   GitHub issue, a Slack message, a tracking dashboard).
4. Every release SBOM is uploaded to Interlynk's platform. Six
   months from now, the platform can answer "what was libtls's hash
   in the 1.0.0 release?" without anyone having to dig through git.

Each layer answers a different question. Together they turn the
SBOM from a one-shot artifact into a living receipt that catches
drift wherever it happens.

---

## Appendix: key concepts

### Purls

A **Package URL** ("purl") is the standard, language-neutral way to
point at a software package. Every purl has the same shape:

```
pkg:<type>/<namespace>/<name>@<version>
```

`<type>` names the ecosystem (`github`, `gitlab`, `npm`, `pypi`,
`cargo`, `generic`, ...). `<namespace>` is the path-like segment
that scopes the name (the GitHub owner, the npm scope, the OpenBSD
distribution path, etc.). `<name>` is the package name and
`<version>` is its version.

A few examples this guide uses:

- `pkg:github/libressl/portable@v3.9.0`: the libressl/portable repo
  on GitHub at tag v3.9.0.
- `pkg:npm/express@4.18.2`: the express package on npm at 4.18.2.
- `pkg:generic/acme/libmqtt@4.3.0`: Acme's internal libmqtt. The
  `generic` type is the catch-all for things that don't live in a
  registry the importers know about.
- `pkg:github/acme/device-firmware/src/cjson@1.7.17`: a vendored
  copy of cjson living inside the device-firmware repo, with the
  vendored path spliced into the namespace.

bomtique uses purls everywhere as the canonical identifier. They
land verbatim in the emitted SBOM, and `depends-on` edges resolve by
purl across every `.components.json` in the tree.

### Refs

`--ref` accepts both purl form (`pkg:npm/express@4.18.2`) and the
matching registry URL (`https://www.npmjs.com/package/express/v/4.18.2`)
for every importer bomtique ships. Whichever you pass, the importer
stores the canonical purl on the component. Use whichever is easier
to copy from your browser.

### Scopes and tags

Two orthogonal labels on each pool component:

- **`scope`** describes runtime presence. `required` is the default
  ("present at runtime, normally reached"). `optional` means
  "present but not always reached" (a feature flag, a plugin).
  `excluded` means "build-time only" (compilers, code generators);
  excluded components are dropped from the emitted SBOM.
- **`tags`** are producer-side filtering hints. Tags never appear in
  the emitted SBOM; `bomtique scan --tag <t>` keeps only pool
  components whose `tags` include `<t>`. We use `core` and `display`
  in this guide to drive per-variant SBOMs.

## Further reading

- [`spec/component-manifest-v1.md`](../spec/component-manifest-v1.md):
  the normative manifest specification, covering schema, identity
  rules, pedigree, hash forms, validation, and determinism.
- [`docs/usage.md`](./usage.md): full CLI reference covering every
  flag on every subcommand, registry importers, exit codes, and
  environment variables.
- [`docs/discovery.md`](./discovery.md): how `bomtique` walks a
  directory tree to find manifests and which directories it skips.
- [`docs/determinism.md`](./determinism.md): what `SOURCE_DATE_EPOCH`
  controls and how byte-identical output is guaranteed across runs.
