#!/bin/bash
set -eu

script_directory="$(cd "$(dirname "$0")" && pwd)"
failed_images=""
tested_images=""

while IFS= read -r base_image <&3 || [ -n "$base_image" ]; do
    case "$base_image" in
        ""|\#*)
            continue
            ;;
    esac
    tested_images="${tested_images} ${base_image}"
    printf '=== %s ===\n' "$base_image"
    if BASE_IMAGE="$base_image" "${script_directory}/image/validate-one.sh" </dev/null; then
        printf 'PASS %s\n' "$base_image"
    else
        printf 'FAIL %s\n' "$base_image" >&2
        failed_images="${failed_images} ${base_image}"
    fi
done 3< "${script_directory}/images.txt"

if [ -z "$tested_images" ]; then
    echo "no images listed" >&2
    exit 1
fi
if [ -n "$failed_images" ]; then
    printf 'validation failed:%s\n' "$failed_images" >&2
    exit 1
fi
printf 'validation passed:%s\n' "$tested_images"
