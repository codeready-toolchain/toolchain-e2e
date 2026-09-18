#!/usr/bin/env bash
set -euo pipefail

# Staged perf tests: additive users, no cleanup between stages.
# RHOAI must be installed before running this script (SM3 is not required on OCP 4.21+).

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

KUBEADMIN_PASSWORD="$(cat "${SCRIPT_DIR}/clusters/${CLUSTER_NAME}/auth/kubeadmin-password")"
oc login -u kubeadmin -p "${KUBEADMIN_PASSWORD}" --insecure-skip-tls-verify=true

# RHOAI dependency operators
WORKLOADS="cert-manager-operator:cert-manager-operator-controller-manager"
WORKLOADS+=",cert-manager:cert-manager"
WORKLOADS+=",cert-manager:cert-manager-cainjector"
WORKLOADS+=",cert-manager:cert-manager-webhook"
WORKLOADS+=",openshift-jobset-system:jobset-operator"
WORKLOADS+=",openshift-nfd:nfd-controller-manager"
WORKLOADS+=",nvidia-gpu-operator:gpu-operator"
# RHOAI operator and components (must match DSC — verify with:
#   oc get deployments -n redhat-ods-operator -o name
#   oc get deployments -n redhat-ods-applications -o name)
WORKLOADS+=",redhat-ods-operator:rhods-operator"
WORKLOADS+=",redhat-ods-applications:dashboard-redirect"
WORKLOADS+=",redhat-ods-applications:data-science-pipelines-operator-controller-manager"
WORKLOADS+=",redhat-ods-applications:kserve-controller-manager"
WORKLOADS+=",redhat-ods-applications:kubeflow-training-operator"
WORKLOADS+=",redhat-ods-applications:llmisvc-controller-manager"
WORKLOADS+=",redhat-ods-applications:mlflow-operator-controller-manager"
WORKLOADS+=",redhat-ods-applications:model-serving-api"
WORKLOADS+=",redhat-ods-applications:notebook-controller-deployment"
WORKLOADS+=",redhat-ods-applications:odh-model-controller"
WORKLOADS+=",redhat-ods-applications:odh-notebook-controller-manager"
WORKLOADS+=",redhat-ods-applications:ogx-k8s-operator-controller-manager"
WORKLOADS+=",redhat-ods-applications:rhods-dashboard"

RESULTS_DIR="tmp/results"
mkdir -p "${RESULTS_DIR}"

run_test() {
  local desc="$1"; shift
  local stamp
  stamp="$(date +%Y-%m-%d_%H:%M:%S)"
  local testname=""
  for arg in "$@"; do
    if [[ "${prev:-}" == "--testname" || "${arg}" == --testname=* ]]; then
      testname="${arg#--testname=}"
      break
    fi
    prev="${arg}"
  done
  local logfile="${RESULTS_DIR}/${stamp}-${testname:-unknown}.log"

  echo "--- ${desc} ---"
  echo "Logging to: ${logfile}"
  timeout --signal=KILL 3600 go run setup/main.go "$@" 2>&1 | tee "${logfile}"
  local rc=${PIPESTATUS[0]}
  if [[ ${rc} -ne 0 ]]; then
    echo "WARNING: test '${desc}' exited ${rc} or was killed by timeout"
  fi
  return ${rc}
}

echo "===== Stage 1: Baseline — 1 default user ====="

run_test "Stage 1: 1-user baseline" --users 1 --default 1 --custom 0 \
  --username baseline --testname=stage1-baseline \
  --workloads "${WORKLOADS}" --interactive=false

echo "===== Stage 2: 2000 default users ====="

run_test "Stage 2: 2000 default users" --users 2000 --default 2000 --custom 0 \
  --username rhoai35 --testname=stage2-2k-default \
  --workloads "${WORKLOADS}" --interactive=false

echo "===== Stage 3: +10 RHOAI custom users ====="

run_test "Stage 3: 10 RHOAI custom users" --users 10 --default 0 --custom 10 \
  --template setup/resources/rhoai-user-workloads.yaml \
  --username rhoai-c1 --testname=stage3-custom-10 \
  --workloads "${WORKLOADS}" --interactive=false

echo "===== Stage 4: +50 RHOAI custom users (cumulative 60) ====="

run_test "Stage 4: 50 RHOAI custom users" --users 50 --default 0 --custom 50 \
  --template setup/resources/rhoai-user-workloads.yaml \
  --username rhoai-c2 --testname=stage4-custom-50 \
  --workloads "${WORKLOADS}" --interactive=false

echo "===== Stage 5: +200 RHOAI custom users (cumulative 260) ====="

run_test "Stage 5: 200 RHOAI custom users" --users 200 --default 0 --custom 200 \
  --template setup/resources/rhoai-user-workloads.yaml \
  --username rhoai-c3 --testname=stage5-custom-200 \
  --workloads "${WORKLOADS}" --interactive=false

echo ""
echo "===== ALL 5 STAGES COMPLETE ====="
echo "Results in tmp/results/"
ls -lt tmp/results/*.csv | head -10
