#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/config"

if [[ ! -f "${CONFIG_FILE}" ]]; then
  echo "ERROR: ${CONFIG_FILE} not found."
  exit 1
fi

# shellcheck source=config.example
source "${CONFIG_FILE}"

if [[ -z "${CLUSTER_NAME:-}" ]]; then
  echo "ERROR: CLUSTER_NAME is not set in ${CONFIG_FILE}"
  exit 1
fi

CLUSTER_DIR="${SCRIPT_DIR}/clusters/${CLUSTER_NAME}"

if [[ ! -d "${CLUSTER_DIR}" ]]; then
  echo "No cluster directory to clean up: ${CLUSTER_DIR}"
  exit 0
fi

if [[ -f "${CLUSTER_DIR}/metadata.json" ]]; then
  echo "ERROR: ${CLUSTER_DIR}/metadata.json exists — this cluster may have AWS resources."
  echo "Use 'make cluster-destroy' instead to clean up AWS resources first."
  exit 1
fi

echo "Removing local cluster state: ${CLUSTER_DIR}"
rm -rf "${CLUSTER_DIR}"
echo "Done. You can now run 'make cluster-create' again."