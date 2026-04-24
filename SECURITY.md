# Security Policy

## Supported Versions

bomtique is pre-1.0. Only `main` receives security fixes until the first
tagged release.

## Reporting a Vulnerability

Email `security@interlynk.io` with a description of the issue, steps to
reproduce, and the affected commit or release. Please do not open public
GitHub issues for security reports.

Expect an acknowledgement within three working days. We will coordinate
a fix, a disclosure timeline, and, where appropriate, a CVE request.

## Threat Model

bomtique consumes Component Manifest files as **untrusted input** per
the threat model in `spec/component-manifest-v1.md` §18. The reference
consumer upholds these invariants by construction:

- **Bounded I/O.** Relative paths are resolved against the manifest's
  directory; absolute paths, path traversal (`..`), and symbolic links
  are rejected (§4.3, §18.1, §18.2).
- **No network.** The consumer never fetches URLs at runtime (§18.3).
  External-reference URLs are recorded in the emitted SBOM verbatim.
- **File-size cap.** Every file read enforces a configurable maximum
  (default 10 MiB, §8).
- **Algorithm allowlist.** Only SHA-256, SHA-384, SHA-512, SHA-3-256,
  and SHA-3-512 are permitted for hashes (§8.1); MD5 and SHA-1 are
  refused.

If you discover behaviour that violates any of these invariants,
that is a security-relevant bug and should be reported via the process
above.
