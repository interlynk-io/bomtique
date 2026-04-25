#!/usr/bin/env bash
#
# Regenerate the section-by-section snapshots that accompany
# docs/getting-started.md. Each section directory captures the repo
# state a reader would have after following that section's commands,
# plus the SBOM scan emits at that point. Snapshots are byte-stable
# under SOURCE_DATE_EPOCH=1700000000 (matching the dogfood test).
#
# Network is required: section 1 fetches libtls metadata from GitHub
# (libressl/portable), and section 3 fetches lvgl. Without network or
# under rate limiting the script fails. Re-run after the issue clears.
#
# Usage: ./examples/getting-started/generate.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
BOMTIQUE="$REPO/bin/bomtique"
OUT="$SCRIPT_DIR"
EPOCH=1700000000

if [[ ! -x "$BOMTIQUE" ]]; then
    echo "building bomtique..." >&2
    (cd "$REPO" && go build -o bin/bomtique ./cmd/bomtique)
fi

WORK=$(mktemp -d)
trap "rm -rf '$WORK'" EXIT
cd "$WORK"

snapshot() {
    local name=$1
    local target="$OUT/$name"
    rm -rf "$target"
    mkdir -p "$target"
    # Copy everything in the working tree. There's no .git or build
    # output to filter; the working dir only has manifests, sources,
    # patches, and emitted SBOMs.
    cp -R . "$target/"
    echo "snapshot: $name" >&2
}

# Clear all sbom* output dirs so the next section's scan(s) produce
# only the artifacts that section is meant to emit; otherwise stale
# outputs from earlier sections leak into later snapshots.
clear_sboms() {
    rm -rf ./sbom ./sbom-base ./sbom-display
}

# ─── Section 1 ─────────────────────────────────────────────────────────
# scaffold + fetch libtls + refine + wire to primary + scan
$BOMTIQUE manifest init \
    --name device-firmware --version 0.1.0 \
    --type firmware --license MIT
$BOMTIQUE manifest add \
    --ref https://github.com/libressl/portable/releases/tag/v3.9.0
$BOMTIQUE manifest update pkg:github/libressl/portable@v3.9.0 \
    --name libtls --to 3.9.0 --license ISC
$BOMTIQUE manifest add --primary \
    --name libtls --version 3.9.0 \
    --purl pkg:github/libressl/portable@3.9.0
clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --out ./sbom

snapshot section1

# ─── Section 3 ─────────────────────────────────────────────────────────
# add lvgl, tag for variants, wire to primary
$BOMTIQUE manifest add --ref pkg:github/lvgl/lvgl@v9.2.2
$BOMTIQUE manifest update pkg:github/lvgl/lvgl@v9.2.2 --tag display
$BOMTIQUE manifest update pkg:github/libressl/portable@3.9.0 --tag core
$BOMTIQUE manifest add --primary \
    --name lvgl --version 9.2.2 \
    --purl pkg:github/lvgl/lvgl@v9.2.2
clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --out ./sbom

snapshot section3

# ─── Section 5 ─────────────────────────────────────────────────────────
# libmqtt internal submodule (ships its own manifest); wire to primary
mkdir -p libs/libmqtt
cat > libs/libmqtt/.components.json <<'EOF'
{
  "schema": "component-manifest/v1",
  "components": [
    {
      "name": "libmqtt",
      "version": "4.3.0",
      "description": "Acme MQTT client library",
      "supplier": { "name": "Acme Corp" },
      "license": { "expression": "EPL-2.0" },
      "purl": "pkg:generic/acme/libmqtt@4.3.0",
      "depends-on": ["pkg:github/libressl/portable@3.9.0"],
      "tags": ["core", "networking"]
    }
  ]
}
EOF
$BOMTIQUE manifest add --primary \
    --name libmqtt --version 4.3.0 \
    --purl pkg:generic/acme/libmqtt@4.3.0
clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --out ./sbom

snapshot section5

# ─── Section 6 ─────────────────────────────────────────────────────────
# vendor cjson + miniz, register the cjson patch, wire to primary
mkdir -p src/cjson/patches src/miniz
cat > src/cjson/cJSON.c <<'EOF'
/* Vendored fork of DaveGamble/cJSON 1.7.17 with local CVE-2024-XXXXX
 * fix applied via patches/cjson-fix-int-overflow.patch. */
