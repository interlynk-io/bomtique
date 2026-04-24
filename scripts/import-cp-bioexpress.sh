#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Interlynk.io
# SPDX-License-Identifier: Apache-2.0
#
# End-to-end driver that turns every version.txt in CP.BioExpress
# into a bomtique component manifest entry, wires up per-primary
# depends-on, and emits CycloneDX + SPDX SBOMs under
# deterministic SOURCE_DATE_EPOCH.
#
# Usage:
#   scripts/import-cp-bioexpress.sh [--repo <path>] [--dry-run]
#                                   [--step pool|primaries|scan|all]
#                                   [--offline] [--force]
#
# See plan file for full description.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/import-cp-bioexpress-lib.sh
source "$SCRIPT_DIR/import-cp-bioexpress-lib.sh"

repo="/home/riteshnoronha/Work/fun/customer-embedded/CP.BioExpress"
dry_run=0
step="all"
offline=0
force=0

while [ "$#" -gt 0 ]; do
    case "$1" in
        --repo)      repo="$2"; shift 2 ;;
        --dry-run)   dry_run=1; shift ;;
        --step)      step="$2"; shift 2 ;;
        --offline)   offline=1; shift ;;
        --force)     force=1; shift ;;
        -h|--help)
            sed -n '2,14p' "${BASH_SOURCE[0]}" | sed 's/^# //; s/^#//'
            exit 0
            ;;
        *) echo "unknown flag: $1" >&2; exit 2 ;;
    esac
done

if [ ! -d "$repo" ]; then
    echo "repo not found: $repo" >&2
    exit 1
fi

# Locate bomtique. Prefer a fresh build of the repo we live in.
BOMTIQUE_REPO="$(cd "$SCRIPT_DIR/.." && pwd)"
BQ_BIN="$BOMTIQUE_REPO/.build/bomtique"
mkdir -p "$(dirname "$BQ_BIN")"
if [ ! -x "$BQ_BIN" ] || [ "$BOMTIQUE_REPO/cmd/bomtique" -nt "$BQ_BIN" ]; then
    echo "[bootstrap] building bomtique..." >&2
    (cd "$BOMTIQUE_REPO" && go build -o "$BQ_BIN" ./cmd/bomtique)
fi

CACHE_DIR="$(mktemp -d -t bq-bioexpress-cache.XXXXXX)"
trap 'rm -rf "$CACHE_DIR"' EXIT

bq() {
    if [ "$dry_run" = "1" ]; then
        printf '[dry-run] %s' "$BQ_BIN"
        for arg in "$@"; do printf ' %q' "$arg"; done
        printf '\n'
    else
        "$BQ_BIN" "$@"
    fi
}

# ---- Component registry -----------------------------------------------------
# The 17 version.txt locations discovered by the Explore agent,
# grouped by their owning pool.  Paths are relative to $repo so the
# script is portable across checkouts.
CFW_POOL_DIRS=(
    "CFW/CP.SOUP/mbed-crypto"
    "CFW/CP.SOUP/mbedtls"
    "CFW/CP.SOUP/mcuboot"
    "CFW/CP.SOUP/STM_HAL"
    "CFW/CP.SOUP/azure_iot_sdk"
    "CFW/CP.SOUP/azure_iot_sdk/umqtt"
    "CFW/CP.SOUP/azure_iot_sdk/uamqp"
    "CFW/CP.SOUP/azure_iot_sdk/c-utility"
    "CFW/CP.SOUP/azure_iot_sdk/iothub_service_client"
    "CFW/CP.SOUP/KVStore"
    "CFW/CP.SOUP/internal_flash"
    "CFW/CP.SOUP/mjson"
    "CFW/CP.SOUP/miniz"
    "CFW/CP.SOUP/printf"
    "CFW/CP.SOUP/threadx"
)

NS_POOL_DIRS=(
    "NonSecure/SOUP/emWin"
    "NonSecure/SOUP/react"
)

