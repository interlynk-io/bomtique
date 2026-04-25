# bomtique

Hand-authored SBOM toolkit — a manifest specification and reference consumer
for producing CycloneDX (and SPDX) SBOMs from deliberately-curated component
metadata.

## Status

Draft. The Component Manifest v1 specification and the reference
consumer (`bomtique`) both ship today; a v0.2 release is in preparation.

## Install

```bash
brew tap interlynk-io/tap
brew install bomtique
```

For Docker, pre-built binaries, or `go install`, see
[Other install methods](#other-install-methods) at the bottom.

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

## Other install methods

Homebrew (above) is the recommended path. The release pipeline also
publishes the channels below for cases where Homebrew isn't an option.

### Docker

```bash
docker pull ghcr.io/interlynk-io/bomtique:latest
docker run --rm -v "$PWD:/work" ghcr.io/interlynk-io/bomtique:latest scan
```

The image is multi-arch (linux/amd64 + linux/arm64) and runs as a
non-root user out of a distroless base.

### Pre-built binary

Download the archive for your platform from the
[releases page](https://github.com/interlynk-io/bomtique/releases),
verify against `checksums.txt`, and drop the `bomtique` binary on your
`PATH`:

```bash
# macOS Apple Silicon (substitute the version + arch you need)
curl -LO https://github.com/interlynk-io/bomtique/releases/download/v0.1.0/bomtique_0.1.0_Darwin_arm64.tar.gz
curl -LO https://github.com/interlynk-io/bomtique/releases/download/v0.1.0/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar xzf bomtique_0.1.0_Darwin_arm64.tar.gz
sudo mv bomtique /usr/local/bin/
```

The release also ships a cosign signature of `checksums.txt`
(`checksums.txt.sig` + `checksums.txt.pem`) for keyless signature
verification.

### From source (Go 1.26+)

```bash
go install github.com/interlynk-io/bomtique/cmd/bomtique@latest
```

Verify with `bomtique --version`.

## License

Apache-2.0 — see [LICENSE](./LICENSE).
