# Component Manifest, Version 1

**Schema markers:** `primary-manifest/v1`, `component-manifest/v1`
**Status:** Draft
**Latest version:** this document

## Abstract

This document specifies the Component Manifest, a pair of file formats for
hand-curated software bill-of-materials inputs.

- A **primary manifest** describes a single primary artifact — an executable,
  firmware image, service, wheel, container, or similar buildable output —
  and its top-level dependencies.
- A **components manifest** describes a pool of third-party components that
  one or more primary manifests can depend on.

Conforming consumers read a processing set of manifests and emit one Software
Bill of Materials (SBOM) per primary manifest, in CycloneDX or SPDX form.

The format is designed for codebases where automated Software Composition
Analysis (SCA) is unreliable or absent — C/C++, embedded, legacy, and hybrid
projects with vendored or patched native dependencies — and for projects that
produce multiple artifacts from the same source tree.

## Status of This Document

This is a draft specification. Version 1 of the schema markers
(`primary-manifest/v1` and `component-manifest/v1`) is considered stable:
additive, backward-compatible changes MAY be made to this document;
non-additive changes require a new schema marker (Section 17).

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conventions and Terminology](#2-conventions-and-terminology)
3. [Design Posture](#3-design-posture)
4. [File Format](#4-file-format)
5. [Manifest Types](#5-manifest-types)
6. [Component Object](#6-component-object)
7. [Enumerations](#7-enumerations)
8. [Hashes](#8-hashes)
9. [Pedigree](#9-pedigree)
10. [Dependency Model](#10-dependency-model)
11. [Identity and Uniqueness](#11-identity-and-uniqueness)
12. [Multi-File Composition](#12-multi-file-composition)
13. [Validation](#13-validation)
14. [Serialization to SBOM Formats](#14-serialization-to-sbom-formats)
15. [Determinism](#15-determinism)
16. [Conformance](#16-conformance)
17. [Versioning](#17-versioning)
18. [Security Considerations](#18-security-considerations)
19. [References](#19-references)
20. [Appendix A: JSON Schema](#appendix-a-json-schema)
21. [Appendix B: Examples (Non-Normative)](#appendix-b-examples-non-normative)

## 1. Introduction

This specification defines a pair of hand-authored file formats that
together describe the inputs needed to produce a Software Bill of Materials
(SBOM):

- A **primary manifest** (JSON) declares a single primary artifact — the
  thing being built — together with its top-level dependencies.
- A **components manifest** (JSON or CSV) declares a pool of third-party
  components that primary manifests can depend on.

Both file types share a common Component object (Section 6). A repository
that builds one artifact typically has one primary manifest plus one or
more components manifests; a repository that builds many artifacts (a
monorepo, a microservice collection, a multi-wheel Python project) has one
primary manifest per artifact and a shared pool of components manifests.
Conforming consumers emit one SBOM per primary manifest.

The formats are authored and maintained by developers in the artifact's
source repository, alongside the code they describe.

### 1.1 Scope

This specification defines:

- The structure and field semantics of primary manifests and components
  manifests.
- Rules for identity, deduplication, and cross-manifest dependency
  resolution.
- Rules for hash computation, including deterministic digests over
  directories.
- The mapping from manifest fields to CycloneDX component and dependency
  records.
- The projection from manifest fields to SPDX package and relationship
  records.
- Determinism requirements for any conforming consumer.

This specification does **not** define:

- Command-line interfaces, flags, or invocation of any particular tool.
- Policy for which components must appear in a manifest.
- Mechanisms for automated discovery of components from source code,
  binaries, or package managers.
- Mechanisms for automated discovery of manifest files on disk.
- Quality or compliance scoring of the generated SBOM.

### 1.2 Non-Goals

Automatic component detection, PURL or CPE lookup, nested or recursive
component hierarchies, and rewriting of existing SBOMs are explicitly out of
scope.

## 2. Conventions and Terminology

### 2.1 Requirements Language

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this
document are to be interpreted as described in BCP 14 [RFC2119] [RFC8174] when,
and only when, they appear in all capitals, as shown here.

### 2.2 Definitions

**Manifest.** A file conforming to Section 4 that declares one of the
schema markers `primary-manifest/v1` or `component-manifest/v1`.

**Primary manifest.** A manifest (Section 5.1) that describes a single
primary artifact and its top-level dependencies.

**Primary.** The single Component object declared by a primary manifest.
The primary of a primary manifest becomes the `metadata.component` of the
emitted CycloneDX SBOM, or the SPDX DESCRIBES target of the emitted SPDX
document.

**Components manifest.** A manifest (Section 5.2) that describes a pool of
third-party components available to primaries.

**Component.** A software package, library, firmware image, or other
artifact. Used uniformly for both the primary and entries in a components
manifest's `components[]` array. See Section 6 for the Component schema.

**Pool.** The merged set of components from every components manifest in
a processing set (Section 12.2).

**Processing set.** The collection of manifest files a consumer is
preparing to process in a single run (Section 12.1). Partitions into
primary manifests and components manifests.

**Producer.** A human or tool that writes a manifest.

**Consumer.** A tool that reads a processing set and emits one SBOM per
primary manifest.

**Identity.** The value used to uniquely identify a component across
manifests. See Section 11.

**Vendored component.** A component whose source is copied into the
artifact's repository rather than fetched at build time.

**Patched component.** A vendored component modified locally relative to
its upstream source.

**Manifest file.** The on-disk file containing a manifest. Relative paths
inside a manifest are resolved from the manifest file's directory
(Section 4.3).

## 3. Design Posture

The manifest vocabulary aligns with a **subset** of CycloneDX 1.7. Field
names, enumerations, and structural shapes are chosen to round-trip losslessly
to CycloneDX component records for the fields this specification covers. SPDX
output is a supported, lossy projection (Section 14.2).

This alignment is normative for v1: a consumer MUST NOT reinterpret manifest
enumerations with semantics divergent from CycloneDX 1.7. CycloneDX fields
not covered by this specification are not part of v1, and producers MUST NOT
rely on them being preserved by a conforming consumer.

Known deviations from CycloneDX 1.7, deliberate in v1:

- The `crypto-asset` component type is not supported (CBOM use cases are
  out of scope for v1).
- `externalReferences[].hashes` are not supported.
- The CycloneDX `licenses[].expression` form is supported but the
  `licenses[].license` object form with `name` (non-SPDX licenses) is not;
  v1 accepts only valid SPDX License Expressions (Section 6.3).
- `tags` in the manifest drive producer-side per-build filtering
  (Section 6.2); they are not serialized as CycloneDX component tags in the
  output, because their role in v1 is upstream of SBOM emission.

## 4. File Format

### 4.1 Serializations

A manifest MUST be serialized as one of:

- A JSON document conforming to Section 5.
- A CSV document conforming to Section 4.5 (components manifests only).

A **primary manifest** MUST be serialized as JSON; CSV is not defined for
primary manifests (a single-component format is not worth a CSV variant).
A **components manifest** MAY use either JSON or CSV.

The serialization is determined by the file name extension: `.json` or `.csv`.
Other extensions are not defined by this specification; a consumer MAY reject
them or MAY infer the format from file contents.

### 4.2 Character Encoding

A manifest MUST be encoded as UTF-8. A consumer MUST reject manifests
containing invalid UTF-8 sequences.

### 4.3 Path Resolution

Every path appearing in a manifest (license `file`, hash `file`, hash `path`,
patch `diff.url` with a local path) MUST be a **relative path**. A consumer
MUST reject any manifest carrying an absolute path — POSIX paths beginning
with `/`, Windows paths with a drive letter prefix (e.g. `C:\`), or Windows
UNC paths (e.g. `\\server\share\...`) — with an error identifying the
offending field.

Relative paths MUST be resolved against the directory containing the
manifest file. A consumer MUST NOT resolve relative paths against the current
working directory or any other base.

A consumer MUST reject a manifest whose relative path, after resolution,
escapes the manifest file's directory tree by traversal (`..`). There is no
configuration toggle to permit escape: if a referenced file lives outside
the manifest directory, the producer MUST place the manifest in a parent
directory that contains it. See Section 18.

### 4.4 Schema Marker

Every manifest file MUST include a schema marker identifying its type and
version. Two markers are defined by v1:

- `primary-manifest/v1` — identifies a primary manifest (Section 5.1).
- `component-manifest/v1` — identifies a components manifest (Section 5.2).

The marker appears as follows:

- In JSON: as the top-level string field `"schema"`.
- In CSV: as the first non-empty line of the file, exactly one of
  `#primary-manifest/v1` or `#component-manifest/v1` (a CSV comment, not a
  data row).

A file without a schema marker is not a manifest. A consumer that encounters
such a file during discovery MUST ignore it silently; see Section 12.5.

A consumer MUST reject a file whose marker matches either family pattern
`primary-manifest/*` or `component-manifest/*` but does not exactly equal
`primary-manifest/v1` or `component-manifest/v1` respectively. Such files
are manifests declared against an unknown version of this specification.

### 4.5 CSV Format

The CSV serialization is a strict subset of the JSON serialization. The
following columns are defined, in this order:

```
name,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags
```

- The first line MUST be the schema marker comment (Section 4.4).
- The second line MUST be the column header, exactly as above.
- Each subsequent non-empty line represents one component.
- `depends_on` and `tags` are comma-separated lists; fields containing commas
  MUST be double-quoted per [RFC4180].
- A row MUST NOT specify both `hash_value` and `hash_file`.

The CSV format does not support `external_references`, the structured
`license` object form (only a simple SPDX expression as a string fits in the
`license` column; per-license `texts[]` are JSON-only), `pedigree`, directory
hashes, or an explicit `bom-ref`. Producers requiring those fields MUST use
the JSON format.

### 4.5.1 CSV Parsing Rules

- **Line endings.** A consumer MUST accept both `CRLF` (per [RFC4180]) and
  bare `LF` as line terminators. Producers SHOULD emit `LF` for consistency
  with modern Unix toolchains.
- **BOM.** A leading UTF-8 byte-order mark (U+FEFF) at the start of the file
  MUST be stripped before parsing. This accommodates files saved by
  spreadsheet applications such as Excel.
- **Blank lines.** Empty lines and lines containing only whitespace between
  records MUST be skipped. The schema marker line (Section 4.4) and the
  column header MUST be the first two non-blank lines of the file, in that
  order.
- **Quoting.** Double-quote quoting per [RFC4180]. Embedded double quotes
  are escaped by doubling (`""`). A field containing a comma, a double
  quote, or a line terminator MUST be quoted.
- **Columns.** The column set and order defined above are frozen for v1.
  Extra columns, missing columns, or columns in a different order MUST
  cause the consumer to reject the file with an error identifying the
  divergence.

### 4.6 Unicode Normalization

All relative paths in a manifest, and the `extensions` filter comparisons
in Section 8.3, MUST be normalized to Unicode Normalization Form C (NFC)
before being used for filesystem lookups or comparisons. The
case-insensitive extension filter uses Unicode default case-folding
(locale-independent), so `.C` and `.c` collapse uniformly regardless of
locale (for example, Turkish-locale `I`/`ı` quirks are not observed).

A consumer MUST NOT depend on filesystem-native normalization, which
varies: NFC on Linux ext4, NFD on legacy macOS HFS+, APFS-specific
handling on newer macOS, and UTF-16 on Windows NTFS. Normalizing on the
consumer side makes directory digests reproducible across these platforms.

Component `name`, `version`, SPDX expressions in `license.expression`, and
`purl` strings are compared byte-exact and do **not** apply Unicode
normalization. Producers are responsible for using consistent encodings
across manifests that reference each other.

## 5. Manifest Types

This specification defines two manifest types. Every manifest file is
exactly one of them, identified by its schema marker (Section 4.4). For
every primary manifest a consumer processes, it emits one SBOM; components
manifests contribute a shared dependency pool consumed by every primary.

### 5.1 Primary Manifest

A **primary manifest** describes a single primary artifact — an executable,
firmware image, service, wheel, container, library release, or similar
buildable output — together with its top-level dependencies.

```json
{
  "schema": "primary-manifest/v1",
  "primary": { <Component object> }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema` | string | yes | MUST equal `primary-manifest/v1` |
| `primary` | Component object (Section 6) | yes | The primary artifact |

- `primary` is REQUIRED and MUST be a valid Component object per Section 6.
- `primary.name` MUST be non-empty.
- At least one of `primary.version`, `primary.purl`, or `primary.hashes`
  MUST be present and non-empty (Section 6.1).
- `primary.depends-on` — when present — names the primary's top-level
  dependencies, which MUST resolve to components in the pool (Section 10.4).
- A primary manifest MUST be serialized as JSON (Section 4.1).

No other top-level fields are defined in v1. A consumer MAY preserve
unknown top-level fields for round-tripping but MUST NOT give them semantic
meaning.

A project that builds multiple artifacts (several binaries from the same
repository, a set of microservices, multi-wheel Python packages) SHOULD
produce one primary manifest per artifact.

### 5.2 Components Manifest

A **components manifest** describes a pool of third-party components that
primary manifests in the same processing set can depend on.

```json
{
  "schema": "component-manifest/v1",
  "components": [ <Component object>, … ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema` | string | yes | MUST equal `component-manifest/v1` |
| `components` | array of Component | yes | One or more components (Section 6) |

A consumer MUST reject a components manifest whose `components` array is
absent or empty.

No other top-level fields are defined in v1. A consumer MAY preserve
unknown top-level fields for round-tripping but MUST NOT give them semantic
meaning.

### 5.3 Component Object Semantics on a Primary

The Component object (Section 6) applies uniformly to the `primary` field
of a primary manifest and to every entry of a components manifest's
`components[]` array. Some fields have no effect when set on a primary and
a conforming consumer MUST ignore them:

- `scope` — a primary is the artifact itself, not a dependency of something
  else. Setting `scope` on a primary has no meaning.
- `tags` — tag filtering (if a consumer provides it) applies to pool
  components; primaries are processed individually, not filtered out.

Fields that apply meaningfully to a primary:

- `name`, `version`, `type`, `description` — primary identity.
- `supplier`, `license`, `purl`, `cpe`, `external_references`, `hashes` —
  primary metadata.
- `pedigree` — when the primary is itself a fork of some upstream artifact.
- `depends-on` — the root of the primary's dependency graph (Section 10.4).
- `bom-ref` — an explicit bom-ref override, serialized as
  `metadata.component.bom-ref` in the CycloneDX output.

## 6. Component Object

A component is a JSON object with the following fields.

### 6.1 Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The component's name. MUST be non-empty. |

In addition to `name`, at least one of `version`, `purl`, or `hashes` MUST be
present and non-empty. A component carrying only `name` is not conforming:
the consumer would have no way to distinguish it from another component
sharing the same name. Vendored source snippets that do not carry upstream
versioning (for example, rolling backport headers) MAY omit `version`
provided they carry `purl` or `hashes`.

Throughout this specification, "non-empty" applied to a string means a string
of length ≥ 1 after stripping leading and trailing whitespace. Applied to an
array, it means an array of length ≥ 1 in which every element is itself
conforming to the rules for that field.

### 6.2 Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `bom-ref` | string | Explicit bom-ref override. See Section 15.1. |
| `version` | string | The component's version. Required unless `purl` or `hashes` is present (Section 6.1). |
| `type` | string | Component type. See Section 7.1. Default: `library`. |
| `description` | string | Human-readable description. |
| `supplier` | object | `{ name, email?, url? }`. `name` MUST be non-empty when `supplier` is present. |
| `license` | string or object | See Section 6.3. |
| `purl` | string | Package URL. See Section 6.4 and Section 9.3. |
| `cpe` | string | CPE 2.3 identifier. |
| `external_references` | array of object | `[{ type, url, comment? }]`. See Section 7.3. |
| `hashes` | array of object | See Section 8. |
| `scope` | string | See Section 7.2. Default: `required`. |
| `pedigree` | object | See Section 9. |
| `depends-on` | array of string | See Section 10. |
| `tags` | array of string | Producer-side tags that drive per-build filtering of pool components. Not serialized in the output SBOM. Ignored when set on a primary (Section 5.3). |
| `lifecycles` | array of `{ phase }` | Primary-only. See Section 7.5. Ignored when set on a pool component. |

A consumer MUST NOT reject a manifest for containing fields not listed in this
section; unknown component fields MUST be ignored for semantic purposes but
MAY be preserved for round-tripping.

### 6.3 License Field

v1 diverges deliberately from CycloneDX in how licensing is modelled. A
component carries **one SPDX License Expression** describing the component's
overall licensing, plus optional per-license text references attached to the
individual SPDX IDs that appear in the expression. The CycloneDX alternative
of a free-form `license.name` for non-SPDX licenses is **not supported**; v1
accepts only valid SPDX License Expressions [SPDX-Expressions].

The `license` field is either a string or an object:

```json
"license": "MIT"
```

```json
"license": {
  "expression": "MIT"
}
```

```json
"license": {
  "expression": "Apache-2.0 OR MIT"
}
```

```json
"license": {
  "expression": "MIT AND BSD-3-Clause",
  "texts": [
    { "id": "MIT", "file": "./LICENSE-MIT" },
    { "id": "BSD-3-Clause", "text": "…inline license text…" }
  ]
}
```

- **String form.** `"license": "<expression>"` is shorthand for
  `{ "expression": "<expression>" }` with no texts.
- **`expression`.** REQUIRED when the object form is used. MUST be a valid
  SPDX License Expression [SPDX-Expressions]. A consumer MUST NOT silently
  normalize, rewrite, or alter the expression; the value is passed through to
  the output SBOM verbatim. A consumer MAY validate the expression against
  the SPDX grammar; if it does, an invalid expression MUST be rejected.
- **`texts`.** OPTIONAL. An array of per-license text references. Each entry
  carries an `id` (a simple SPDX License Identifier, not an expression) plus
  exactly one of `text` (inline text) or `file` (a path resolved per
  Section 4.3). Each entry's `id` MUST appear as a simple identifier within
  `expression`; a consumer MUST reject a `texts[].id` that does not appear in
  the expression.

Identifiers and expressions are case-sensitive per the SPDX specification
(for example, `MIT` is valid but `mit` is not).

Dual-licensing (`"MIT OR Apache-2.0"`, user chooses) is expressed with `OR`;
compound licensing where both apply (`"IJG AND BSD-3-Clause"`) is expressed
with `AND`. Consumers MUST NOT infer the operator from a list; it is always
explicit in `expression`.

### 6.4 Purl Field

The `purl` field, when present, MUST be a valid Package URL per [purl-spec].
For patched and vendored components, additional constraints apply; see
Section 9.3.

## 7. Enumerations

Enumeration values are case-sensitive.

### 7.1 Component Type

Allowed values for `component.type`:

```
library (default)
application
framework
container
operating-system
device
firmware
file
platform
device-driver
machine-learning-model
data
```

These values mirror CycloneDX 1.7 `component.type`.

### 7.2 Scope

Allowed values for `component.scope`:

| Value | Meaning |
|-------|---------|
| `required` (default) | The component's code logic is present at runtime and is reached under normal operation. |
| `optional` | The component's code logic is present at runtime but may not be reached (feature flag off, platform conditional, plugin not loaded). |
| `excluded` | The component is not present at runtime: it produces artifacts at build time but does not itself run in the output. |

**Decision rule.** Scope turns on whether the component's **code logic is
present at runtime**, not on whether its source files are in the artifact:

- If the component's logic is present at runtime — directly compiled in,
  inlined via headers, or code generated from it that embodies its
  semantics — use `required` or `optional`.
- If the component is a tool that produces artifacts but does not itself
  run in the output — compilers, code generators such as SWIG or protoc,
  build-time template expanders, test harnesses — use `excluded`.
- **Header-only libraries** whose inlined code ends up in the binary are
  `required` or `optional`, not `excluded`, because their code logic IS
  present at runtime.
- **Bindings generators** such as `pybind11` are `excluded`: the runtime
  behaviour lives in the generated glue code, not in the generator.

The distinction between `required` and `optional` then turns on runtime
reachability under the artifact's normal operation.

A consumer MUST drop components with `scope: excluded` before emitting the
SBOM. Build-time dependencies are `excluded`, not `optional`. A producer
MUST NOT use `optional` to denote a build-time-only dependency.

### 7.3 External Reference Type

Allowed values for `external_references[].type`:

```
website
vcs
documentation
issue-tracker
distribution
support
release-notes
advisories
other
```

These mirror CycloneDX 1.7 `externalReferences.type`.

### 7.4 Patch Type

Allowed values for `pedigree.patches[].type`:

```
unofficial
monkey
backport
cherry-pick
```

### 7.5 Lifecycle Phase

Allowed values for `lifecycles[].phase` on a primary:

```
design
pre-build
build
post-build
operations
discovery
decommission
```

Lifecycle phases MAY be set on a primary via an optional `lifecycles` array
(Section 6.2). When present, they serialize to `metadata.lifecycles` in the
emitted CycloneDX SBOM. When absent, the default `[{ "phase": "build" }]`
applies — the SBOM was produced at build time from source manifests. The
`lifecycles` field has no effect on pool components and MUST be ignored
there.

## 8. Hashes

The `hashes` field is an array of hash entries. Each entry MUST be in exactly
one of three forms.

**File-size limit.** Whenever this specification directs a consumer to read
a file referenced by a manifest — `license.file` (Section 6.3),
`license.texts[].file` (Section 6.3), `hashes[].file` (Section 8.2),
`hashes[].path` resolving to a regular file (Section 8.3), patch `diff.url`
pointing at a local path (Section 9.2) — the consumer SHOULD enforce a
maximum size per read. The RECOMMENDED default is 10 MiB. A file exceeding
the configured limit MUST cause the consumer to fail with an error naming
the offending path; silent truncation MUST NOT occur. This applies uniformly
to all file reads described elsewhere in this document. The directory-walk
form (Section 8.4) applies the limit per regular file encountered during
the walk.

### 8.1 Literal Form

```json
{ "algorithm": "SHA-256", "value": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
```

The `value` is a lowercase hexadecimal digest. The consumer MUST pass the value
through to the output SBOM without recomputation.

Permitted algorithms in all hash forms (literal, file, and directory) are
restricted to **SHA-256**, **SHA-384**, **SHA-512**, **SHA-3-256**, and
**SHA-3-512**. MD5, SHA-1, and any other algorithm MUST be rejected. This
restriction applies uniformly: weaker algorithms are not safe for
supply-chain integrity attestation regardless of who computed the digest.

### 8.2 File Form

```json
{ "algorithm": "SHA-256", "file": "./parser.c" }
```

The `file` path is resolved per Section 4.3. The consumer MUST:

1. Read the file's bytes.
2. Compute the digest using the declared algorithm.
3. Emit the resulting hex-encoded digest as `value` in the output SBOM.

If the path does not resolve to a regular file, the consumer MUST reject the
manifest with an error naming the offending path.

Permitted algorithms are defined in Section 8.1.

### 8.3 Directory Form

```json
{ "algorithm": "SHA-256", "path": "./src/vendor/", "extensions": ["c", "h"] }
```

The `path` is resolved per Section 4.3 and MAY point to a regular file or a
directory:

- If `path` resolves to a regular file, behavior is identical to the File Form
  (Section 8.2); the `extensions` field, if present, is ignored.
- If `path` resolves to a directory, the directory digest algorithm (Section
  8.4) applies.

Permitted algorithms are defined in Section 8.1.

The optional `extensions` field is an array of strings. Each string MAY begin
with `*.` or `.`, both of which are stripped; comparison is case-insensitive.
When `extensions` is present, only regular files whose name, lowercased, ends
with `.<ext>` for some listed extension are included. When absent, every
regular file is included.

### 8.4 Directory Digest Algorithm

Given a directory `D`, an algorithm `H`, and an optional extension filter `F`,
the directory digest is computed as follows:

1. **Walk.** Enumerate all regular files under `D` recursively. During the
   walk, do **not** descend into any subdirectory whose basename starts with
   `.` (for example, `.git/`, `.venv/`, `.vscode/`); such subtrees are
   skipped entirely. Do not follow symbolic links encountered either as
   directories or as files during the walk.
2. **Filter.** Exclude any file whose basename starts with `.`
   (hidden files at the walked levels). If `F` is present, exclude any file
   whose lowercased name does not end with `.<ext>` for some `ext` in `F`.
3. **Empty check.** If no files remain, the consumer MUST reject the manifest
   with an error. Empty directory digests are not permitted.
4. **Per-file digest.** For each remaining file, compute `H` over its bytes
   and record the hex-encoded digest.
5. **Manifest.** Build a manifest string in which each file contributes one
   line: `<hex-digest><SP><SP><relative-path><LF>`. The relative path is the
   file's path relative to `D`, expressed with forward slashes (`/`) regardless
   of host operating system. Lines MUST be sorted lexicographically by the
   relative path (byte-wise comparison, UTF-8 encoding).
6. **Final digest.** Compute `H` over the UTF-8 encoding of the manifest string.
   Emit this hex-encoded value as the hash `value` in the output SBOM.

This algorithm is deterministic: given the same input directory, the same set
of non-hidden, non-symlink files, the same algorithm, and the same filter, the
computed digest is identical across filesystems, operating systems, and runs.

### 8.5 Multiple Hash Entries

A component MAY carry multiple hash entries, including a mixture of the three
forms above (for example, a frozen upstream literal digest plus a live digest
computed over a vendored directory). A consumer MUST process each entry
independently.

## 9. Pedigree

The `pedigree` object declares the provenance of a vendored or modified
component. Its shape mirrors CycloneDX 1.6 `pedigree`:

```json
"pedigree": {
  "ancestors": [{ "purl": "pkg:github/upstream-org/upstream-lib@2.4.0" }],
  "descendants": [],
  "variants": [],
  "commits": [{ "uid": "abc123", "url": "https://…" }],
  "patches": [
    {
      "type": "backport",
      "diff": { "text": "…", "url": "./patches/fix.patch" },
      "resolves": [{ "type": "security", "name": "CVE-2024-XXXXX", "url": "…" }]
    }
  ],
  "notes": "…"
}
```

### 9.1 Ancestor Reference

An ancestor entry follows the same identity rules as a Component
(Section 6.1): `name` is REQUIRED, and at least one of `version`, `purl`,
or `hashes` MUST be present and non-empty. An ancestor MAY carry any other
Component field (supplier, external_references, etc.) to record upstream
metadata; a consumer MUST preserve all ancestor fields in the output SBOM.

### 9.2 Patches

Patch `type` MUST be one of the values in Section 7.4.

A patch `diff` object MAY contain `text`, `url`, or both.

The `text` field carries the content of the diff. It MAY take either of two
forms:

- **String form.** A bare string holds the diff content verbatim, interpreted
  as raw UTF-8 text.
- **Attachment form.** An object `{ content, encoding?, contentType? }`
  matching the CycloneDX Attachment shape. The `content` field is REQUIRED.
  The `encoding` field, when present, MUST be `base64`; a consumer MUST
  base64-decode `content` to recover the diff bytes. The `contentType` field,
  when present, is a MIME type recorded in the output SBOM and otherwise
  ignored.

A consumer MUST emit `diff.text` as a CycloneDX Attachment in the output
SBOM. String-form input is emitted as `{ content: <text>, contentType:
"text/plain" }`; attachment-form input is emitted with all supplied fields
preserved.

The `url` field MAY be either a local relative path or an `http://` /
`https://` URL:

- If `url` resolves to a local relative path (Section 4.3), the consumer
  MUST read the file's bytes, base64-encode them, and emit the content as
  `diff.text = { content: <base64>, encoding: "base64" }` in the output
  SBOM. This rule handles both textual and binary patch files uniformly and
  produces a deterministic byte-exact record of the patch.
- If `url` is an `http://` or `https://` URL, the consumer MUST leave it
  as-is and MUST NOT fetch it.

If both `text` and `url` are present, the consumer MUST preserve both in the
output SBOM.

### 9.3 Purl on Patched Components

**A component whose `pedigree.ancestors[]` is non-empty MUST NOT have a
top-level `purl` equal to any `pedigree.ancestors[i].purl`.**

All purl comparisons in this specification — the patched-component rule here,
`depends-on` resolution (Section 10.2), and identity matching (Section 11) —
MUST be performed on the **canonical form** defined by the purl-spec
[purl-spec]. A consumer MUST normalize both sides to canonical form
(lowercasing scheme/type/namespace where the purl-spec mandates it; ordering
qualifiers lexicographically; applying any type-specific normalization
rules) before comparison. Naive string comparison is not sufficient.

This constraint is a correctness rule, not a quality gate: a patched copy of
upstream software is not the upstream software, and sharing its identifier
misleads vulnerability scanners. A consumer MUST reject a manifest violating
this rule.

Conforming patterns for patched components:

1. **Repository-local purl.** `"purl": "pkg:github/acme/device-firmware/src/vendor-lib@2.4.0"`
2. **Upstream purl with qualifier.** `"purl": "pkg:github/upstream-org/upstream-lib@2.4.0?vendored=true"`
3. **Omit `purl`.** Identity falls back to `name@version` (Section 11).

## 10. Dependency Model

### 10.1 Direction

Dependencies are expressed as `depends-on`: a component lists the components
it depends upon. The reverse direction is not defined in v1.

### 10.2 Reference Format

Each entry in `depends-on` is one of two forms, distinguished by prefix:

- A **Package URL**, identified by the `pkg:` scheme prefix. The reference
  is matched against candidate components' `purl` values using the
  canonical-form comparison rule from Section 9.3 (normalize both sides
  before comparing). A string beginning with `pkg:` but failing to parse
  as a valid purl per [purl-spec] is a hard error.
- A **`name@version` reference**, used when the referenced component has
  no `purl`. The parse splits the string on the **last** `@` character:
  the portion before the last `@` is `name`, the portion after is
  `version`. This rule correctly handles scoped ecosystem identifiers such
  as `@angular/core@1.0.0` (name `@angular/core`, version `1.0.0`).
  Matching against a candidate component is byte-exact and
  case-sensitive on both `name` and `version`; producers are responsible
  for consistent casing.

A reference that fits neither form (for example, a bare name with no `@`,
or any string containing whitespace) MUST cause the consumer to fail with
an error identifying the offending entry.

### 10.3 Resolution

Dependency references are resolved against the merged components pool
(Section 12.2). A reference that does not resolve MUST produce a warning;
the unresolved edge MUST be dropped from the output SBOM but the referring
component MUST be preserved.

### 10.4 Top-Level Dependencies and Reachability

When a consumer emits an SBOM for a given primary manifest, the root of
that SBOM's dependency graph is the primary. The primary's `depends-on`
array names its direct dependencies in the pool.

Behaviour depends on how many primary manifests are in the processing set
(Section 12.1):

**Single-primary processing set.** If exactly one primary manifest is
being processed in the current run:

- If `primary.depends-on` is present and non-empty, the SBOM's components
  are the transitive closure reachable from those references. Pool
  components that are not reachable MUST be omitted from the SBOM, and the
  consumer MUST emit one warning per unreachable pool component.
- If `primary.depends-on` is absent or empty, the consumer MUST treat every
  pool component (across all components manifests in the processing set) as
  a direct dependency of the primary. This is a convenience for small
  projects with a single artifact: drop one primary manifest and one
  components manifest into a tree, done.

**Multi-primary processing set.** If two or more primary manifests are
being processed in the current run:

- Each primary manifest's `primary.depends-on` MUST be present and
  non-empty. An absent or empty `depends-on` on any primary in this case
  is a hard error.
- Each primary's SBOM components are the transitive closure reachable from
  that primary's `depends-on`.
- Pool components that are not reachable from a given primary MUST be
  omitted from that primary's SBOM, and the consumer MUST emit one warning
  per unreachable pool component per SBOM run, identifying the component
  and the primary currently being processed.
- Pool components that are not reachable from *any* primary in the
  processing set are unused; the consumer MUST emit one warning per such
  orphan, once for the run (not per SBOM).

A pool component whose own `depends-on` field is absent is a leaf node: it
has zero outgoing edges. A pool component whose `depends-on` is present but
empty has the same semantics.

## 11. Identity and Uniqueness

A component's identity is, in order of precedence:

1. Its `purl`, if present and non-empty; otherwise
2. The pair `(name, version)`, if `version` is present and non-empty;
   otherwise
3. The singleton `name`.

The identity rules apply uniformly to pool components and to the primary of
a primary manifest. Within a single SBOM emission (one primary manifest
plus its reachable pool components), a primary's identity MUST be distinct
from every pool component's identity. If the primary and a reachable pool
component share an identity, the consumer MUST reject the manifest set.

Within the merged components pool (Section 12.2), a consumer MUST
deduplicate by identity:

- If two components share the same `purl`, the consumer MUST emit a warning
  and keep the first occurrence (by merge order).
- If two components share the same `(name, version)` but declare different
  `purl` values, the consumer MUST treat them as distinct and MUST emit a
  warning noting the likely upstream collision.
- If a component has no `purl` and another component with the same
  `(name, version)` also has no `purl`, the consumer MUST emit a warning and
  keep the first occurrence.
- If two components share `name`, neither declares `purl`, and neither
  declares `version`, the consumer MUST emit a warning and keep the first
  occurrence.

After the direct-identity pass above, a consumer MUST perform a secondary
cross-check for mixed purl / no-purl duplicates:

1. Build an index of components whose identity is a `purl`, keyed by the
   pair `(name, version)` of each such component (using the component's
   own `name` and `version` fields, not any fields decoded from the purl).
2. For every remaining component whose identity fell through to
   `(name, version)` — that is, a component without a `purl` — look up
   the same `(name, version)` pair in the index.
3. On a match, emit a warning identifying both components and merge the
   no-purl component into the purl-bearing one. The purl-bearing
   component's `purl` is kept (as richer identity); any `hashes`,
   `pedigree`, `depends-on`, or other fields unique to the no-purl
   component MUST be preserved on the merged record, with conflicts
   between the two resolved by keeping the purl-bearing component's
   values and emitting a warning naming each conflicting field.

This pass catches the common case of the same library declared with and
without a purl across different input files. A component whose identity
is just `name` (no `version`, no `purl`) is not matched by the secondary
pass; producers facing that case are responsible for ensuring uniqueness
manually.

## 12. Multi-File Composition

### 12.1 Processing Set

A **processing set** is the collection of manifest files a consumer is
preparing to process in a single run. On reading the files, a consumer MUST
partition the processing set by schema marker (Section 4.4):

- **Primary manifests** (marker `primary-manifest/v1`) — each produces
  one SBOM output.
- **Components manifests** (marker `component-manifest/v1`) — merged into
  a single shared pool consumed by every primary in this run.

A processing set MUST contain at least one primary manifest; a run with
zero primaries is a hard error, because there is nothing to emit an SBOM
for.

### 12.2 Pool Construction

A consumer MUST build the shared components pool once per run:

1. Read each components manifest independently.
2. Concatenate all `components[]` arrays into a single pool, preserving
   input order.
3. Apply identity deduplication across the pool (Section 11).
4. Resolve `depends-on` references across the pool (Section 10.3).

### 12.3 Per-Primary Emission

For each primary manifest in the processing set, a consumer MUST:

1. Take the shared pool from Section 12.2 (constructed once, reused across
   primaries).
2. Compute the transitive closure of pool components reachable from
   `primary.depends-on`, per the reachability rules in Section 10.4. The
   single-primary convenience rule applies only when the processing set
   contains exactly one primary manifest.
3. Emit one SBOM whose `metadata.component` (CycloneDX) or DESCRIBES
   target (SPDX) is the primary, and whose `components[]` is the reachable
   closure.
4. Emit warnings per Section 10.4 for pool components unreachable from
   this primary.

### 12.4 Relative Path Scope

Each relative path inside a manifest MUST be resolved against that
manifest's own directory (Section 4.3). A consumer MUST NOT resolve paths
in one manifest against another manifest's directory.

### 12.5 Discovery (Non-Normative)

Discovery of manifest files by directory traversal is an implementation
concern and is **not part of this specification**. The choice of traversal
order, symbolic-link handling, and directory exclusions (`.git/`,
`node_modules/`, `vendor/`, etc.) is implementation-defined. Implementations
that perform discovery SHOULD document their discovery semantics and SHOULD
produce deterministic output given a fixed input tree.

Regardless of how a consumer obtains its processing set, the following
rules apply when processing each file (and are normative):

- A file without a schema marker (Section 4.4) MUST be ignored silently.
- A file with a marker matching `primary-manifest/*` or
  `component-manifest/*` that cannot be parsed, fails validation, or
  declares a version other than `v1` MUST cause the consumer to fail with
  an error identifying the file.
- A file referenced by hash `file`, hash `path`, license `file`, or patch
  `diff.url` that does not exist MUST cause the consumer to fail with an
  error identifying the reference.

## 13. Validation

A consumer MUST apply validation in two layers:

### 13.1 Structural Validation

The manifest MUST satisfy the JSON Schema in Appendix A. A consumer that fails
structural validation MUST reject the manifest.

### 13.2 Semantic Validation

After structural validation, a consumer MUST apply the following semantic rules
and MUST reject the manifest on any violation:

- Schema marker equals `primary-manifest/v1` or `component-manifest/v1`, matching the manifest's top-level shape (Section 4.4, Section 5).
- Every component carries a non-empty `name` and at least one of `version`,
  `purl`, or `hashes` (Section 6.1).
- Every hash entry matches exactly one form (Section 8).
- Every file-form or path-form hash resolves to an existing filesystem entry
  of the required type (Section 8.2, 8.3).
- Every directory-form hash produces at least one included file (Section 8.4).
- Hash algorithms in all forms are restricted to the permitted set in
  Section 8.1 (SHA-256, SHA-384, SHA-512, SHA-3-256, SHA-3-512).
- No relative path escapes the manifest's directory without explicit consumer
  configuration (Section 4.3, Section 18).
- The patched-purl rule holds for every component with a non-empty pedigree
  ancestor list (Section 9.3).
- Every enumeration value is in the allowed set (Section 7).

A consumer MAY apply additional quality checks (for example, warning on
components lacking a license or a hash). Such checks are implementation-defined
and are not part of this specification.

### 13.3 Warning Channel

Every warning required or permitted by this specification (identity
collisions in Section 11, unresolved `depends-on` references in Section 10.3,
SPDX field drops in Section 14.2, symlink skips during directory walks in
Section 8.4, etc.) MUST be emitted by the consumer to the standard error
stream (stderr) as one human-readable line per warning, prefixed with
`warning:`. Implementations MAY additionally expose machine-readable warning
output (JSON log, annotation file, non-zero exit code) but the stderr
line-oriented channel is the baseline contract so that CI tooling can
uniformly tee and inspect warnings.

## 14. Serialization to SBOM Formats

A conforming consumer MUST be capable of emitting at least CycloneDX. SPDX
emission is OPTIONAL.

### 14.1 CycloneDX Mapping

Manifest inputs map to CycloneDX outputs as follows:

| Input | CycloneDX output |
|-------|------------------|
| Primary manifest `primary` object | `metadata.component` |
| Reachable pool components (Section 10.4) | `components[]` |
| Primary `depends-on` | `dependencies[].dependsOn` on the entry whose `ref` matches the primary's `bom-ref` |
| Pool `depends-on` | `dependencies[].dependsOn` on each pool component's entry |

Component field mapping (applies uniformly to the primary and to each pool
component):

| Component field | CycloneDX field |
|-----------------|-----------------|
| `bom-ref` (explicit or derived) | `components[].bom-ref` (pool) or `metadata.component.bom-ref` (primary) |
| `name` | `.name` |
| `version` | `.version` |
| `type` | `.type` |
| `description` | `.description` |
| `supplier` | `.supplier` |
| `license` | `.licenses[]` (CDX `expression` form when compound; `license.id` + `license.text` per-ID entries for `texts[]`) |
| `purl` | `.purl` |
| `cpe` | `.cpe` |
| `external_references` | `.externalReferences` |
| `hashes` | `.hashes` |
| `scope` | `.scope` (pool components only; ignored on primary, Section 5.3) |
| `pedigree` | `.pedigree` |
| `tags` | not serialized |

A consumer MUST emit a `bom-ref` for the primary and for every pool
component in the SBOM per Section 15.1.

### 14.2 SPDX Projection

A consumer emitting SPDX 2.3 MUST apply the following projection. Fields
labeled **dropped** MUST be omitted from the output with a warning.

In the SPDX output, the primary becomes the package referenced by the
document's `DESCRIBES` relationship; reachable pool components become
packages linked to the primary (and to each other) via `DEPENDS_ON`
relationships. The field-level mapping below applies to each package
regardless of whether it originates from a primary or from the pool.

| Manifest field | SPDX 2.3 target | Fidelity | Notes |
|----------------|-----------------|----------|-------|
| `name` | `package.name` | lossless | |
| `version` | `package.versionInfo` | lossless | |
| `description` | `package.description` | lossless | |
| `type` | `package.primaryPackagePurpose` | approximated | SPDX 2.3 covers `library`, `application`, `framework`, `container`, `operating-system`, `device`, `firmware`, `file` losslessly. Values without a match (`platform`, `device-driver`, `machine-learning-model`, `data`) project to `OTHER`. |
| `supplier` | `package.supplier` | lossless | |
| `license.expression` | `package.licenseConcluded` and `package.licenseDeclared` | lossless | SPDX expression passes through verbatim. |
| `license.texts[].text` / `license.texts[].file` | `package.licenseComments` (concatenation with `id`-label headings) | approximated | SPDX 2.3 has no per-ID license-text attachment slot; texts are combined into `licenseComments`. |
| `purl` | `package.externalRefs` (`PACKAGE-MANAGER` / `purl`) | lossless | |
| `cpe` | `package.externalRefs` (`SECURITY` / `cpe23Type`) | lossless | |
| `external_references[type=website]` | `package.homepage` | lossless | |
| `external_references[type=distribution]` | `package.downloadLocation` | lossless | |
| `external_references[type=vcs]` | `package.externalRefs` (`OTHER` / `vcs`) | approximated | |
| `external_references` (other types) | `package.externalRefs` (`OTHER`) with type recorded in comment | approximated | |
| `hashes` (SHA-256, SHA-512) | `package.checksums` | lossless | |
| `depends-on` | `DEPENDS_ON` relationship | lossless | |
| `pedigree.ancestors` | `package.sourceInfo` (free text, one line per ancestor) | approximated | |
| `pedigree.patches` | `package.annotations` (one per patch) | approximated | |
| `pedigree.commits` | appended to `package.sourceInfo` | approximated | |
| `pedigree.notes` | `package.comment` | lossless | |
| `scope` | — | **dropped** | SPDX 2.3 has no runtime-presence concept. |
| `pedigree.variants` | — | **dropped** | |
| `pedigree.descendants` | — | **dropped** | |
| `metadata.lifecycles` (application) | — | **dropped** | |
| `tags` | — | not serialized | |

A consumer emitting SPDX MUST emit one warning per dropped field class.

## 15. Determinism

A conforming consumer MUST produce byte-identical SBOM output when invoked
twice on identical inputs. Specifically:

### 15.1 bom-ref Derivation

Each component's CycloneDX `bom-ref` MUST be derived as, in order of
precedence:

1. The component's explicit `bom-ref` field, if present and non-empty;
   otherwise
2. The component's `purl`, if present; otherwise
3. `pkg:generic/<encoded-name>@<version>`, where `<encoded-name>` is the
   `name` with each character outside the unreserved set
   `[A-Za-z0-9._~-]` (as defined by [RFC3986] Section 2.3) percent-encoded.
   Percent-encoding preserves distinctness — `"foo bar"` becomes
   `pkg:generic/foo%20bar@1.0` while `"foo-bar"` stays
   `pkg:generic/foo-bar@1.0` — and produces a valid URI and a valid purl
   `name` component. If `version` is absent, the `@<version>` suffix is
   omitted.

An explicit `bom-ref` MAY be any string. It need not be a valid Package URL.
This accommodates patterns such as sub-component references of a parent
artifact (for example, a parent purl with a URL fragment naming an
internal extension module: `pkg:pypi/example@1.0.0#module/_core`), or
compact reference identifiers for optional native dependencies that ship
outside the artifact.

`bom-ref` participates only in serialization: it does not affect identity
or deduplication (Section 11). Two components with distinct `bom-ref`
values but the same identity are still duplicates.

If two components resolve to the same `bom-ref`, the consumer MUST reject the
manifest.

### 15.2 Ordering

- `components[]` MUST be emitted sorted by `bom-ref` ascending.
- `dependencies[].dependsOn` arrays MUST be sorted by ref ascending.
- `components[].hashes`, `components[].externalReferences`, and any `tags`
  array MUST be sorted: hashes by `algorithm` then `value`; external references
  by `type` then `url`; tags lexicographically.

### 15.3 Timestamp and Serial Number

If the `SOURCE_DATE_EPOCH` environment variable is set to a non-negative
integer of seconds since the Unix epoch [SOURCE-DATE-EPOCH], a consumer MUST:

- Set `metadata.timestamp` (CycloneDX) or `creationInfo.created` (SPDX) to
  that time, rendered in ISO 8601 UTC with second precision.
- Derive `serialNumber` (CycloneDX) deterministically from the canonical
  representation of the emitted SBOM's `components[]` array, so repeated
  runs against the same inputs produce the same serial number.

The deterministic `serialNumber` MUST be `urn:uuid:<UUID>` where `<UUID>` is
a version 5 UUID [RFC4122] computed as follows:

- **Namespace.** The DNS namespace UUID `6ba7b810-9dad-11d1-80b4-00c04fd430c8`
  (defined in [RFC4122] Appendix C).
- **Name.** The UTF-8 string formed by concatenating the fixed prefix
  `component-manifest/v1/serial/` with the lowercase hex-encoded
  SHA-256 digest of the canonical JSON representation of the output SBOM's
  `components[]` array.
- **Canonical JSON.** The JSON Canonicalization Scheme defined by
  [RFC8785]. Every conforming consumer MUST apply JCS to the sorted
  components array (Section 15.2) before digesting.

If `SOURCE_DATE_EPOCH` is unset, the consumer MAY use the current wall-clock
time for the timestamp and MAY generate a version 4 (random) `serialNumber`.

### 15.4 File-Based Hashes

File-based hashes (Section 8.2, 8.3) MUST be computed over the file contents
as they exist on disk at generation time. Producers seeking reproducibility
are responsible for ensuring hashed files do not change between runs.

## 16. Conformance

### 16.1 Conforming Manifest

A primary manifest is conforming if it satisfies all MUST-level
requirements of Sections 4, 5.1, 5.3, 6, 7, 8, 9, 10, and 13 that apply to
a primary manifest.

A components manifest is conforming if it satisfies all MUST-level
requirements of Sections 4, 5.2, 6, 7, 8, 9, 10, and 13 that apply to a
components manifest.

### 16.2 Conforming Producer

A producer is conforming if every manifest it writes is a conforming
manifest. In addition, a conforming producer SHOULD:

- Emit a `purl` for every component whose ecosystem has a registered purl
  type (pypi, npm, maven, cargo, golang, generic, github, etc.).
- Emit a `supplier` block when a supplier is known — at minimum the
  supplier's `name`.
- Prefer `hashes` in file or directory form (Sections 8.2, 8.3) over
  literal form when the component's source is present in the repository,
  so that the emitted digest stays in sync with the source.
- For vendored or patched components: set `purl` to a repository-local
  form per Section 9.3, and populate `pedigree.ancestors[]` with the
  upstream `purl` and the specific upstream tag or commit.
- Use the object form of `license` (`{ expression, texts? }`) whenever
  per-license text is attached, rather than mixing the string shorthand
  with any implicit texts.
- Emit one manifest file per source-code directory that owns a distinct
  set of third-party components (per Section 12), rather than listing
  every component in a single repository-root manifest.
- Name primary manifests `.primary.json` and components manifests
  `.components.json` (or `.components.csv` for CSV) for discoverability by
  discovery tools that glob by default. The manifest type itself is
  identified by the schema marker (Section 4.4), not the
  filename, so this convention is non-binding; projects whose layout
  requires different names (e.g. multiple manifests in a single directory,
  or alignment with an existing repo convention) are free to choose them.

These producer SHOULDs are quality guidance. A consumer MUST NOT reject a
manifest for failing to follow them; they exist so that reference
implementations converge on the same authorial conventions over time.

### 16.3 Conforming Consumer

A consumer is conforming if it:

- Correctly parses every conforming manifest of either type (Sections 4, 5).
- Correctly applies structural and semantic validation (Section 13).
- Correctly partitions a processing set, constructs the shared pool, and
  applies per-primary emission (Section 12).
- Correctly applies identity deduplication (Section 11), reachability and
  unreachable-component warnings (Section 10.4).
- Correctly emits one CycloneDX SBOM per primary manifest per Section 14.1
  and the determinism rules per Section 15.
- If it advertises SPDX support, correctly applies the SPDX projection per
  Section 14.2, emitting one SPDX document per primary manifest.

A consumer SHOULD validate its emitted SBOM against the target spec's
published JSON Schema (for example, `check-jsonschema` against
`https://cyclonedx.org/schema/bom-1.7.schema.json`) before writing the
output. Failing to validate does not render the consumer non-conforming but
is a strong signal of an upstream bug.

A consumer MAY implement extensions (additional warnings, quality gates,
output formats beyond CycloneDX and SPDX) but MUST NOT alter the semantics of
conforming manifests.

## 17. Versioning

The schema markers `primary-manifest/v1` and `component-manifest/v1`
identify this version of the specification. Additive, backward-compatible
changes to this document
(clarifications, new optional fields, new enumeration values where an explicit
extension point is defined) MAY be made without changing the marker.

A change is additive if every conforming v1 manifest under the prior revision
remains a conforming v1 manifest under the new revision, and every SBOM
produced under the prior revision remains a correct SBOM under the new
revision.

Non-additive changes — removal of fields, changes to required-field sets,
semantic redefinition of existing values, vocabulary realignment with a newer
CycloneDX version — MUST introduce a new schema marker
(`component-manifest/v2` and `primary-manifest/v2`, etc.) and MUST NOT be applied retroactively
to v1.

## 18. Security Considerations

**Threat model.** This specification treats component manifests as
**untrusted input** to the consumer. A manifest's trust boundary is the
manifest file itself plus the files it references by relative path. A
consumer's responsibilities under this model are:

- **Integrity of output.** The emitted SBOM MUST reflect the manifest's
  declarations, not injected or exfiltrated content.
- **Bounded I/O.** A manifest MUST NOT direct the consumer to read files
  outside the manifest's directory tree, follow symbolic links, make
  network requests, or execute code.
- **Resource bounds.** A consumer SHOULD refuse to process inputs that
  would consume unbounded memory or time (oversized patch files, runaway
  directory walks, pathological input).

Attackers in scope: a hostile or compromised contributor who submits a
pull request that modifies a manifest or adds new manifest files in a
repository processed by downstream consumers. Attackers out of scope: an
attacker with write access to the consumer's host filesystem, environment,
or binaries — at that point all bets are off, and no spec-level mechanism
can restore the invariants.

Sections 18.1 through 18.5 describe the specific mechanisms by which this
threat model is enforced.

### 18.1 Path Traversal

Relative paths in a manifest can reference files outside the manifest's
directory via `..` traversal. A consumer MUST reject such paths
unconditionally (Section 4.3); there is no configuration toggle. This
prevents a malicious or mistaken manifest from causing a consumer to read
arbitrary files on the host, and keeps the security guarantee spec-level
rather than implementation-level.

### 18.2 Symbolic Links

A consumer MUST NOT follow symbolic links when resolving any path referenced
by a manifest. Specifically:

- File reads for hash `file` (Section 8.2), hash `path` resolving to a
  regular file (Section 8.3), license `file` (Section 6.3), and patch
  `diff.url` local paths (Section 9.2) MUST be rejected with an error if the
  target path, or any directory component along the way from the manifest
  file's directory, is a symbolic link.
- The directory digest walk (Section 8.4) skips symbolic links encountered
  as files or directories during traversal.

A consumer MAY expose an opt-in configuration (e.g. a CLI flag) to follow
symbolic links, but this behavior is outside this specification and MUST NOT
be the default. The default refusal prevents two failure modes that
particularly affect SBOM generation: (a) a crafted or mistaken manifest
exfiltrating sensitive files into an SBOM by pointing a `license.file` or
`hash.file` at an absolute target via a symbolic link; (b) a directory
digest silently covering content outside the intended tree.

### 18.3 Network Fetches

A consumer MUST NOT fetch resources over the network while processing a
manifest. URLs in `external_references[].url`, `pedigree.patches[].diff.url`
(when `http://`/`https://`), and `pedigree.commits[].url` are recorded in the
output SBOM verbatim and are not dereferenced.

### 18.4 Manifest Trust

A consumer processes manifests as configuration, not as code. No field in a
manifest directs the consumer to execute code, spawn processes, or modify
filesystem state outside the output path. A producer MUST NOT rely on any
such side effect, because none is specified.

### 18.5 Hash Algorithm Choice

MD5, SHA-1, and any algorithm outside the permitted set (Section 8.1) are
forbidden in **all** hash forms — literal, file, and directory alike. Both
MD5 and SHA-1 have known collision weaknesses unsuitable for supply-chain
integrity attestation, and permitting them in literal form would undermine
the protection offered elsewhere. Producers importing legacy digests from
upstream manifests that still use MD5 or SHA-1 must either obtain a
stronger digest or omit the hash entirely rather than carry a weak one
forward.

## 19. References

### 19.1 Normative References

- **[RFC2119]** Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997.
- **[RFC8174]** Leiba, B., "Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words", BCP 14, RFC 8174, May 2017.
- **[RFC3986]** Berners-Lee, T., Fielding, R., and L. Masinter, "Uniform Resource Identifier (URI): Generic Syntax", STD 66, RFC 3986, January 2005.
- **[RFC4122]** Leach, P., Mealling, M., and R. Salz, "A Universally Unique IDentifier (UUID) URN Namespace", RFC 4122, July 2005.
- **[RFC4180]** Shafranovich, Y., "Common Format and MIME Type for Comma-Separated Values (CSV) Files", RFC 4180, October 2005.
- **[RFC8785]** Rundgren, A., Jordan, B., and S. Erdtman, "JSON Canonicalization Scheme (JCS)", RFC 8785, June 2020.
- **[purl-spec]** Package URL specification, https://github.com/package-url/purl-spec
- **[SPDX-Expressions]** SPDX License Expression syntax, https://spdx.github.io/spdx-spec/v2.3/SPDX-license-expressions/
- **[CycloneDX-1.7]** CycloneDX Bill of Materials Standard, Version 1.7, https://cyclonedx.org/specification/overview/
- **[SPDX-2.3]** The Software Package Data Exchange (SPDX) Specification, Version 2.3, https://spdx.github.io/spdx-spec/v2.3/
- **[SOURCE-DATE-EPOCH]** `SOURCE_DATE_EPOCH`, https://reproducible-builds.org/specs/source-date-epoch/

### 19.2 Informative References

- **[ECMA-424]** Ecma International, ECMA-424 1st edition: CycloneDX Bill of Materials Standard, June 2024.
- **[NTIA-Minimum]** The Minimum Elements For a Software Bill of Materials (SBOM), NTIA, July 2021.

## Appendix A: JSON Schema

> **TODO.** The canonical JSON Schema (draft 2020-12) for the component
> manifest is to be published alongside this document at
> `schemas/component-manifest-v1.schema.json`. Until that file is authored,
> this appendix is a placeholder and the structural-validation MUST in
> Section 13.1 cannot be fully satisfied. Drafting the schema is the
> highest-leverage remaining task before v1 is implementable.

When published, the JSON Schema will be the authoritative source for field
types, required-field sets, and enumeration membership; where this prose and
the schema disagree, the schema will govern for structural questions and
this prose will govern for semantics.

## Appendix B: Examples (Non-Normative)

### B.1 Minimal Primary Manifest

```json
{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "acme-server",
    "version": "1.0.0",
    "type": "application",
    "supplier": { "name": "Acme Corp" },
    "license": "Apache-2.0",
    "purl": "pkg:generic/acme/acme-server@1.0.0",
    "depends-on": [
      "pkg:generic/acme/libmqtt@4.3.0",
      "pkg:generic/openbsd/libtls@3.9.0"
    ]
  }
}
```

### B.2 Minimal Components Manifest

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libmqtt",
      "version": "4.3.0",
      "license": "EPL-2.0",
      "purl": "pkg:generic/acme/libmqtt@4.3.0",
      "hashes": [
        { "algorithm": "SHA-256", "value": "9f86d08..." }
      ]
    }
  ]
}
```

### B.3 Multi-Primary Project With Shared Pool

A repository that produces two independent artifacts (a server and a
command-line worker) from the same source tree. The processing set
contains two primary manifests and one components manifest; the consumer
emits two SBOMs.

Primary manifest `server/.primary.json`:

```json
{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "acme-server",
    "version": "1.0.0",
    "type": "application",
    "purl": "pkg:generic/acme/acme-server@1.0.0",
    "license": "Apache-2.0",
    "depends-on": [
      "pkg:generic/acme/libmqtt@4.3.0",
      "pkg:generic/openbsd/libtls@3.9.0"
    ]
  }
}
```

Primary manifest `worker/.primary.json`:

```json
{
  "schema": "primary-manifest/v1",
  "primary": {
    "name": "acme-worker",
    "version": "1.0.0",
    "type": "application",
    "purl": "pkg:generic/acme/acme-worker@1.0.0",
    "license": "Apache-2.0",
    "depends-on": [
      "pkg:generic/openbsd/libtls@3.9.0"
    ]
  }
}
```

Shared components manifest `.components.json`:

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libmqtt",
      "version": "4.3.0",
      "license": "EPL-2.0",
      "purl": "pkg:generic/acme/libmqtt@4.3.0"
    },
    {
      "name": "libtls",
      "version": "3.9.0",
      "license": "ISC",
      "purl": "pkg:generic/openbsd/libtls@3.9.0"
    }
  ]
}
```

The server's SBOM lists both `libmqtt` and `libtls`; the worker's SBOM
lists only `libtls`. Pool components not reachable from a primary are
omitted from that primary's SBOM with a warning.

### B.4 Dual-Licensed Native Dependency

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libjpeg-turbo",
      "version": "3.0.4",
      "type": "library",
      "scope": "optional",
      "license": {
        "expression": "IJG AND BSD-3-Clause",
        "texts": [
          { "id": "IJG", "file": "./licenses/IJG.txt" },
          { "id": "BSD-3-Clause", "file": "./licenses/BSD-3-Clause.txt" }
        ]
      },
      "purl": "pkg:generic/libjpeg-turbo@3.0.4",
      "external_references": [
        { "type": "website", "url": "https://libjpeg-turbo.org" }
      ]
    }
  ]
}
```

### B.5 Vendored Component with Pedigree, Directory Hash, and Patches

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "vendor-parser",
      "version": "2.4.0",
      "type": "library",
      "license": { "expression": "MIT", "texts": [{ "id": "MIT", "file": "./LICENSE" }] },
      "purl": "pkg:github/acme/device-firmware/src/vendor-parser@2.4.0",
      "hashes": [
        { "algorithm": "SHA-256", "path": "./src/vendor/", "extensions": ["c", "h"] }
      ],
      "pedigree": {
        "ancestors": [
          { "purl": "pkg:github/upstream-org/upstream-lib@2.4.0" }
        ],
        "patches": [
          {
            "type": "backport",
            "diff": { "url": "./patches/fix-int-overflow.patch" },
            "resolves": [
              { "type": "security", "name": "CVE-2024-XXXXX" }
            ]
          }
        ],
        "notes": "Forked at commit abc123; local fix for integer overflow."
      }
    }
  ]
}
```

### B.6 Sub-Component with Explicit bom-ref

Internal artifact parts that need their own dependency edges but share the
parent's identity. Useful for Python C extensions, Rust crates with feature
flags, or multi-binary Go modules.

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "bom-ref": "pkg:pypi/acme-imaging@1.2.0#c-ext/_core",
      "name": "acme_imaging._core",
      "version": "1.2.0",
      "type": "library",
      "description": "Core image processing C extension",
      "license": "Apache-2.0",
      "depends-on": [
        "pkg:generic/libjpeg-turbo@3.0.4",
        "pkg:generic/zlib@1.3.1"
      ]
    }
  ]
}
```

### B.7 Vendored Header Without Upstream Version

```json
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "bom-ref": "pkg:github/python/pythoncapi-compat",
      "name": "pythoncapi_compat",
      "type": "library",
      "description": "Rolling backport header for new CPython C-API functions",
      "license": "MIT-0",
      "hashes": [
        { "algorithm": "SHA-256", "file": "./src/thirdparty/pythoncapi_compat.h" }
      ],
      "external_references": [
        { "type": "vcs", "url": "https://github.com/python/pythoncapi-compat" }
      ]
    }
  ]
}
```

### B.8 CSV Components Manifest

```csv
#component-manifest/v1
name,version,type,description,supplier_name,supplier_email,license,purl,cpe,hash_algorithm,hash_value,hash_file,scope,depends_on,tags
libmqtt,4.3.0,library,Acme MQTT client,Acme Corp,,EPL-2.0,pkg:generic/acme/libmqtt@4.3.0,,SHA-256,9f86d08...,,required,libtls@3.9.0,"core,networking"
libtls,3.9.0,library,OpenBSD TLS,OpenBSD,,ISC,pkg:generic/openbsd/libtls@3.9.0,,SHA-256,e3b0c44...,,required,,"core,networking"
```

