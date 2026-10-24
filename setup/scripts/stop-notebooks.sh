#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<EOF
Usage: $0 <kubeconfig>

List all kubeflow.org/v1beta1 Notebook resources and add the
kubeflow-resource-stopped annotation to stop each one.

Arguments:
  kubeconfig    Path to the kubeconfig file
EOF
  exit 1
}

if [[ $# -ne 1 ]] || [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; then
  usage
fi

KUBECONFIG_PATH="$1"

if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
  echo "error: kubeconfig not found: ${KUBECONFIG_PATH}" >&2
  exit 1
fi

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc is required but was not found in PATH" >&2
  exit 1
fi

OC=(oc --kubeconfig="${KUBECONFIG_PATH}")
STOPPED_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
count=0

while IFS='/' read -r ns name; do
  [[ -z "${ns}" || -z "${name}" ]] && continue
  echo "Stopping ${ns}/${name}"
  "${OC[@]}" annotate "notebooks.v1beta1.kubeflow.org/${name}" \
    -n "${ns}" \
    "kubeflow-resource-stopped=${STOPPED_AT}" \
    --overwrite
  count=$((count + 1))
done < <("${OC[@]}" get notebooks.v1beta1.kubeflow.org --all-namespaces -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}')

if [[ "${count}" -eq 0 ]]; then
  echo "No kubeflow.org/v1beta1 Notebook resources found."
  exit 0
fi

echo "Stopped ${count} Notebook resource(s)."
