# bomtique

Hand-authored SBOM toolkit — a manifest specification and reference consumer
for producing CycloneDX (and SPDX) SBOMs from deliberately-curated component
metadata.

## Status

Draft. The Component Manifest v1 specification and the reference
consumer (`bomtique`) both ship today; a v0.2 release is in preparation.

## Getting started

New here? Start with [**`docs/getting-started.md`**](./docs/getting-started.md):
a progressive walkthrough that grows a small C/embedded firmware project
from one library to a CI-validated SBOM with vendored + patched
components, per-build variants, deterministic builds, and drift
detection over time. Each section adds one feature on top of the
previous one, so you can stop reading wherever your project's needs
end.

## Contents

- [`docs/getting-started.md`](./docs/getting-started.md) — the user-facing
  walkthrough: scaffold a primary, add dependencies, emit an SBOM, and
  layer features in as the project grows.
- [`docs/usage.md`](./docs/usage.md) — full CLI reference for every
  subcommand and flag, registry importers, and exit codes.
- [`spec/component-manifest-v1.md`](./spec/component-manifest-v1.md) — the
  Component Manifest specification, version 1. A normative RFC-style
  specification of two hand-authored file formats (primary manifests and
  components manifests) and the rules a conforming consumer applies to emit
  one SBOM per primary.

## Why

Automated Software Composition Analysis (SCA) tools are unreliable in C/C++,
embedded, legacy, and hybrid codebases. They produce false positives, miss
vendored and patched components, and have no story for per-build-variant
SBOMs. For these projects, the practical workflow is for developers to
curate component metadata by hand and generate a compliant SBOM from it —
deterministically, reproducibly, and at per-artifact granularity.

bomtique aims to be the specification and tooling that makes that workflow
first-class.

## License

Apache-2.0 — see [LICENSE](./LICENSE).
