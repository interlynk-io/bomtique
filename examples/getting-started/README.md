# Getting Started — section snapshots

Each `sectionN/` directory is a snapshot of the repo state a reader
would have after working through the matching section of
[`docs/getting-started.md`](../../docs/getting-started.md). Use them
as reference while reading: the manifests, vendored sources, patches,
and emitted SBOMs are exactly what the doc's commands produce.

| Snapshot | What's in it |
|----------|--------------|
| [`section1/`](./section1/) | scaffolded primary, libtls fetched + refined, primary `depends-on` wired, `sbom/device-firmware-0.1.0.cdx.json` |
| [`section3/`](./section3/) | + lvgl added, `core` and `display` tags applied, primary depends-on extended |
| [`section5/`](./section5/) | + `libs/libmqtt/.components.json` (the internal submodule's manifest) and primary wire |
| [`section6/`](./section6/) | + vendored cjson and miniz under `src/`, cjson patch registered, primary wires |
| [`section7/`](./section7/) | same source tree as section 6, `sbom-base/` and `sbom-display/` showing the variant filter at work |
| [`section8/`](./section8/) | primary version bumped to 1.0.0 by hand-edit; `sbom/device-firmware-1.0.0.cdx.json` |
| [`section9/`](./section9/) | libmqtt bumped 4.3.0 → 4.4.0 (purl in lockstep), primary `depends-on` re-wired |

Sections 2 and 4 don't have their own directory because they
introduce no on-disk state changes (section 2 inspects what section
1 produced; section 4 introduces `bomtique validate` and
`--output-validate`, both read-only).

## Regenerating

[`generate.sh`](./generate.sh) builds the `bomtique` binary, walks
the doc's narrative end-to-end in a temporary directory, and
overwrites these snapshot directories with the resulting state.
SBOMs are stamped with `SOURCE_DATE_EPOCH=1700000000` for byte-stable
output, matching the dogfood test convention.

```
./examples/getting-started/generate.sh
```

Network is required: section 1 fetches libressl/portable metadata
from GitHub, and section 3 fetches lvgl. If GitHub returns different
metadata over time (description tweak, license-detection change),
the snapshots regenerate to match. Commit the regen.