PRIMARIES=(
    "CI/CP.SBSFU:sbsfu:1.0.0"
    "CI/CP.Secure:secure:1.0.0"
    "CI/CP.NonSecure:nonsecure:1.0.0"
)

# ---- Phase 0: scaffold primaries --------------------------------------------

init_primaries() {
    for entry in "${PRIMARIES[@]}"; do
        IFS=: read -r rel name_suffix version <<<"$entry"
        local dir="$repo/$rel"
        if [ ! -d "$dir" ]; then
            echo "warning: primary dir $dir missing; skipping" >&2
            continue
        fi
        local primary_file="$dir/.primary.json"
        if [ -f "$primary_file" ] && [ "$force" != "1" ]; then
            echo "[init] $primary_file already exists (use --force to overwrite)"
            continue
        fi
        local args=(
            manifest init -C "$dir"
            --name "cp-bioexpress-$name_suffix"
            --version "$version"
            --type application
            --license "LicenseRef-Biotronik-Proprietary"
            --supplier "Biotronik"
            --purl "pkg:generic/biotronik/cp-bioexpress-$name_suffix@$version"
            --description "BioExpress $name_suffix partition"
        )
        [ "$force" = "1" ] && args+=(--force)
        echo "[init] $primary_file"
        bq "${args[@]}"
    done
}

# ---- Phase 1: pool ----------------------------------------------------------

