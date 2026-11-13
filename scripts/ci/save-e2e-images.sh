#!/usr/bin/env bash
set -euo pipefail

dir="${E2E_IMAGE_LIST_DIR:-${GITHUB_WORKSPACE:-/tmp}/e2e-images}"
list="${dir}/images.txt"

if [[ ! -s "${list}" ]]; then
    echo "no e2e images recorded in ${list}" >&2
    exit 1
fi

images=()
while IFS= read -r ref; do
    [[ -z "${ref}" ]] && continue
    images+=("${ref}")
done < "${list}"

podman save -o "${dir}/e2e-images.tar" "${images[@]}"
