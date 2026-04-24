#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Interlynk.io
# SPDX-License-Identifier: Apache-2.0
#
# Helper functions for scripts/import-cp-bioexpress.sh. Sourced by
# the driver; not intended to be run directly.

set -euo pipefail

# parse_versiontxt <file> <assoc-array-name>
# Populates the named associative array with one entry per KEY from
# the file. Keys are normalised (uppercase, hyphens replaced with
# underscores so GIT-COMMIT becomes GIT_COMMIT). Values have CRLF
# line endings and trailing whitespace stripped. The NOTES block is
# joined into a single multiline value under key NOTES.
#
# Requires bash 4 (declare -n). Caller declares the array:
#   declare -A fields
#   parse_versiontxt "$vfile" fields
parse_versiontxt() {
    local file="$1"
    local -n _out="$2"
    local in_notes=0
    local notes=""
    local line
    while IFS= read -r line || [ -n "$line" ]; do
        # Strip trailing CR (many version.txt files use CRLF).
        line="${line%$'\r'}"
        if [ "$in_notes" = "1" ]; then
            if [ -n "$notes" ]; then
                notes+=$'\n'
            fi
            notes+="$line"
            continue
        fi
        if [[ "$line" =~ ^[[:space:]]*NOTES[[:space:]]*: ]]; then
            in_notes=1
            continue
        fi
        if [[ "$line" =~ ^[[:space:]]*([A-Z][A-Z0-9_-]*)[[:space:]]*:[[:space:]]*(.*)$ ]]; then
            local key="${BASH_REMATCH[1]}"
            local val="${BASH_REMATCH[2]}"
            val="${val%"${val##*[![:space:]]}"}"  # rtrim
            # Normalise key: hyphens → underscores so callers can
            # use ${fields[GIT_COMMIT]} rather than an eval trick.
            key="${key//-/_}"
            _out["$key"]="$val"
        fi
    done < "$file"
    if [ -n "$notes" ]; then
        _out["NOTES"]="$notes"
    fi
}

# slugify <string>
# Lowercases and replaces every non-alphanumeric byte with `-`.
# Collapses runs of `-`. Used for repo-local purl name segments.
slugify() {
    local s="$1"
    s="${s,,}"                          # lowercase
    s="$(printf '%s' "$s" | sed -E 's/[^a-z0-9]+/-/g; s/^-+|-+$//g')"
    printf '%s' "$s"
}

# normalise_license <LICENSE-string> <PROJECT-slug>
# Echoes a valid SPDX expression or empty when we can't map.
normalise_license() {
    local lic="${1:-}"
    local slug="${2:-unknown}"
    case "${lic^^}" in
        ""|"NONE")
            printf '' ;;
        "APACHE-2.0"|"APACHE 2.0"|"APACHE"|"APACHE LICENSE 2.0"|"APACHE SOFTWARE LICENSE")
            printf 'Apache-2.0' ;;
        "MIT"|"MIT LICENSE")
            printf 'MIT' ;;
        "BSD-3-CLAUSE"|"BSD 3-CLAUSE"|"BSD"|"BSD LICENSE"|"NEW BSD LICENSE")
            printf 'BSD-3-Clause' ;;
        "BSD-2-CLAUSE"|"BSD 2-CLAUSE"|"SIMPLIFIED BSD")
            printf 'BSD-2-Clause' ;;
        "ISC"|"ISC LICENSE")
            printf 'ISC' ;;
        "MPL-2.0"|"MPL 2.0"|"MOZILLA PUBLIC LICENSE 2.0")
            printf 'MPL-2.0' ;;
        "GPL-2.0"|"GPL V2"|"GNU GPL V2")
            printf 'GPL-2.0-only' ;;
        "GPL-3.0"|"GPL V3"|"GNU GPL V3")
            printf 'GPL-3.0-only' ;;
        "LGPL-2.1"|"LGPL V2.1")
            printf 'LGPL-2.1-only' ;;
        "LGPL-3.0"|"LGPL V3")
            printf 'LGPL-3.0-only' ;;
        "UNLICENSE")
            printf 'Unlicense' ;;
        "CC0-1.0"|"CC0")
            printf 'CC0-1.0' ;;
        "COMMERCIAL"|"PROPRIETARY")
            # SPDX LicenseRef- form: valid shape, unresolvable ID.
            printf 'LicenseRef-%s-Commercial' "${slug^}"
            ;;
        *)
            # Unknown — let the caller emit a warning.
            printf '' ;;
    esac
}