# add_component <abs-component-dir> <components-file-path>
#
# Drives one `bomtique manifest add --vendored-at` for a single
# version.txt directory, applying registry enrichment when the
# version.txt carries a recognisable URL.
add_component() {
    local comp_dir="$1"
    local components_file="$2"

    local vfile="$comp_dir/version.txt"
    [ -f "$vfile" ] || { echo "warning: $vfile missing" >&2; return 0; }

    # Parse version.txt into a local associative array. Keys are
    # uppercased with `-` normalised to `_` (GIT-COMMIT → GIT_COMMIT).
    local -A fields=()
    parse_versiontxt "$vfile" fields

    local PROJECT="${fields[PROJECT]:-}"
    local VERSION="${fields[VERSION]:-}"
    local MANUFACTURER="${fields[MANUFACTURER]:-}"
    local LICENSE="${fields[LICENSE]:-}"
    local WEBSITE="${fields[WEBSITE]:-}"
    local URL="${fields[URL]:-}"
    local NOTES="${fields[NOTES]:-}"
    local GIT_COMMIT="${fields[GIT_COMMIT]:-}"

    # Fallback for bare-version files (e.g. azure_iot_sdk submodules
    # whose version.txt contents are literally "1.1.12\n"). When the
    # parser found no KEY:VALUE pairs, treat the file as a pure
    # version string and inherit other fields from the parent
    # directory's version.txt if one exists.
    if [ "${#fields[@]}" -eq 0 ]; then
        local bare
        bare="$(head -n1 "$vfile" | tr -d '\r\n' | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')"
        VERSION="$bare"
        PROJECT="$(basename "$comp_dir")"
        local parent_dir
        parent_dir="$(dirname "$comp_dir")"
        if [ -f "$parent_dir/version.txt" ]; then
            local -A parent_fields=()
            parse_versiontxt "$parent_dir/version.txt" parent_fields
            MANUFACTURER="${parent_fields[MANUFACTURER]:-$MANUFACTURER}"
            LICENSE="${parent_fields[LICENSE]:-$LICENSE}"
            WEBSITE="${parent_fields[WEBSITE]:-$WEBSITE}"
            URL="${parent_fields[URL]:-$URL}"
        fi
        echo "[bare] $vfile → version=$VERSION name=$PROJECT (inherited MANUFACTURER/LICENSE from parent)"
    fi

    local project_slug
    project_slug="$(slugify "${PROJECT:-unknown}")"

    local spdx=""
    spdx="$(normalise_license "${LICENSE:-}" "$project_slug")"
    if [ -z "$spdx" ] && [ -n "${LICENSE:-}" ]; then
        echo "warning: $vfile: unknown license $(printf '%q' "$LICENSE"); dropping field" >&2
    fi

    # Relative path from repo root, used in the repo-local purl and
    # as --vendored-at (relative to the components manifest's dir).
    local abs_comp_dir_rel
    abs_comp_dir_rel="$(realpath --relative-to="$repo" "$comp_dir")"
    local repo_purl
    repo_purl="$(repo_local_purl "${VERSION:-0}" "$abs_comp_dir_rel")"

    # --vendored-at must be relative to the components manifest's
    # directory, not the repo root.
    local components_dir
    components_dir="$(dirname "$components_file")"
    local vendored_at
    vendored_at="$(realpath --relative-to="$components_dir" "$comp_dir")"
    [[ "$vendored_at" =~ ^\. ]] || vendored_at="./$vendored_at"

    # Detect registry URL and (unless --offline) pre-fetch
    # enrichment data.
    local registry_purl=""
    local enriched_desc=""
    local -a enrich_external=()
    if [ "$offline" != "1" ]; then
        if registry_purl="$(detect_registry_purl "${GIT_COMMIT:-}" "${WEBSITE:-}" "${URL:-}" "${VERSION:-}")"; then
            if [ -n "$registry_purl" ]; then
                echo "[enrich] $PROJECT -> $registry_purl" >&2
                local reg_json=""
                if [ "$dry_run" != "1" ]; then
                    reg_json="$(registry_fetch_fields "$BQ_BIN" "$registry_purl" "$CACHE_DIR" || echo '{}')"
                fi
                if [ -n "$reg_json" ] && [ "$reg_json" != "{}" ] && [ "$reg_json" != "null" ]; then
                    enriched_desc="$(printf '%s' "$reg_json" | jq -r '.description // empty')"
                    while IFS= read -r line; do
                        enrich_external+=("$line")
                    done < <(external_refs_from_registry "$reg_json")
                fi
            fi
        fi
    fi

    # First line of NOTES (if any) feeds --description as a
    # fallback when the registry didn't provide one.
    local description=""
    if [ -n "$enriched_desc" ]; then
        description="$enriched_desc"
    elif [ -n "${NOTES:-}" ]; then
        description="$(printf '%s' "$NOTES" | head -n1)"
    fi

    # Pick an --ext filter that actually matches content. Most
    # components carry .c + .h, but JS-only vendored bits (react)
    # have nothing of that shape, and an empty filter would leave
    # the directory hash empty which §8.4 rejects.
    local ext_filter=""
    if find "$comp_dir" -maxdepth 3 \( -name '*.c' -o -name '*.h' \) -print -quit 2>/dev/null | grep -q .; then
        ext_filter="c,h"
    fi

    # Build the args array incrementally so empty values don't
    # create empty flags.
    local args=(
        manifest add
        --offline
        --into "$components_file"
        --name "${PROJECT:-unknown}"
        --version "${VERSION:-0}"
        --type library
        --purl "$repo_purl"
        --vendored-at "$vendored_at"
    )
    [ -n "$ext_filter" ] && args+=(--ext "$ext_filter")
    [ -n "$spdx" ]         && args+=(--license "$spdx")
    [ -n "${MANUFACTURER:-}" ] && args+=(--supplier "$MANUFACTURER")
    [ -n "${WEBSITE:-}" ]     && args+=(--website "$WEBSITE")
    [ -n "${URL:-}" ]         && args+=(--distribution "$URL")
    [ -n "$description" ]     && args+=(--description "$description")
    if [ "${#enrich_external[@]}" -gt 0 ]; then
        # external_refs_from_registry emits alternating "--external" and
        # "type=url" lines; append directly.
        args+=("${enrich_external[@]}")
    fi

    # Upstream ancestor: version.txt-supplied identity, with
    # registry purl promoted when available.
    args+=(
        --upstream-name "${PROJECT:-unknown}"
        --upstream-version "${VERSION:-0}"
    )
    [ -n "${MANUFACTURER:-}" ] && args+=(--upstream-supplier "$MANUFACTURER")
    [ -n "${WEBSITE:-}" ]     && args+=(--upstream-website "$WEBSITE")
    [ -n "$registry_purl" ]   && args+=(--upstream-purl "$registry_purl")

    echo "[add ] $PROJECT@${VERSION:-0}  ->  $components_file"
    bq "${args[@]}"
}

