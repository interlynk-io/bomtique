Goal: produce one or more SBOMs for THIS repository using the
locally-installed `bomtique` tool, by hand-authoring Component
Manifest v1 files (`.primary.json` + `.components.json`) that
describe the project's primary artifact(s) and their dependencies.

Operating constraints:

1. Do NOT fetch bomtique's documentation from the network. Learn the
   tool by running `bomtique --help` and `bomtique <subcommand>
   --help` for every command you use. The CLI is the source of
   truth; flag names, semantics, and exit codes you "remember" from
   training data may be stale.
2. Do NOT make assumptions about this repository. Survey it first,
   then ask the user for clarifications on anything ambiguous before
   authoring a single manifest.
3. Do NOT try to compile the project. Bomtique scans hand-authored
   manifests; the build system is irrelevant to the SBOM, except as
   a source of dependency information you read passively.
4. Do NOT touch `obsolete/`, `legacy/`, `archive/`, or similarly-
   named directories without explicit user confirmation.
5. Bomtique itself MAY make outbound network calls when you use the
   `--ref` / `--upstream-ref` / `--refresh` flags (those hit GitHub,
   GitLab, npm, PyPI, or crates.io to fetch metadata). That's fine.
   The agent's own learning of bomtique's CLI is what stays local.
6. Co-locate manifests with the source they describe. Do NOT create
   a separate `manifests/`, `sbom-inputs/`, `bomtique/`, or similar
   centralised directory to hold all the manifest files. Specifically:
   - Each `.primary.json` lives in the directory of the artifact it
     describes — the repo root for a single-artifact project, or
     the artifact's own source directory for a multi-artifact
     project.
   - Each `.components.json` lives where its components live.
     Vendored libraries sitting under `lib/foo/` get a
     `lib/foo/.components.json`; an internal submodule ships its
     own `.components.json` at the submodule root; project-wide
     dependencies live in a root `.components.json`.
   - Bomtique's discovery walks the tree and merges every
     `.components.json` into one shared pool, so co-location costs
     nothing at scan time and keeps SBOM ownership close to code
     ownership.

Work in five phases, in order. Pause at the end of each phase for
user confirmation before moving to the next.

==============================================================
PHASE 1 — Learn bomtique
==============================================================

Run these and read the output carefully:

  bomtique --version
  bomtique --help
  bomtique scan --help
  bomtique validate --help
  bomtique manifest --help
  bomtique manifest init --help
  bomtique manifest add --help
  bomtique manifest remove --help
  bomtique manifest update --help
  bomtique manifest patch --help
  bomtique manifest schema --help

Take note of any flags or sub-options that surprise you relative to
your prior knowledge. Bomtique's --ref / --upstream-ref / --refresh
/ --primary / BOMTIQUE_OFFLINE surface evolves; trust the help text
over your assumptions.

Concept primer (cross-check against `bomtique manifest schema` and
the help texts above):

- A **primary manifest** (`.primary.json`) describes one buildable
  artifact (a binary, firmware image, library release, container
  image, etc.). One primary per artifact. A repo with multiple
  shippable artifacts has multiple `.primary.json` files, typically
  one in each artifact's source directory.
- A **components manifest** (`.components.json`) describes a pool
  of dependencies. Multiple `.components.json` files anywhere in
  the tree are merged into one shared pool at scan time. Each
  primary's `depends-on` list resolves against this shared pool.
- `bomtique scan` walks the directory tree, discovers every
  `.primary.json` and `.components.json`, builds the pool, resolves
  reachability per primary, and emits one SBOM per primary.
- Vendored libraries (third-party source copied into the repo) are
  added with `--vendored-at <dir>`, which records a directory hash
  recomputed at scan time and a `pedigree.ancestors[0]` block for
  the upstream. `--upstream-ref <purl-or-url>` fetches the
  upstream's metadata via bomtique's importer (GitHub, GitLab, npm,
  PyPI, crates.io); `--upstream-name`/`--upstream-version`/etc.
  override fetched fields or substitute when no clean upstream tag
  exists.
- `--tag <name>` on pool components groups them; `bomtique scan
  --tag <name>` filters to just the components carrying that tag.
  This is how you produce per-variant SBOMs from the same source
  tree (per-board, per-feature-set, debug vs release).
- `SOURCE_DATE_EPOCH=<seconds>` (env var or `--source-date-epoch
  <n>` flag on `scan`) makes the emitted SBOM byte-identical across
  runs. Use it whenever you commit a baseline SBOM or want CI drift
  detection.

