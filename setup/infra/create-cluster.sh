#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/config"

if [[ ! -f "${CONFIG_FILE}" ]]; then
  echo "ERROR: ${CONFIG_FILE} not found."
  echo "Copy config.example and customize:"
  echo "  cp ${SCRIPT_DIR}/config.example ${CONFIG_FILE}"
  exit 1
fi

# shellcheck source=config.example
source "${CONFIG_FILE}"

CREDENTIALS_FILE="${SCRIPT_DIR}/aws-credentials"
if [[ -f "${CREDENTIALS_FILE}" ]]; then
  # shellcheck source=/dev/null
  source "${CREDENTIALS_FILE}"
fi

for var in CLUSTER_NAME BASE_DOMAIN AWS_REGION AWS_ZONE MASTER_TYPE WORKER_TYPE WORKER_REPLICAS PULL_SECRET_FILE OCP_VERSION; do
  if [[ -z "${!var:-}" ]]; then
    echo "ERROR: ${var} is not set in ${CONFIG_FILE}"
    exit 1
  fi
done

if [[ "${CLUSTER_NAME}" == *CHANGEME* ]]; then
  echo "ERROR: Set CLUSTER_NAME in ${CONFIG_FILE} (currently contains CHANGEME)"
  exit 1
fi

if ! command -v openshift-install &>/dev/null; then
  echo "ERROR: openshift-install not found in PATH"
  echo "Download from: https://mirror.openshift.com/pub/openshift-v4/clients/ocp/"
  exit 1
fi

INSTALLED_VERSION=$(openshift-install version | head -1 | grep -oE '[0-9]+\.[0-9]+')
if [[ "${INSTALLED_VERSION}" != "${OCP_VERSION}" ]]; then
  echo "ERROR: openshift-install version mismatch"
  echo "  Config requires: ${OCP_VERSION}"
  echo "  Installed:       ${INSTALLED_VERSION} ($(command -v openshift-install))"
  echo "Download the correct version from: https://mirror.openshift.com/pub/openshift-v4/clients/ocp/"
  exit 1
fi

if ! command -v oc &>/dev/null; then
  echo "ERROR: oc not found in PATH"
  exit 1
fi

if ! aws sts get-caller-identity &>/dev/null; then
  echo "ERROR: AWS credentials not configured or expired"
  echo "Configure credentials via: aws configure, AWS env vars, or aws sso login"
  exit 1
fi

if [[ ! -f "${PULL_SECRET_FILE}" ]]; then
  echo "ERROR: Pull secret not found at ${PULL_SECRET_FILE}"
  echo "Download from: https://console.redhat.com/openshift/install/pull-secret"
  exit 1
fi

PULL_SECRET="$(cat "${PULL_SECRET_FILE}")"

SSH_KEY=""
if [[ -n "${SSH_KEY_FILE:-}" && -f "${SSH_KEY_FILE}" ]]; then
  SSH_KEY="$(cat "${SSH_KEY_FILE}")"
elif [[ -n "${SSH_KEY_FILE:-}" ]]; then
  echo "WARNING: SSH key file not found at ${SSH_KEY_FILE} — proceeding without SSH key"
fi

CLUSTER_DIR="${SCRIPT_DIR}/clusters/${CLUSTER_NAME}"

if [[ -d "${CLUSTER_DIR}" ]]; then
  echo "ERROR: Cluster directory already exists: ${CLUSTER_DIR}"
  echo "If a previous install failed, run ./destroy-cluster.sh first or remove the directory manually."
  exit 1
fi

mkdir -p "${CLUSTER_DIR}"

echo "Generating install-config.yaml..."
sed \
  -e "s|\${CLUSTER_NAME}|${CLUSTER_NAME}|g" \
  -e "s|\${BASE_DOMAIN}|${BASE_DOMAIN}|g" \
  -e "s|\${AWS_REGION}|${AWS_REGION}|g" \
  -e "s|\${AWS_ZONE}|${AWS_ZONE}|g" \
  -e "s|\${MASTER_TYPE}|${MASTER_TYPE}|g" \
  -e "s|\${WORKER_TYPE}|${WORKER_TYPE}|g" \
  -e "s|\${WORKER_REPLICAS}|${WORKER_REPLICAS}|g" \
  -e "s|\${PULL_SECRET}|${PULL_SECRET}|g" \
  -e "s|\${SSH_KEY}|${SSH_KEY}|g" \
  "${SCRIPT_DIR}/install-config.yaml.template" > "${CLUSTER_DIR}/install-config.yaml"

cp "${CLUSTER_DIR}/install-config.yaml" "${CLUSTER_DIR}/install-config.yaml.backup"

echo ""
echo "=== Cluster Configuration ==="
echo "  Name:         ${CLUSTER_NAME}"
echo "  Base Domain:  ${BASE_DOMAIN}"
echo "  Region:       ${AWS_REGION}"
echo "  Masters:      3x ${MASTER_TYPE}"
echo "  Workers:      ${WORKER_REPLICAS}x ${WORKER_TYPE}"
echo "  OCP Version:  $(openshift-install version | head -1)"
echo "  Cluster Dir:  ${CLUSTER_DIR}"
echo ""
echo "Creating cluster (this takes ~40 minutes)..."
echo ""

openshift-install create cluster --dir="${CLUSTER_DIR}" --log-level=info

echo ""
echo "=== Cluster Ready ==="
echo "  Kubeconfig: ${CLUSTER_DIR}/auth/kubeconfig"
echo "  Password:   ${CLUSTER_DIR}/auth/kubeadmin-password"
echo ""
echo "To access the cluster:"
echo "  export KUBECONFIG=${CLUSTER_DIR}/auth/kubeconfig"
echo "  oc whoami"
echo ""
echo "To install Sandbox operators (from project root):"
echo "  make dev-deploy-latest"