run_pool_phase() {
    init_primaries

    local cfw_file="$repo/CFW/CP.SOUP/.components.json"
    local ns_file="$repo/NonSecure/SOUP/.components.json"

    # Fresh start when --force is passed; otherwise append.
    if [ "$force" = "1" ]; then
        rm -f "$cfw_file" "$ns_file"
    fi

    for rel in "${CFW_POOL_DIRS[@]}"; do
        add_component "$repo/$rel" "$cfw_file"
    done
    for rel in "${NS_POOL_DIRS[@]}"; do
        add_component "$repo/$rel" "$ns_file"
    done

    # azure_iot_sdk: wire parent -> submodules via --depends-on.
    # The parent's purl must match what Phase 1 emitted; re-derive
    # it from version.txt. Submodule version.txt files are bare
    # version strings — read them directly rather than via the
    # KEY:VALUE parser.
    local parent_vfile="$repo/CFW/CP.SOUP/azure_iot_sdk/version.txt"
    local parent_version="0"
    if [ -f "$parent_vfile" ]; then
        local -A pfields=()
        parse_versiontxt "$parent_vfile" pfields
        parent_version="${pfields[VERSION]:-0}"
    fi
    local parent_purl
    parent_purl="$(repo_local_purl "$parent_version" "CFW/CP.SOUP/azure_iot_sdk")"

    local -a sub_purls=()
    for sub in umqtt uamqp c-utility iothub_service_client; do
        local sub_dir="CFW/CP.SOUP/azure_iot_sdk/$sub"
        local sub_vfile="$repo/$sub_dir/version.txt"
        [ -f "$sub_vfile" ] || continue
        local sv
        # Try KEY:VALUE shape first; fall back to first non-empty
        # line for bare-version files.
        local -A sf=()
        parse_versiontxt "$sub_vfile" sf
        sv="${sf[VERSION]:-}"
        if [ -z "$sv" ]; then
            sv="$(head -n1 "$sub_vfile" | tr -d '\r\n' | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')"
        fi
        sub_purls+=("$(repo_local_purl "$sv" "$sub_dir")")
    done

    if [ "${#sub_purls[@]}" -gt 0 ]; then
        echo "[wire] azure_iot_sdk parent=$parent_purl"
        echo "[wire]   +${#sub_purls[@]} submodules"
        if [ "$dry_run" = "1" ]; then
            printf '[dry-run] jq-edit %s: set depends-on on %s\n' "$cfw_file" "$parent_purl"
        else
            # bomtique's `manifest update` walks UP from CWD to find
            # the primary, then DOWN from the primary's dir to find
            # components manifests. CP.BioExpress has primaries
            # nested in /CI/CP.*/ and a pool at /CFW/CP.SOUP/ that
            # those walks can't bridge. Edit the depends-on in place
            # via jq — the semantic outcome is identical.
            local deps_json
            deps_json="$(printf '%s\n' "${sub_purls[@]}" | jq -R . | jq -s .)"
            local tmp
            tmp="$(mktemp)"
            jq --arg purl "$parent_purl" --argjson deps "$deps_json" '
                .components |= map(
                    if .purl == $purl then
                        .["depends-on"] = $deps
                    else . end
                )
            ' "$cfw_file" > "$tmp" && mv "$tmp" "$cfw_file"
        fi
    fi
}

# ---- Phase 2: primaries ----------------------------------------------------

