INFRA_DIR := $(realpath $(dir $(lastword $(MAKEFILE_LIST)))/../setup/infra)

.PHONY: iam-setup
## Create an IAM user with static access keys for openshift-install (one-time setup per AWS account)
iam-setup:
	$(INFRA_DIR)/setup-iam.sh

.PHONY: iam-cleanup
## Delete the IAM user and access keys created by iam-setup
iam-cleanup:
	$(INFRA_DIR)/cleanup-iam.sh

.PHONY: cluster-create
## Provision a perf-test OCP cluster on AWS via openshift-install (see setup/infra/config.example)
cluster-create:
	$(INFRA_DIR)/create-cluster.sh

.PHONY: cluster-destroy
## Tear down the perf-test OCP cluster and clean up AWS resources
cluster-destroy:
	$(INFRA_DIR)/destroy-cluster.sh

.PHONY: cluster-reset
## Remove local cluster state after a failed install (no AWS resources to clean up)
cluster-reset:
	$(INFRA_DIR)/reset-cluster.sh

.PHONY: servicemesh-install
## Install Service Mesh 3 operator on the perf-test cluster (optional — not required on OCP 4.21+)
servicemesh-install:
	$(INFRA_DIR)/install-servicemesh.sh

.PHONY: rhoai-install
## Install RHOAI on the perf-test cluster
rhoai-install:
	$(INFRA_DIR)/install-rhoai.sh

.PHONY: perf-test
## Run the staged performance tests on the perf-test cluster
perf-test:
	$(INFRA_DIR)/run-tests.sh
