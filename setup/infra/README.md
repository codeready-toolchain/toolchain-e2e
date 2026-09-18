# Sandbox Performance Test Cluster

Wrapper scripts for provisioning disposable OCP clusters on AWS using
`openshift-install` IPI, for Dev Sandbox performance testing.

## Prerequisites

- `openshift-install` and `oc` CLI for your target OCP version
- AWS CLI configured and authenticated (`aws sts get-caller-identity` should succeed)
- A Route53 hosted zone in the target AWS account (`aws route53 list-hosted-zones`)

## Quick start

All `make` targets run from the **project root** (not `setup/infra/`).

```bash
# 1. Configure
cp setup/infra/config.example setup/infra/config
# Edit setup/infra/config — set CLUSTER_NAME and BASE_DOMAIN at minimum

# 2. Create IAM user with static access keys (one-time per AWS account, skip if you already have one)
make iam-setup

# 3. Provision the cluster (~40 minutes)
make cluster-create

# 4. Log in (the perf tool requires token-based auth, not certificate)
export KUBECONFIG=setup/infra/clusters/<your-cluster-name>/auth/kubeconfig
oc login -u kubeadmin -p "$(cat setup/infra/clusters/<your-cluster-name>/auth/kubeadmin-password)"
```

### Why an IAM user?

`openshift-install` requires static AWS access keys (IAM user with programmatic
access). Temporary credentials from SSO/STS are not supported for cluster
provisioning.

`setup-iam.sh` creates an IAM user named `openshift-installer` with
`AdministratorAccess` and writes the access keys to `aws-credentials` (gitignored).
The cluster scripts source this file automatically.

## Running performance tests

See [setup/README.adoc](../README.adoc) for the full guide.

Must run on a **fresh cluster** — reusing clusters inflates memory metrics
(etcd, apiserver caches don't fully drain between user provisioning cycles).

1. Provision fresh OCP cluster (`make cluster-create`)
2. Install RHOAI (`make rhoai-install`) — provisions GPU node, installs NFD, NVIDIA GPU, cert-manager, JobSet, and RHOAI operators, then applies DSCI and DSC
3. Install Sandbox operators (`make dev-deploy-latest`)
4. Run staged performance tests (`make perf-test`)

RHOAI operator metrics are captured via `--workloads` (see `run-tests.sh`).

**macOS note:** The 2000-user test opens many concurrent connections. If you
hit TCP port exhaustion errors, raise the ephemeral port range:

```bash
sudo sysctl -w net.inet.ip.portrange.first=32768
```

### Full reproducible workflow

```bash
# Set KUBECONFIG first (use your CLUSTER_NAME from setup/infra/config):
export KUBECONFIG=setup/infra/clusters/<your-cluster-name>/auth/kubeconfig

# Install RHOAI before Sandbox operators so their workloads
# are present during both baseline and 2000-user measurements.
make rhoai-install

# Install Sandbox operators
make dev-deploy-latest

# Run the 5-stage performance test
make perf-test
```

Results CSVs are written to `tmp/results/`.

## Teardown

```bash
make cluster-destroy        # Tear down AWS resources (~10 minutes)
make iam-cleanup            # Delete the IAM user (when done with all testing)
```

Do NOT delete the `clusters/<name>/` directory manually — the destroy script
needs `metadata.json` to clean up AWS resources.

## Make targets

| Target | Description |
|--------|-------------|
| `make iam-setup` | Create IAM user with static access keys (one-time) |
| `make iam-cleanup` | Delete IAM user and credentials |
| `make cluster-create` | Provision the OCP cluster |
| `make cluster-destroy` | Tear down the OCP cluster |
| `make servicemesh-install` | Install Service Mesh 3 operator (optional — not required on OCP 4.21+) |
| `make rhoai-install` | Install RHOAI and prerequisites |
| `make perf-test` | Run staged performance tests |

## Notes

- **Route53**: `openshift-install` creates DNS records under `api.<cluster>.<basedomain>`
  and `*.apps.<cluster>.<basedomain>`. Set `BASE_DOMAIN` in your config to match a
  hosted zone in your AWS account.
- **Cluster state**: the `clusters/` directory (gitignored) holds kubeconfig, admin
  password, and metadata. Each developer manages their own local state.
- **Credentials**: `aws-credentials` and `config` are gitignored. Never commit AWS keys.
