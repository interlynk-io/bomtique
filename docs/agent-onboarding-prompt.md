# Onboarding prompt for AI coding agents

bomtique ships a drop-in prompt you can paste into any AI coding
agent (Claude Code, Cursor, Aider, ...) when you want it to set up
SBOM generation on **your** repo.

**The prompt itself lives at
[`prompts/agent-onboarding.md`](../prompts/agent-onboarding.md).**
That file is intentionally just the prompt — copy its full contents
into your agent's prompt window or system message, no surrounding
prose to strip.

## What the prompt does

The agent walks through five phases on your repository, pausing at
the end of each phase for your confirmation:

1. **Learn bomtique** — the agent runs `bomtique --help` plus each
   subcommand's `--help` and uses that as the source of truth, so
   the prompt stays accurate as bomtique's CLI evolves.
2. **Survey the repository** — read-only inventory: layout,
   license, build system, versioning, vendored content, variants,
   CI, existing SBOMs.
3. **Ask clarifying questions** — the agent presents a numbered
   list of clarifications based on what it found in Phase 2 and
   waits for your answers before authoring anything. No guessing
   on primary identification, license rollup, vendored
   classification, or variant strategy.
4. **Author the manifests** — `bomtique manifest init` for each
   primary, then per-component `manifest add` decisions across five
   patterns (registry-fetchable, vendored-with-upstream,
   vendored-without-upstream, internal/no-upstream, build-time tool
   as `--scope excluded`).
5. **Validate, scan, verify** — `bomtique validate
   --warnings-as-errors`, then a deterministic
   `bomtique scan --output-validate` under `SOURCE_DATE_EPOCH`,
   then optional CI workflow.

## Constraints baked into the prompt

- **bomtique must already be installed locally** and on the
  agent's `$PATH`. See [Install in the README](../README.md#install).
- **No network for bomtique docs.** The agent learns the CLI
  exclusively from local `--help` output. Bomtique itself still
  hits the network when the agent uses `--ref`/`--upstream-ref`/
  `--refresh` — those are the registry importers.
- **No build attempts.** Bomtique scans manifests; the build system
  is a passive read source.
- **No git commits.** The agent stages and stops; you review and
  commit.
- **Hands off `obsolete/`/`legacy/`/`archive/`** without explicit
  confirmation.
- **Manifests co-locate with source.** Each `.primary.json` lands
  in the directory of the artifact it describes, and each
  `.components.json` lives next to the components it covers
  (vendored library at `lib/foo/` → `lib/foo/.components.json`,
  internal submodule → its own root, repo-wide deps → repo root).
  Bomtique's discovery walks the tree and merges every
  `.components.json` into one shared pool at scan time, so this
  costs nothing at scan time and keeps SBOM ownership close to code
  ownership for PR review. The agent will not create a centralised
  `manifests/` holding directory.

## Adapting the prompt

The prompt works as-is. Two optional refinements depending on your
agent and your repo:

- **Long projects** (hundreds of subdirectories, huge dep trees):
  append "Work on one primary/dependency at a time and pause for
  confirmation between each, rather than batching." after the
  operating constraints.
- **Strict review**: append "Show me each command before running
  it. I'll approve each one individually." Same place.

If your agent supports project-level rules
(`CLAUDE.md`, `.cursorrules`, etc.), consider copying the operating
constraints into that file too so they persist across sessions.