==============================================================
PHASE 2 — Survey the repository
==============================================================

Run a passive read-only inventory. Do not write anything yet.

Capture, in your own scratch notes:

a. Top-level layout: `ls -la`, `git log -1 --oneline`,
   `git submodule status`, `cat README.md` (or first ~80 lines).
b. License: `ls LICENSE*`, `head LICENSE`. Note SPDX identifier if
   visible (Apache-2.0, MIT, GPL-2.0-only, GPL-3.0-or-later, etc.).
c. Build system: presence of Makefile, CMakeLists.txt,
   build.gradle, Cargo.toml, package.json, pyproject.toml,
   platformio.ini, west.yml, etc. Note the language(s) and
   toolchain(s).
d. Versioning: hunt for the project's current version in obvious
   places (`package.json`, `Cargo.toml`, `pyproject.toml`,
   `version.h`, `VERSION`, `CMakeLists.txt`, `git describe --tags`,
   the changelog, the README).
e. Vendored or third-party content: directories that look like they
   contain copied-in OSS source. Common patterns: `vendor/`, `lib/`,
   `third_party/`, `external/`, `deps/`, named after libraries
   directly. For each, note: language, apparent license (look for
   LICENSE / COPYING / SPDX header), version markers, whether it's
   a git submodule (in `.gitmodules`) or directly committed.
f. Configuration variants: per-board configs, feature flags,
   target matrices. Look in `configs/`, `targets/`, `variants/`,
   `boards/`, or wherever the build system selects between them.
g. CI: `.github/workflows/`, `.gitlab-ci.yml`, `circleci/`,
   `azure-pipelines.yml`. Note whether SBOM generation is already
   wired up.
h. Existing SBOMs: any `*.cdx.json`, `*.spdx.json`, `sbom/`,
   `bom.xml` files in the tree.

==============================================================
PHASE 3 — Ask clarifying questions
==============================================================

Stop. Compile a list of clarifications based on Phase 2's findings,
present them to the user as a numbered list, and wait for answers
before authoring anything. The list should cover (when relevant):

1. **Primary artifact(s).** "I see X, Y, and Z that look buildable.
   Should each be its own primary, or is one of them THE shippable
   artifact and the others build helpers? Which directories should
   I scaffold a `.primary.json` in?"
2. **Project version.** "The README says 2.1.0; CMakeLists.txt
   says 2.0.5; git describe says v2.1.0-rc3. Which value do you
   want on the primary's `version` field?"
3. **Top-level license.** "I see GPL-3 in LICENSE but several
   vendored directories carry MIT / Apache-2.0 / BSD-3 headers.
   Should the primary record `GPL-3.0-or-later` and let each pool
   component carry its own license?"
4. **Vendored content classification.** Confirm each candidate
   third-party directory: "Is `<path>` a vendored copy I should
   model with `--vendored-at`, a build-time tool I should mark
   `--scope excluded`, or out of scope entirely?" Specifically ask
   about `tools/`, `scripts/`, `docs/`, generated code, and
   anything ambiguous.
5. **Variants.** "I see N target boards / build configurations.
   Should I produce one SBOM per variant (`--tag <variant>`), one
   SBOM covering everything, or both?"
6. **Patched vendoring.** "Is anything under `vendor/` or `lib/`
   carrying local patches relative to upstream? If yes, point me at
   the patch files (or a diff) so I can register them via
   `bomtique manifest patch`."
7. **Submodule init policy.** "Submodules X, Y, Z are large
   (multi-GB SDKs). For the SBOM I only need the manifests +
   vendored library trees, not the SDK source. Should I `git
   submodule update --init` only the ones whose contents flow into
   the build I'm modelling?"
8. **CI integration.** "Once the manifests are committed, do you
   want me to draft a CI step that regenerates the SBOM on every PR
   and attaches it to releases?"

Ask only the questions Phase 2 actually surfaced. Don't ask
hypothetical ones.

==============================================================
PHASE 4 — Author the manifests
==============================================================

Once the user has answered Phase 3:

a. `bomtique manifest init` for each primary the user confirmed,
   passing `--name`, `--version`, `--type`, `--license`, supplier,
   external references. Use `-C <artifact-source-dir>` so each
   `.primary.json` lands in the directory of the artifact it
   describes, never in a centralised manifests/ folder.

