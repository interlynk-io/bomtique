# bomtique

Hand-authored SBOM toolkit — a manifest specification and reference consumer
for producing CycloneDX (and SPDX) SBOMs from deliberately-curated component
metadata.

## Status

Draft. The first published artifact is the manifest specification; the
reference consumer tool is planned.

## Contents

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
