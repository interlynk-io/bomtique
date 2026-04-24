# Contributing to bomtique

## Development setup

Requirements: Go 1.26+. The `go` directive in `go.mod` will auto-fetch the
toolchain on first build.

```
git clone https://github.com/interlynk-io/bomtique
cd bomtique
make build           # ./bin/bomtique
make test            # full suite
make ci              # build, vet, fmt-check, test, test-race
```

Some tests in `internal/purl` exercise the upstream
[package-url/purl-spec](https://github.com/package-url/purl-spec) suite.
They are skipped when the suite is absent. To run them:

```
git clone --depth=1 https://github.com/package-url/purl-spec testdata/purl-spec
go test ./internal/purl/
```

`testdata/purl-spec/` is gitignored.

## Branching and PRs

`main` is protected; all changes land via pull request.

- Keep PRs scoped to one logical change. When work builds on another
  open PR, stack the second branch on the first and set its base to the
  predecessor branch (GitHub retargets to `main` after the predecessor
  merges).
- PR title and description should explain the spec section being
  implemented (for consumer behaviour) or the reason for the spec change
  (for edits to `spec/`).
- CI must be green before merge.

## Commit messages

Short imperative subject (≤ 72 chars), optional body explaining the
motivation. Prefer spec section references in the body (e.g.
"Component Manifest v1 §9.3") over inline code references that rot.

## Coding conventions

- `gofmt -s`; `go vet`; `staticcheck`; `golangci-lint run`. `make ci`
  runs all of these.
- Every `.go` file starts with the two-line SPDX header shown in any
  existing file.
- Add tests alongside new behaviour. Conformance-level work in
  `internal/*` must include the corresponding fixture in
  `testdata/conformance/` when M12 lands.
- Follow `TASKS.md`. Each task is a verifiable state; close it by
  ticking the box in the same PR that lands the work.

## Security

Never commit credentials or test fixtures with real PII. Report
suspected vulnerabilities per `SECURITY.md`.
