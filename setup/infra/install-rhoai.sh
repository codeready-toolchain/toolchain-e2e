#!/usr/bin/env bash
#
# Installs RHOAI 3.5 and its prerequisites on the perf-test cluster.
# Service Mesh 3 is NOT required on OCP 4.21+.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/config"
MANIFESTS="${SCRIPT_DIR}/manifests"

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

# --- Helper: wait for a CSV to reach Succeeded ---
wait_for_csv() {
  local namespace="$1"
  local pattern="$2"
  local label="$3"
  local timeout="${4:-30}"

  echo "Waiting for ${label} CSV..."
  for i in $(seq 1 "${timeout}"); do
    if oc get csv -n "${namespace}" 2>/dev/null | grep -qE "${pattern}.*Succeeded"; then
      echo "✓ ${label} installed."
      return 0
    fi
    if [[ $i -eq ${timeout} ]]; then
      echo "ERROR: ${label} CSV did not reach Succeeded after $((timeout * 10))s."
      exit 1
    fi
    sleep 10
  done
}

# --- Step 1: GPU MachineSet (g4dn.xlarge worker for LlamaStack/RAG) ---
if oc get machineset -n openshift-machine-api 2>/dev/null | grep -q "gpu"; then
  echo "✓ GPU MachineSet already exists."
else
  echo "Creating GPU MachineSet (g4dn.xlarge)..."

  INFRA_NAME=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
  WORKER_MS=$(oc get machineset -n openshift-machine-api -o name | head -1)

  AMI_ID=$(oc get "${WORKER_MS}" -n openshift-machine-api -o jsonpath='{.spec.template.spec.providerSpec.value.ami.id}')
  AZ=$(oc get "${WORKER_MS}" -n openshift-machine-api -o jsonpath='{.spec.template.spec.providerSpec.value.placement.availabilityZone}')
  REGION=$(oc get "${WORKER_MS}" -n openshift-machine-api -o jsonpath='{.spec.template.spec.providerSpec.value.placement.region}')
  SUBNET_FILTER=$(oc get "${WORKER_MS}" -n openshift-machine-api -o jsonpath='{.spec.template.spec.providerSpec.value.subnet.filters[0].values[0]}')
  SG_FILTER=$(oc get "${WORKER_MS}" -n openshift-machine-api -o jsonpath='{.spec.template.spec.providerSpec.value.securityGroups[0].filters[0].values[0]}')
  IAM_PROFILE=$(oc get "${WORKER_MS}" -n openshift-machine-api -o jsonpath='{.spec.template.spec.providerSpec.value.iamInstanceProfile.id}')

  GPU_MS_NAME="${INFRA_NAME}-gpu-${AZ}"

  cat <<EOF | oc apply -f -
apiVersion: machine.openshift.io/v1beta1
kind: MachineSet
metadata:
  name: ${GPU_MS_NAME}
  namespace: openshift-machine-api
  labels:
    machine.openshift.io/cluster-api-cluster: ${INFRA_NAME}
spec:
  replicas: 1
  selector:
    matchLabels:
      machine.openshift.io/cluster-api-cluster: ${INFRA_NAME}
      machine.openshift.io/cluster-api-machineset: ${GPU_MS_NAME}
  template:
    metadata:
      labels:
        machine.openshift.io/cluster-api-cluster: ${INFRA_NAME}
        machine.openshift.io/cluster-api-machine-role: worker
        machine.openshift.io/cluster-api-machine-type: worker
        machine.openshift.io/cluster-api-machineset: ${GPU_MS_NAME}
    spec:
      metadata:
        labels:
          node-role.kubernetes.io/worker: ""
      providerSpec:
        value:
          apiVersion: machine.openshift.io/v1beta1
          kind: AWSMachineProviderConfig
          ami:
            id: ${AMI_ID}
          blockDevices:
          - ebs:
              encrypted: true
              iops: 0
              kmsKey:
                arn: ""
              volumeSize: 120
              volumeType: gp3
          credentialsSecret:
            name: aws-cloud-credentials
          deviceIndex: 0
          iamInstanceProfile:
            id: ${IAM_PROFILE}
          instanceType: g4dn.xlarge
          kind: AWSMachineProviderConfig
          placement:
            availabilityZone: ${AZ}
            region: ${REGION}
          securityGroups:
          - filters:
            - name: tag:Name
              values:
              - ${SG_FILTER}
          subnet:
            filters:
            - name: tag:Name
              values:
              - ${SUBNET_FILTER}
          tags:
          - name: kubernetes.io/cluster/${INFRA_NAME}
            value: owned
          userDataSecret:
            name: worker-user-data
