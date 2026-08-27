#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<EOF
Usage: $0 <kubeconfig>

Delete ConfigMaps in this order:
  1. name odh-trusted-ca-bundle with label config.openshift.io/inject-trusted-cabundle=true
  2. name odh-kserve-custom-ca-bundle with label opendatahub.io/managed=true
     (only after every odh-trusted-ca-bundle ConfigMap has been deleted)

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
CABUNDLE_LABEL="config.openshift.io/inject-trusted-cabundle=true"
CABUNDLE_NAME="odh-trusted-ca-bundle"
KSERVE_CA_LABEL="opendatahub.io/managed=true"
KSERVE_CA_NAME="odh-kserve-custom-ca-bundle"

delete_configmaps() {
  local label="$1"
  local name="$2"
  "${OC[@]}" delete configmap --all-namespaces \
    -l "${label}" \
    --field-selector "metadata.name=${name}" \
    --ignore-not-found --wait=false
}

echo "Deleting ${CABUNDLE_NAME} ConfigMaps..."
delete_configmaps "${CABUNDLE_LABEL}" "${CABUNDLE_NAME}" true

echo "Deleting ${KSERVE_CA_NAME} ConfigMaps..."
delete_configmaps "${KSERVE_CA_LABEL}" "${KSERVE_CA_NAME}"