# detect_registry_purl <GIT-COMMIT-url> <WEBSITE-url> <URL-url> <VERSION>
# Echoes a pkg:github/..., pkg:gitlab/..., or pkg:npm/... purl when
# the inputs contain a recognisable shape. Prefers GIT-COMMIT
# (the tightest reference), then WEBSITE, then URL.
detect_registry_purl() {
    local git_commit="${1:-}"
    local website="${2:-}"
    local url="${3:-}"
    local version="${4:-}"

    for candidate in "$git_commit" "$website" "$url"; do
        [ -z "$candidate" ] && continue
        # GitHub: extract owner/repo plus optional tag/branch from
        # the path after /tree/ or /releases/tag/.
        if [[ "$candidate" =~ ^https?://github\.com/([^/]+)/([^/]+)(/.*)?$ ]]; then
            local owner="${BASH_REMATCH[1]}"
            local repo="${BASH_REMATCH[2]}"
            repo="${repo%.git}"
            local rest="${BASH_REMATCH[3]:-}"
            local tag=""
            if [[ "$rest" =~ /(tree|releases/tag|commits|commit)/([^/]+) ]]; then
                tag="${BASH_REMATCH[2]}"
            fi
            [ -z "$tag" ] && tag="$version"
            if [ -n "$tag" ]; then
                printf 'pkg:github/%s/%s@%s' "$owner" "$repo" "$tag"
            else
                printf 'pkg:github/%s/%s' "$owner" "$repo"
            fi
            return 0
        fi
        if [[ "$candidate" =~ ^https?://gitlab\.com/(.+)$ ]]; then
            local path="${BASH_REMATCH[1]}"
            # Strip trailing /, .git, /-/tree/..., /-/tags/..., etc.
            path="${path%.git}"
            path="${path%/}"
            local tag=""
            if [[ "$path" =~ ^(.+)/-/(tree|tags|commits)/(.+)$ ]]; then
                path="${BASH_REMATCH[1]}"
                tag="${BASH_REMATCH[3]}"
            fi
            [ -z "$tag" ] && tag="$version"
            if [ -n "$tag" ]; then
                printf 'pkg:gitlab/%s@%s' "$path" "$tag"
            else
                printf 'pkg:gitlab/%s' "$path"
            fi
            return 0
        fi
        if [[ "$candidate" =~ ^https?://(www\.)?npmjs\.com/package/([^/]+)(/v/([^/]+))?$ ]]; then
            local pkgname="${BASH_REMATCH[2]}"
            local pkgver="${BASH_REMATCH[4]:-$version}"
            if [ -n "$pkgver" ]; then
                printf 'pkg:npm/%s@%s' "$pkgname" "$pkgver"
            else
                printf 'pkg:npm/%s' "$pkgname"
            fi
            return 0
        fi
    done
    return 1
}

# registry_fetch_fields <bomtique-bin> <registry-purl> <cache-dir>
# Runs a scratch `bomtique manifest add --online` into a tmp dir so
# we can harvest the importer's output. Prints the component entry
# as a single line of JSON on stdout. Cached by purl.
registry_fetch_fields() {
    local bq="$1"
    local purl="$2"
    local cache_dir="$3"

    local cache_key
    cache_key="$(printf '%s' "$purl" | sed -E 's/[^A-Za-z0-9]+/_/g')"
    local cache_file="$cache_dir/$cache_key.json"

    if [ -f "$cache_file" ]; then
        cat "$cache_file"
        return 0
    fi

    local scratch
    scratch="$(mktemp -d)"
    trap '[ -n "${scratch:-}" ] && rm -rf "$scratch"' RETURN

    # Scratch primary.
    if ! "$bq" manifest init -C "$scratch" \
        --name pre --version 0 \
        --license Apache-2.0 \
        --purl "pkg:generic/regfetch-prewarm@0" >/dev/null 2>&1; then
        echo "registry_fetch_fields: init failed" >&2
        return 1
    fi

    # Try the fetch; on failure, return empty JSON.
    if ! "$bq" manifest add -C "$scratch" \
        --online \
        --purl "$purl" \
        --name prefetch \
        --version 0 >/dev/null 2>&1; then
        echo '{}' > "$cache_file"
        cat "$cache_file"
        return 0
    fi

    local components="$scratch/.components.json"
    if [ ! -f "$components" ]; then
        echo '{}' > "$cache_file"
        cat "$cache_file"
        return 0
    fi
    # Extract the first (only) component.
    jq -c '.components[0]' "$components" > "$cache_file"
    cat "$cache_file"
}

# external_refs_from_registry <component-json>
# Emits one --external "type=url" argument per line for each
# external_references entry in the registry component. Callers feed
# these into the real `manifest add` so the enrichment lands on the
# final component as additional references.
external_refs_from_registry() {
    local json="$1"
    [ -z "$json" ] && return 0
    [ "$json" = "null" ] && return 0
    [ "$json" = "{}" ] && return 0
    printf '%s' "$json" | jq -r '.external_references[]? | "--external\n\(.type)=\(.url)"'
}

# make_relpath <repo-root> <absolute-path>
# Returns the path of the absolute-path relative to repo-root, with
# a leading `./`. Used for the `--vendored-at` argument.
make_relpath() {
    local root="$1"
    local target="$2"
    local rel
    rel="$(realpath --relative-to="$root" "$target")"
    printf './%s' "$rel"
}

# repo_local_purl <version> <relpath-without-leading-dotslash>
# Builds the synthetic pkg:generic purl for a repo-local component.
repo_local_purl() {
    local version="$1"
    local relpath="$2"
    # Strip leading ./ and trailing /.
    relpath="${relpath#./}"
    relpath="${relpath%/}"
    # Lowercase and slugify every path segment.
    local IFS=/
    local segs=()
    for seg in $relpath; do
        segs+=("$(slugify "$seg")")
    done
    unset IFS
    local joined
    joined="$(IFS=/; printf '%s' "${segs[*]}")"
    printf 'pkg:generic/biotronik/cp-bioexpress/%s@%s' "$joined" "$(slugify "$version")"
}

# parse_makefile_deps <makefile-path> <components-roots...>
# BioExpress Makefiles are subdir.mk-style — the per-target Makefile
# is a flat list of `include <path>/subdir.mk` directives, no -I /
# CFLAGS / C_SOURCES. Extract each included path's directory prefix
# and match against the declared component roots. A path
# `CFW/CP.SOUP/mbedtls/library/subdir.mk` matches component root
# `CFW/CP.SOUP/mbedtls`. Nested submodules (azure_iot_sdk/umqtt)
# take precedence over the parent (azure_iot_sdk) when a prefix
# match is possible.
parse_makefile_deps() {
    local makefile="$1"
    shift
    local -a roots=("$@")

    # Sort roots by descending path length so the most-specific
    # (longest) prefix wins when matching.
    local sorted_roots
    sorted_roots="$(printf '%s\n' "${roots[@]}" | awk '{print length, $0}' | sort -rn | cut -d' ' -f2-)"

    local -A hits=()
    while IFS= read -r line; do
        # include <path>/subdir.mk
        if [[ "$line" =~ ^[[:space:]]*include[[:space:]]+(.+/)?subdir\.mk ]]; then
            local path="${BASH_REMATCH[1]%/}"
            # Strip the `../` Makefile prefix if present.
            path="${path#../}"
            for root in $sorted_roots; do
                # Match when the root is a prefix of the path OR a
                # substring (basename match for paths like
                # `../NonSecure/SOUP/emWin/Core`).
                if [[ "$path" == "$root" ]] || [[ "$path" == "$root"/* ]] || [[ "$path" == *"/$root" ]] || [[ "$path" == *"/$root"/* ]]; then
                    hits["$root"]=1
                    break   # most-specific wins; stop scanning
                fi
            done
        fi
        # -I flags in case some Makefile uses them.
        while [[ "$line" =~ -I[[:space:]]*([^[:space:]]+) ]]; do
            local inc="${BASH_REMATCH[1]}"
            line="${line#*"${BASH_REMATCH[0]}"}"
            for root in $sorted_roots; do
                if [[ "$inc" == *"/$root"/* ]] || [[ "$inc" == *"/$root" ]] || [[ "$inc" == "$root"/* ]]; then
                    hits["$root"]=1
                    break
                fi
            done
        done
    done < "$makefile"

    for root in "${!hits[@]}"; do
        printf '%s\n' "$root"
    done | sort
}