EOF

  echo "Waiting for GPU node to become Ready..."
  for i in $(seq 1 60); do
    GPU_MACHINE=$(oc get machines -n openshift-machine-api -l "machine.openshift.io/cluster-api-machineset=${GPU_MS_NAME}" -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")
    GPU_NODE=$(oc get machines -n openshift-machine-api -l "machine.openshift.io/cluster-api-machineset=${GPU_MS_NAME}" -o jsonpath='{.items[0].status.nodeRef.name}' 2>/dev/null || echo "")
    if [[ -n "${GPU_NODE}" ]]; then
      NODE_READY=$(oc get node "${GPU_NODE}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
      if [[ "${NODE_READY}" == "True" ]]; then
        echo "✓ GPU node ${GPU_NODE} is Ready."
        break
      fi
    fi
    if [[ $i -eq 60 ]]; then
      echo "ERROR: GPU node did not become Ready after 10 minutes."
      echo "  Machine phase: ${GPU_MACHINE}"
      oc get machines -n openshift-machine-api -l "machine.openshift.io/cluster-api-machineset=${GPU_MS_NAME}"
      exit 1
    fi
    echo "  Waiting... machine phase=${GPU_MACHINE} (${i}/60)"
    sleep 10
  done
fi

# --- Step 2: NFD Operator (discovers GPU hardware) ---
if oc get csv -n openshift-nfd 2>/dev/null | grep -q "nfd.*Succeeded"; then
  echo "✓ NFD operator is already installed."
else
  echo "Installing NFD operator..."
  oc apply -f "${MANIFESTS}/nfd-operator.yaml"
  wait_for_csv "openshift-nfd" "nfd" "NFD operator" 30
fi

# --- Step 3: NVIDIA GPU Operator (installs drivers on GPU nodes) ---
if oc get csv -n nvidia-gpu-operator 2>/dev/null | grep -q "gpu-operator-certified.*Succeeded"; then
  echo "✓ NVIDIA GPU operator is already installed."
else
  echo "Installing NVIDIA GPU operator..."
  oc apply -f "${MANIFESTS}/nvidia-gpu-operator.yaml"
  wait_for_csv "nvidia-gpu-operator" "gpu-operator-certified" "NVIDIA GPU operator" 60
fi

# --- Step 4: cert-manager operator (required by KServe) ---
if oc get csv -n cert-manager-operator 2>/dev/null | grep -q "cert-manager-operator.*Succeeded"; then
  echo "✓ cert-manager operator is already installed."
else
  echo "Installing cert-manager operator..."
  oc apply -f "${MANIFESTS}/cert-manager-operator.yaml"
  wait_for_csv "cert-manager-operator" "cert-manager-operator" "cert-manager operator" 30
fi

# --- Step 5: JobSet operator (required by RHOAI 3.x) ---
if oc get csv -n openshift-jobset-system 2>/dev/null | grep -qE "(jobset-operator|job-set).*Succeeded"; then
  echo "✓ JobSet operator is already installed."
else
  echo "Installing JobSet operator..."
  oc apply -f "${MANIFESTS}/jobset-operator.yaml"
  wait_for_csv "openshift-jobset-system" "(jobset-operator|job-set)" "JobSet operator" 30
fi

# --- Step 6: RHOAI operator ---
if oc get csv -n redhat-ods-operator 2>/dev/null | grep -q "rhods-operator.*Succeeded"; then
  echo "✓ RHOAI operator is already installed."
else
  echo "Installing RHOAI operator..."
  oc apply -f "${MANIFESTS}/rhoai-operator.yaml"
  wait_for_csv "redhat-ods-operator" "rhods-operator" "RHOAI operator" 60
fi

# --- Step 6b: Override operator image if RHODS_OPERATOR_IMAGE is set ---
if [[ -n "${RHODS_OPERATOR_IMAGE:-}" ]]; then
  echo "Overriding RHOAI operator image: ${RHODS_OPERATOR_IMAGE}"

  # Delete the Subscription so OLM won't reconcile back to the catalog image.
  oc delete subscription rhods-operator -n redhat-ods-operator --ignore-not-found
  echo "  Deleted Subscription (prevents OLM from reverting the image)."

  # Patch the CSV — OLM uses this as the source of truth for the Deployment.
  CSV_NAME=$(oc get csv -n redhat-ods-operator -o name | grep rhods-operator)
  oc patch "${CSV_NAME}" -n redhat-ods-operator --type=json -p \
    "[{\"op\": \"replace\", \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/0/image\", \"value\": \"${RHODS_OPERATOR_IMAGE}\"}]"
  echo "  Patched CSV image."

  # Wait until the running pod has the correct image.
  echo "  Waiting for operator pod to run with the new image..."
  for i in $(seq 1 30); do
    POD_IMAGE=$(oc get pods -n redhat-ods-operator -l name=rhods-operator \
      -o jsonpath='{.items[0].spec.containers[0].image}' 2>/dev/null || echo "")
    if [[ "${POD_IMAGE}" == "${RHODS_OPERATOR_IMAGE}" ]]; then
      POD_READY=$(oc get pods -n redhat-ods-operator -l name=rhods-operator \
        -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
      if [[ "${POD_READY}" == "True" ]]; then
        echo "✓ Operator running with custom image: ${POD_IMAGE}"
        break
      fi
    fi
    if [[ $i -eq 30 ]]; then
      echo "ERROR: Operator pod did not start with image ${RHODS_OPERATOR_IMAGE} after 5 minutes."
      echo "  Current pod image: ${POD_IMAGE}"
      oc get pods -n redhat-ods-operator -o wide
      exit 1
    fi
    sleep 10
  done

  # Wait for kube-apiserver watch caches to expire and GC to reclaim memory.
  COOLDOWN_MINUTES="${OPERATOR_PATCH_COOLDOWN_MINUTES:-15}"
  echo "Waiting ${COOLDOWN_MINUTES} minutes for apiserver watch cache to expire..."
  sleep "$((COOLDOWN_MINUTES * 60))"
  echo "✓ Cooldown complete."
fi

# --- Step 7: DSCI (DSCInitialization) ---
# The RHOAI operator auto-creates a default DSCI; applying a second one is
# rejected by the admission webhook.  If one exists, patch it to match our
# desired config from dsci.yaml.
TRUSTED_CA_STATE=$(python3 -c "import yaml; print(yaml.safe_load(open('${MANIFESTS}/dsci.yaml'))['spec']['trustedCABundle']['managementState'])")
echo "trustedCABundle managementState from manifest: ${TRUSTED_CA_STATE}"

if oc get dsci default-dsci -o jsonpath='{.status.phase}' 2>/dev/null | grep -q "Ready"; then
  echo "DSCInitialization already exists — patching to match desired config..."
  oc patch dsci default-dsci --type=merge \
    -p "{\"spec\":{\"trustedCABundle\":{\"managementState\":\"${TRUSTED_CA_STATE}\",\"customCABundle\":\"\"}}}"
  echo "✓ DSCI patched (trustedCABundle: ${TRUSTED_CA_STATE})."
else
  echo "Applying DSCInitialization..."
  oc apply -f "${MANIFESTS}/dsci.yaml"
  echo "✓ DSCI applied."
fi

# --- Step 8: DSC (DataScienceCluster) ---
echo "Applying DataScienceCluster..."

# Delete existing DSC if present (may have different component config)
oc delete datasciencecluster default-dsc --ignore-not-found 2>/dev/null

oc apply -f "${MANIFESTS}/dsc.yaml"

echo "Waiting for DataScienceCluster to become Ready..."
for i in $(seq 1 60); do
  PHASE=$(oc get datasciencecluster default-dsc -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
  if [[ "${PHASE}" == "Ready" ]]; then
    echo "✓ DataScienceCluster is Ready."
    break
  fi
  if [[ $i -eq 60 ]]; then
    echo "ERROR: DataScienceCluster did not become Ready after 10 minutes."
    echo "  Current phase: ${PHASE}"
    oc get datasciencecluster default-dsc -o yaml | tail -30
    exit 1
  fi
  echo "  Waiting... phase=${PHASE} (${i}/60)"
  sleep 10
done

echo ""
echo "=== RHOAI Installation Complete ==="
echo ""
echo "Installed operators:"
oc get csv -A -o custom-columns='NAME:.metadata.name,PHASE:.status.phase' --no-headers | grep -E 'rhods|jobset|cert-manager|nfd|gpu-operator' | sort -u
echo ""
echo "Pods in redhat-ods-applications:"
oc get pods -n redhat-ods-applications
echo ""
echo "Pods in redhat-ods-operator:"
oc get pods -n redhat-ods-operator
echo ""
echo "GPU node:"
oc get nodes -l node-role.kubernetes.io/worker -o wide | grep -i gpu || oc get machines -n openshift-machine-api | grep gpu
echo ""
echo "Pods in openshift-nfd:"
oc get pods -n openshift-nfd
echo ""
echo "Pods in nvidia-gpu-operator:"
oc get pods -n nvidia-gpu-operator
