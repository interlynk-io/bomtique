# Getting Started

This guide walks you from zero to a working CycloneDX SBOM, then layers
on the features you need as your project grows. Each section adds one
concept on top of the last. Stop reading at any point and you'll still
have something useful.

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

## 1. Quick start

The firmware has libtls and nothing else. Three commands give us a
working SBOM that covers it.

> **What's a purl?** A Package URL is the standard, language-neutral
> way to point at a software package. The shape is
> `pkg:<type>/<namespace>/<name>@<version>`. For a GitHub repo it's
> `pkg:github/<owner>/<repo>@<ref>`, so `libressl/portable` at tag
> `v3.9.0` becomes `pkg:github/libressl/portable@v3.9.0`. bomtique
> uses purls everywhere as the canonical identifier, so it pays to
> learn to read them.

```bash
# 1. Scaffold a primary manifest (.primary.json) for the artifact we
#    ship: device-firmware itself.
bomtique manifest init \
  --name device-firmware \
  --version 0.1.0 \
  --type firmware \
  --license MIT \
  --purl pkg:github/acme/device-firmware@0.1.0

# 2. Add libtls to the pool. --ref accepts a GitHub URL (or any other
#    registered importer URL/purl) and pulls description, website, and
#    issue-tracker URL from the registry. We still pass --name and
#    --version because the repo is libressl/portable (not "libtls"), and
#    --license because GitHub doesn't return a clean SPDX ID for
#    libressl. This creates .components.json on first use and appends
#    to it on every subsequent call.
bomtique manifest add \
  --ref https://github.com/libressl/portable/releases/tag/v3.9.0 \
  --name libtls --version 3.9.0 \
  --license ISC

# 3. Wire libtls into the primary's top-level dependencies, then
#    generate the SBOM.
bomtique manifest add --primary \
  --name libtls --version 3.9.0 \
  --purl pkg:github/libressl/portable@v3.9.0

bomtique scan --out ./sbom
```

You now have `./sbom/device-firmware-0.1.0.cdx.json`. Output filenames
follow the pattern `<name>-<version>.cdx.json`. With no `--out`,
`bomtique scan` writes one compact JSON per primary to stdout (NDJSON),
which is handy for piping.

> A primary needs at least `--name` plus one of `--version`, `--purl`,
> or hashes. A pool component is the same. Everything else is optional.

## 2. What just happened

`bomtique manifest init` wrote `.primary.json`:

```json
{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "device-firmware",
    "version": "0.1.0",
    "type": "firmware",
    "license": { "expression": "MIT" },
    "purl": "pkg:github/acme/device-firmware@0.1.0",
    "depends-on": ["pkg:github/libressl/portable@v3.9.0"]
  }
}
```

The first `bomtique manifest add` wrote `.components.json` next to
it; the second (`--primary`) appended to the primary's `depends-on`:

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libtls",
      "version": "3.9.0",
      "description": "LibreSSL Portable itself. This includes the build scaffold and compatibility layer that builds portable LibreSSL from the OpenBSD source code...",
      "license": { "expression": "ISC" },
      "purl": "pkg:github/libressl/portable@v3.9.0",
      "external_references": [
        { "type": "website", "url": "https://www.libressl.org" },
        { "type": "vcs", "url": "https://github.com/libressl/portable" },
        { "type": "issue-tracker", "url": "https://github.com/libressl/portable/issues" }
      ]
    }
  ]
}

The `description` and `external_references` fields came from the
GitHub importer for free. You'll see a `warning:` line on stderr for
each flag that overrode a value the importer set (`--name`,
`--version`, `--license`); those are informational, not errors.
```

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
so they tag libgui for filtering later (we'll use this in
[Section 7](#7-per-build-variants)):

```bash
bomtique manifest update pkg:github/lvgl/lvgl@v9.2.2 --tag display
```

`pkg:generic/...` references (typical when you snapshot source from a
vendor distribution URL) don't match any importer, so `--ref` will
error on them. Don't pass `--ref` for those — supply the fields with
flags directly, the way section 1's libtls add did before we added
`--ref`.

Wire libgui into the primary's `depends-on`:

```bash
bomtique manifest add --primary \
  --name libgui --version 9.2.2 \
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

For source you've copied into your repo, `--vendored-at <dir>`
synthesises three things in one call:

1. A repo-local `purl` derived from the primary's purl, with the
   vendored path spliced into the namespace.
2. A directory-form SHA-256 hash directive on the vendored path. The
   digest is computed at scan time from on-disk bytes, not now.
3. A `pedigree.ancestors[0]` entry built from your `--upstream-*`
   flags.

Run vendored adds from the project root so `--vendored-at <dir>`
resolves against the primary's directory. The auto-derived purl
requires the primary's purl type to be `github`, `gitlab`, or
`bitbucket`; pass `--purl` explicitly if your primary uses some other
type.

```bash
# cjson — vendored fork of DaveGamble/cJSON, locally patched.
bomtique manifest add \
  --name cjson --version 1.7.17 \
  --license MIT \
  --description "Ultralightweight JSON parser (vendored fork)" \
  --supplier "Dave Gamble" \
  --vendored-at src/cjson --ext c,h \
  --upstream-name cJSON --upstream-version 1.7.17 \
  --upstream-purl pkg:github/DaveGamble/cJSON@1.7.17 \
  --tag core

# miniz — vendored, no patches.
bomtique manifest add \
  --name miniz --version 3.0.2 \
  --license MIT \
  --supplier "Rich Geldreich" \
  --vendored-at src/miniz --ext c,h \
  --upstream-name miniz --upstream-version 3.0.2 \
  --upstream-purl pkg:github/richgel999/miniz@3.0.2 \
  --tag core
```

`--ext c,h` is a comma-separated, case-insensitive extension filter
applied to the directory walk. Only files whose name ends in `.c` or
`.h` (and isn't hidden) feed the digest.

The cjson manifest carries a local CVE backport. Register it with
`manifest patch` using the auto-derived purl as the ref:

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

The cjson entry in the root `.components.json` now looks like:

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
`depends-on` edge to libgui is unresolved (because libgui got
filtered out); the edge is dropped, the primary stays. That's the
right shape for a variant build.

## 8. Reproducibility and CI

Time to cut the 1.0.0 release. Edit `.primary.json` by hand and bump
both the `version` field and the `purl` version segment to `1.0.0`
(bomtique's `manifest update` operates on pool components, not on
the primary itself, so this part is a manual edit). From the next
scan onward, the emitted file is `device-firmware-1.0.0.cdx.json`.

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

## 9. Maintaining the manifests

`manifest update` edits an existing pool entry in place:

```bash
# Bump libmqtt 4.3.0 -> 4.4.0. --to also rewrites the purl version
# segment when it matches the old version.
bomtique manifest update -C libs/libmqtt pkg:generic/acme/libmqtt@4.3.0 --to 4.4.0

# Replace a single field.
bomtique manifest update pkg:github/libressl/portable@v3.9.0 --license ISC

# Refresh metadata from the registry, layering flag overrides on top.
bomtique manifest update --refresh pkg:github/lvgl/lvgl@v9.2.2

# Preview without writing.
bomtique manifest update pkg:github/libressl/portable@v3.9.0 --license ISC --dry-run
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
`{ "algorithm": "SHA-256", "value": "..." }` form.

---

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
