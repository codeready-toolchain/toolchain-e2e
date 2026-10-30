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

CREDENTIALS_FILE="${SCRIPT_DIR}/aws-credentials"
if [[ -f "${CREDENTIALS_FILE}" ]]; then
  # shellcheck source=/dev/null
  source "${CREDENTIALS_FILE}"
fi

if [[ -z "${CLUSTER_NAME:-}" ]]; then
  echo "ERROR: CLUSTER_NAME is not set in ${CONFIG_FILE}"
  exit 1
fi

CLUSTER_DIR="${SCRIPT_DIR}/clusters/${CLUSTER_NAME}"

if [[ ! -d "${CLUSTER_DIR}" ]]; then
  echo "ERROR: Cluster directory not found: ${CLUSTER_DIR}"
  echo "Nothing to destroy."
  exit 1
fi

if ! aws sts get-caller-identity &>/dev/null; then
  echo "ERROR: AWS credentials not configured or expired"
  echo "Configure credentials via: aws configure, AWS env vars, or aws sso login"
  exit 1
fi

echo "Destroying cluster '${CLUSTER_NAME}'..."
echo "  Cluster Dir: ${CLUSTER_DIR}"
echo ""

openshift-install destroy cluster --dir="${CLUSTER_DIR}" --log-level=info

echo ""
echo "Cleaning up cluster directory..."
rm -rf "${CLUSTER_DIR}"

echo "Done. Cluster '${CLUSTER_NAME}' has been destroyed."
