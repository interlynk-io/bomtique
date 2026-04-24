# Determinism

Spec §15 requires byte-identical SBOM output when invoked twice on the
same inputs. bomtique delivers that when `SOURCE_DATE_EPOCH` is set —
either through the `--source-date-epoch` flag or the standard env var.
Without it, timestamps and serial numbers are non-deterministic (the
spec allows either behaviour in that case).

## The determinism contract

Given the same:

- manifest files (byte-for-byte),
- referenced on-disk files (license texts, hashed files, patch diffs),
- `SOURCE_DATE_EPOCH`,

...two invocations of `bomtique generate` produce identical output
bytes. Regression-tested by
[`cmd/bomtique/conformance_test.go`](../cmd/bomtique/conformance_test.go)'s
`TestConformance_Determinism` sweep.

## Mechanisms

### Stable field ordering

The CycloneDX 1.7 emitter hand-rolls struct-per-field types so
`encoding/json` walks fields in declaration order. No map-based
serialisation anywhere in the output path.

### §15.2 array sorting

Applied after assembly, before `json.Marshal`:

- `components[]` by `bom-ref` ascending.
- `dependencies[]` + every `dependsOn` array by `ref` ascending.
- Per-component `hashes` by `(algorithm, content)`.
- Per-component `externalReferences` by `(type, url)`.
- Pedigree sub-components recursively.

### §15.3 timestamp

When `SOURCE_DATE_EPOCH` is set, `metadata.timestamp` (CycloneDX) and
`creationInfo.created` (SPDX) are formatted as ISO 8601 UTC with
second precision via `time.Format("2006-01-02T15:04:05Z")`.

### §15.3 serial number / documentNamespace

CycloneDX `serialNumber` is `urn:uuid:<UUIDv5>` derived from:

1. The `components[]` array post-sort.
2. `json.Marshal` to bytes.
3. `jcs.Canonicalize` (RFC 8785) to produce a byte-stable canonical form.
4. SHA-256 digest, hex-encoded lowercase.
5. UUIDv5 in the RFC 4122 DNS namespace, over the name string
   `"component-manifest/v1/serial/" + <hex>`.

SPDX `documentNamespace` uses the same canonical-seed → SHA-256 →
UUIDv5 path, scoped to `https://interlynk.io/bomtique/spdx/`.

### RFC 8785 JSON canonicalisation

[`internal/jcs`](../internal/jcs/) provides RFC 8785 JCS:

- Object keys sorted by UTF-16 code-unit order.
- No whitespace.
- Minimal string escapes (`\"`, `\\`, short-form controls, `\u00NN`
  for other C0 characters; UTF-8 otherwise).
- ECMA-262 `Number.prototype.toString` numbers (integer / decimal /
  leading-zero decimal / scientific per `(s, k, n)` classification;
  mandatory exponent sign; no leading-zero exponent; `-0` collapses).

## What determinism does NOT guarantee

- File-system contents changing between runs breaks determinism by
  design — a newly computed `hash.file` digest differs. Spec §15.4
  places that responsibility on the producer.
- Different `SOURCE_DATE_EPOCH` values produce different serial
  numbers and timestamps, as expected.
- Different bomtique versions may change output as the emitter
  evolves; CHANGELOG.md notes any on-the-wire change.