b. For each pool component, decide which add pattern fits and
   explain the choice in your scratch notes:

   - **Registry-fetchable upstream**: pool component points at a
     GitHub repo, npm package, PyPI distribution, etc., with a
     clean release tag. Use `--ref <purl-or-url>` (or
     `--upstream-ref` for vendored copies). Bomtique fetches name,
     license, description, supplier, external refs from the
     registry. Flag values override.
   - **Vendored copy with clean upstream**: directory inside the
     repo carrying third-party source. `--vendored-at <dir>` plus
     `--upstream-ref` if the upstream has a matching tag.
   - **Vendored copy without a clean upstream tag**: same
     `--vendored-at`, but use manual `--upstream-name` /
     `--upstream-version` / `--upstream-purl` /
     `--upstream-supplier` / `--upstream-website` / `--upstream-vcs`
     instead of `--upstream-ref`.
   - **Internal library / no upstream**: `--vendored-at` if
     in-tree, no `--upstream-*` block at all.
   - **Build-time tool (compiler, code generator, linter)**: add
     with `--scope excluded` so it's recorded but dropped from the
     emitted SBOM.

c. Pin specific versions wherever possible. If `bomtique manifest
   add` warns "no ref supplied for X; using default branch", do one
   of: pin the upstream-ref to a tag, drop the `--upstream-ref`, or
   document why the default-branch reference is acceptable.

d. Handle `bomtique` errors as they arise:
   - `--upstream-ref ... GitHub tag '...' not found`: the
     upstream repo doesn't carry that tag. Fall back to default-
     branch ref or to manual `--upstream-*`.
   - "Unable to find current revision in submodule path": the
     pinned commit was force-pushed past upstream. Fall back to
     `git clone --depth 1 <url> <path>` from the URL in
     `.gitmodules`.
   - "directory produced no eligible files": the `--vendored-at`
     path is empty (uninitialised submodule, or wrong `--ext`
     filter). Init the submodule, or expand the extension list.

e. Pass `-C <dir>` or `--into <path>` on each `manifest add` so the
   `.components.json` lands alongside the source it describes:
   - Vendored library at `lib/foo/`: target
     `lib/foo/.components.json`.
   - Internal submodule with its own subtree: the submodule's own
     root `.components.json`.
   - Project-wide dependencies that don't fit in any subtree:
     repo-root `.components.json`.
   - NEVER write to `manifests/`, `sbom-inputs/`, or any other
     centralised holding directory.

f. Wire each pool component into the appropriate primary's
   `depends-on` with `bomtique manifest add --primary --purl
   <component-purl> ...`. Use `-C <primary-dir>` if the primary
   isn't at the repo root.

g. If the user wants per-variant SBOMs, attach `--tag <variant>` to
   the relevant pool components.

==============================================================
PHASE 5 — Validate, scan, verify
==============================================================

a. `bomtique validate --warnings-as-errors`. Fix anything it flags
   before emitting an SBOM.

b. `SOURCE_DATE_EPOCH=$(git log -1 --format=%ct) bomtique scan
   --output-validate --out ./sbom`. The `--output-validate` flag
   verifies the emitted document against the bundled CycloneDX 1.7
   schema. The `SOURCE_DATE_EPOCH` makes the output deterministic.

c. If the user asked for variants, run `bomtique scan --tag
   <variant> --out ./sbom-<variant>` for each.

d. Inspect the emitted SBOM(s):
   - One file per primary, named `<name>-<version>.cdx.json`.
   - Confirm `metadata.component` matches the primary.
   - Confirm `components[]` carries the expected pool entries with
     correct purls, hashes (directory-form recomputed at scan
     time), and licenses.
   - Confirm `dependencies[]` reflects each primary's depends-on
     edges.

e. If the user wants CI integration, draft a workflow file (e.g.,
   `.github/workflows/sbom.yml`) that:
   - Runs on PRs and tag pushes.
   - Initialises only the submodules the SBOM needs.
   - Runs `bomtique validate --warnings-as-errors`.
   - Runs `bomtique scan --output-validate
     --source-date-epoch $(git log -1 --format=%ct) --out ./sbom`.
   - Uploads the SBOM as a workflow artifact on PRs.
   - Attaches the SBOM to the GitHub release on tag pushes.

==============================================================
Output format
==============================================================

When you're done, summarise back to the user:

- Number of primaries scaffolded and their names.
- Number of pool components added, broken down by tag.
- Where each `.primary.json` and `.components.json` lives.
- Which SBOM files were emitted, their sizes, and licenses
  represented.
- Any pool components whose upstream version you couldn't pin
  cleanly (these are follow-ups for the user to triage).
- The CI workflow path, if drafted.

Don't commit anything to git. Leave the changes staged or unstaged
so the user reviews and commits themselves.
