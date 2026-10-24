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

export KUBECONFIG="${SCRIPT_DIR}/clusters/${CLUSTER_NAME}/auth/kubeconfig"

if [[ ! -f "${KUBECONFIG}" ]]; then
  echo "ERROR: Kubeconfig not found at ${KUBECONFIG}"
  echo "Run 'make cluster-create' first."
  exit 1
fi

if ! oc whoami &>/dev/null; then
  echo "ERROR: Cannot connect to cluster. Check KUBECONFIG."
  exit 1
fi

# Check if already installed
if oc get csv -n openshift-operators 2>/dev/null | grep -q "servicemeshoperator3.*Succeeded"; then
  echo "Service Mesh 3 operator is already installed."
  oc get csv -n openshift-operators | grep servicemeshoperator3
  exit 0
fi

echo "Installing Service Mesh 3 operator..."

oc apply -f "${SCRIPT_DIR}/manifests/servicemesh-subscription.yaml"

echo "Waiting for Service Mesh 3 CSV to reach Succeeded..."
for i in $(seq 1 60); do
  if oc get csv -n openshift-operators 2>/dev/null | grep -q "servicemeshoperator3.*Succeeded"; then
    echo "Service Mesh 3 operator installed successfully."
    oc get csv -n openshift-operators | grep servicemeshoperator3
    exit 0
  fi
  echo "  Waiting... (${i}/60)"
  sleep 10
done

echo "ERROR: Service Mesh 3 CSV did not reach Succeeded after 10 minutes."
oc get csv -n openshift-operators | grep servicemeshoperator3 || true
exit 1