int cjson_parse_int(const char *s) { return 0; }
EOF
cat > src/cjson/cJSON.h <<'EOF'
#ifndef CJSON_H
#define CJSON_H
int cjson_parse_int(const char *s);
#endif
EOF
cat > src/cjson/patches/cjson-fix-int-overflow.patch <<'EOF'
--- a/cJSON.c
+++ b/cJSON.c
@@ -1,3 +1,4 @@
-int cjson_parse_int(const char *s) { return 0; }
+int cjson_parse_int(const char *s) {
+    /* CVE-2024-XXXXX: bounds-check before strtol */
+    return 0;
+}
EOF
cat > src/miniz/miniz.c <<'EOF'
/* Vendored copy of richgel999/miniz 3.0.2. Unmodified upstream source. */
int miniz_compress(const char *in, char *out) { return 0; }
EOF
cat > src/miniz/miniz.h <<'EOF'
#ifndef MINIZ_H
#define MINIZ_H
int miniz_compress(const char *in, char *out);
#endif
EOF

$BOMTIQUE manifest add \
    --name cjson --version 1.7.17 \
    --license MIT \
    --description "Ultralightweight JSON parser (vendored fork)" \
    --supplier "Dave Gamble" \
    --purl pkg:github/acme/device-firmware/src/cjson@1.7.17 \
    --vendored-at src/cjson --ext c,h \
    --upstream-ref https://github.com/DaveGamble/cJSON/releases/tag/v1.7.17 \
    --tag core

$BOMTIQUE manifest add \
    --name miniz --version 3.0.2 \
    --license MIT \
    --supplier "Rich Geldreich" \
    --purl pkg:github/acme/device-firmware/src/miniz@3.0.2 \
    --vendored-at src/miniz --ext c,h \
    --upstream-ref https://github.com/richgel999/miniz/releases/tag/3.0.2 \
    --tag core

$BOMTIQUE manifest patch \
    pkg:github/acme/device-firmware/src/cjson@1.7.17 \
    ./src/cjson/patches/cjson-fix-int-overflow.patch \
    --type backport \
    --resolves "type=security,name=CVE-2024-XXXXX"

$BOMTIQUE manifest add --primary \
    --name cjson --version 1.7.17 \
    --purl pkg:github/acme/device-firmware/src/cjson@1.7.17
$BOMTIQUE manifest add --primary \
    --name miniz --version 3.0.2 \
    --purl pkg:github/acme/device-firmware/src/miniz@3.0.2

clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --out ./sbom

snapshot section6

# ─── Section 7 ─────────────────────────────────────────────────────────
# variants: same source tree, two SBOMs from --tag filtering
clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --tag core              --out ./sbom-base 2>/dev/null
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --tag core --tag display --out ./sbom-display

snapshot section7

# ─── Section 8 ─────────────────────────────────────────────────────────
# 1.0.0 release: bump the primary's version via manifest update --primary.
$BOMTIQUE manifest update --primary --to 1.0.0
clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --out ./sbom

snapshot section8

# ─── Section 9 ─────────────────────────────────────────────────────────
# Maintenance: bump libmqtt 4.3.0 -> 4.4.0 (purl in lockstep).
$BOMTIQUE manifest update pkg:generic/acme/libmqtt@4.3.0 --to 4.4.0

# Re-wire the primary's depends-on to point at the new libmqtt purl
# (`manifest update --to` doesn't propagate to consumers' depends-on
# lists; that's a separate edit).
$BOMTIQUE manifest remove pkg:generic/acme/libmqtt@4.3.0 --primary 2>/dev/null || true
$BOMTIQUE manifest add --primary \
    --name libmqtt --version 4.4.0 \
    --purl pkg:generic/acme/libmqtt@4.4.0

clear_sboms
SOURCE_DATE_EPOCH=$EPOCH $BOMTIQUE scan --out ./sbom

snapshot section9

echo "done; snapshots written to $OUT/section{1,3,5,6,7,8,9}" >&2
