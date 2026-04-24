# Changelog

All notable changes to this project are documented here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added
- Component Manifest v1 specification under `spec/`.
- Go module `github.com/interlynk-io/bomtique` targeting Go 1.26.
- `internal/purl` — Package-URL parser forked from
  `interlynk-io/lynkctl/pkg/purl`, modularized across
  purl/encoding/chars/qualifiers/subpath/types/normalize/validate,
  extended with `Equal` / `CanonEqual`. Passes the full
  package-url/purl-spec conformance suite.
- Package skeletons for every M1–M10 target: `internal/manifest`,
  `internal/hash`, `internal/pool`, `internal/graph`, `internal/jcs`,
  `internal/safefs`, `internal/diag`, `internal/emit/cyclonedx`,
  `internal/emit/spdx`.
- `internal/diag` warning channel — single emitter for the
  `warning:`-prefixed stderr contract in spec §13.3.
- `internal/safefs.ToNFC` — Unicode NFC normalization helper required by
  spec §4.6.
- `internal/emit/cyclonedx.deterministicSerial` — UUIDv5 derivation
  stub that anchors the serial-number work for spec §15.3.
- `cmd/bomtique` — Cobra root command with `generate` and `validate`
  sub-commands, both currently stubbed.
- Makefile with `build`, `test`, `test-race`, `vet`, `fmt`, `fmt-check`,
  `lint`, `cover`, `fuzz`, `tidy`, `tools`, `ci`, `clean` targets.
- GitHub Actions CI covering Linux / macOS / Windows on Go 1.26, with
  race detector on non-Windows, coverage artifact, staticcheck, and
  golangci-lint.
- `CONTRIBUTING.md`, `SECURITY.md`, this `CHANGELOG.md`.
- SPDX short-form license headers on all Go sources.

### Changed
- Aligned spec §13.2 with §8.1 so the permitted hash-algorithm set is
  stated in one normative place.