run_primaries_phase() {
    local tsv="$repo/.bomtique-deps.tsv"
    if [ -f "$tsv" ] && [ "$force" != "1" ]; then
        echo "[deps] $tsv already exists (use --force to regenerate)"
    else
        echo "[deps] generating $tsv"
        : > "$tsv"
        # Collect all component dirs for matching.
        local -a all_dirs=()
        for rel in "${CFW_POOL_DIRS[@]}" "${NS_POOL_DIRS[@]}"; do
            all_dirs+=("$rel")
        done
        for entry in "${PRIMARIES[@]}"; do
            IFS=: read -r primary_rel name_suffix _ <<<"$entry"
            local makefile="$repo/$primary_rel/Makefile"
            if [ ! -f "$makefile" ]; then
                echo "warning: $makefile missing; skipping deps for $name_suffix" >&2
                continue
            fi
            local matches
            matches="$(parse_makefile_deps "$makefile" "${all_dirs[@]}" || true)"
            while IFS= read -r match; do
                [ -z "$match" ] && continue
                # Read the matching component's version from its
                # version.txt so we emit the exact repo-local purl.
                # Both KEY:VALUE and bare-version shapes must work,
                # so reuse the full parser and fall back to the
                # first non-empty line when no VERSION key exists.
                local v="0"
                if [ -f "$repo/$match/version.txt" ]; then
                    local -A mfields=()
                    parse_versiontxt "$repo/$match/version.txt" mfields
                    v="${mfields[VERSION]:-}"
                    if [ -z "$v" ]; then
                        v="$(head -n1 "$repo/$match/version.txt" | tr -d '\r\n' | sed -E 's/^[[:space:]]+|[[:space:]]+$//g' || true)"
                    fi
                    [ -z "$v" ] && v="0"
                fi
                local purl
                purl="$(repo_local_purl "$v" "$match")"
                printf '%s\t%s\n' "$name_suffix" "$purl" >> "$tsv"
            done <<<"$matches"
        done
        echo "[deps] wrote $(wc -l <"$tsv") rows to $tsv"
        cat "$tsv"
    fi

    if [ "$dry_run" = "1" ] && [ "$force" != "1" ]; then
        echo "[deps] --dry-run: stopping before real manifest add --primary calls"
        return 0
    fi

    # Apply the TSV.
    while IFS=$'\t' read -r name_suffix purl; do
        [ -z "$purl" ] && continue
        local primary_dir=""
        for entry in "${PRIMARIES[@]}"; do
            IFS=: read -r rel suffix _ <<<"$entry"
            if [ "$suffix" = "$name_suffix" ]; then
                primary_dir="$repo/$rel"
                break
            fi
        done
        [ -z "$primary_dir" ] && continue
        # Derive a readable name/version from the purl for the
        # primary's depends-on diagnostic; the ref is what matters.
        local name ver
        name="$(printf '%s' "$purl" | sed -E 's|^.*/||; s|@.*$||')"
        ver="$(printf '%s' "$purl" | sed -E 's|^.*@||')"
        echo "[prim] $name_suffix depends on $purl"
        bq manifest add --primary --offline \
            -C "$primary_dir" \
            --name "$name" --version "$ver" --purl "$purl"
    done < "$tsv"
}

# ---- Phase 3: scan ---------------------------------------------------------

run_scan_phase() {
    local stamp
    stamp="$(git -C "$repo" log -1 --format=%ct 2>/dev/null || date +%s)"
    local out_dir="$repo/sbom"
    mkdir -p "$out_dir"

    for fmt in cyclonedx spdx; do
        echo "[scan] $fmt (SOURCE_DATE_EPOCH=$stamp)"
        bq scan \
            --source-date-epoch "$stamp" \
            --out "$out_dir" \
            --format "$fmt" \
            --output-validate \
            "$repo/CI" "$repo/CFW/CP.SOUP" "$repo/NonSecure/SOUP"
    done
    if [ "$dry_run" != "1" ]; then
        echo "[scan] files in $out_dir:"
        ls -1 "$out_dir"
    fi
}

# ---- Dispatch ---------------------------------------------------------------

case "$step" in
    pool)       run_pool_phase ;;
    primaries)  run_primaries_phase ;;
    scan)       run_scan_phase ;;
    all)
        run_pool_phase
        run_primaries_phase
        run_scan_phase
        ;;
    *) echo "unknown --step: $step (expected pool|primaries|scan|all)" >&2; exit 2 ;;
esac
