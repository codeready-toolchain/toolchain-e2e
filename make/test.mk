###########################################################
#
# End-to-end Tests
#
###########################################################

QUAY_NAMESPACE ?= codeready-toolchain-test
DATE_SUFFIX := $(shell date +'%d%H%M%S')
HOST_NS ?= toolchain-host-${DATE_SUFFIX}
MEMBER_NS ?= toolchain-member-${DATE_SUFFIX}

SECOND_MEMBER_MODE = true

ifeq ($(SECOND_MEMBER_MODE),true)
MEMBER_NS_2 = toolchain-member2-${DATE_SUFFIX}
endif

REGISTRATION_SERVICE_NS := $(HOST_NS)

ENVIRONMENT := e2e-tests
IMAGE_NAMES_DIR := /tmp/crt-e2e-image-names


ifneq ($(CLONEREFS_OPTIONS),)
PUBLISH_OPERATOR := false
else
PUBLISH_OPERATOR := true
endif

.PHONY: test-e2e
## Run the e2e tests
test-e2e: deploy-e2e e2e-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: deploy-e2e
deploy-e2e: INSTALL_OPERATOR=true
deploy-e2e: build clean-e2e-files deploy-host deploy-members e2e-service-account setup-toolchainclusters

.PHONY: test-e2e-local
## Run the e2e tests with the local 'host', 'member', and 'registration-service' repositories
test-e2e-local:
	$(MAKE) test-e2e HOST_REPO_PATH=${PWD}/../host-operator MEMBER_REPO_PATH=${PWD}/../member-operator REG_REPO_PATH=${PWD}/../registration-service

.PHONY: deploy-e2e-local
## Deploy the e2e environment with the local 'host', 'member', and 'registration-service' repositories
deploy-e2e-local:
	$(MAKE) deploy-e2e HOST_REPO_PATH=${PWD}/../host-operator MEMBER_REPO_PATH=${PWD}/../member-operator REG_REPO_PATH=${PWD}/../registration-service

.PHONY: test-e2e-member-local
## Run the e2e tests with the local 'member' repository only
test-e2e-member-local:
	$(MAKE) test-e2e MEMBER_REPO_PATH=${PWD}/../member-operator

.PHONY: test-e2e-host-local
## Run the e2e tests with the local 'host' repository only
test-e2e-host-local:
	$(MAKE) test-e2e HOST_REPO_PATH=${PWD}/../host-operator

.PHONY: test-e2e-registration-local
## Run the e2e tests with the local 'registration' repository only
test-e2e-registration-local:
	$(MAKE) test-e2e REG_REPO_PATH=${PWD}/../registration-service

.PHONY: e2e-run
e2e-run:
	oc get toolchaincluster -n $(HOST_NS)
	oc get toolchaincluster -n $(MEMBER_NS)
	MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} go test ./test/e2e -v -timeout=90m -failfast || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} && exit 1)

.PHONY: print-logs
print-logs:
ifneq ($(OPENSHIFT_BUILD_NAMESPACE),)
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=host-operator-controller-manager NAMESPACE=${HOST_NS} ADDITIONAL_PARAMS="-c manager"
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=member-operator-controller-manager NAMESPACE=${MEMBER_NS} ADDITIONAL_PARAMS="-c manager"
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=member-operator-webhook NAMESPACE=${MEMBER_NS}
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then $(MAKE) print-operator-logs DEPLOYMENT_NAME=member-operator-controller-manager NAMESPACE=${MEMBER_NS_2} ADDITIONAL_PARAMS="-c manager"; fi
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=registration-service NAMESPACE=${REGISTRATION_SERVICE_NS}
else
	@echo "you can print logs using the commands:"
	@echo "oc logs deployment.apps/host-operator-controller-manager -c manager --namespace ${HOST_NS}"
	@echo "oc logs deployment.apps/member-operator-controller-manager -c manager --namespace ${MEMBER_NS}"
	@if [[ ${SECOND_MEMBER_MODE} == true ]]; then echo "oc logs deployment.apps/member-operator-controller-manager -c manager --namespace ${MEMBER_NS_2}"; fi
	@echo "oc logs deployment.apps/member-operator-webhook --namespace ${MEMBER_NS}"
	@echo "oc logs deployment.apps/registration-service --namespace ${REGISTRATION_SERVICE_NS}"
endif

.PHONY: print-operator-logs
print-operator-logs:
	@echo "==============================================================================================================="
	@echo "========================== ${DEPLOYMENT_NAME} deployment YAML- Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc get deployment.apps/${DEPLOYMENT_NAME} --namespace ${NAMESPACE} -o yaml
	@echo "==============================================================================================================="
	@echo "========================== ${DEPLOYMENT_NAME} pod YAML- Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc get pods -l control-plane=controller-manager --namespace ${NAMESPACE} -o yaml
	@echo "==============================================================================================================="
	@echo "========================== ${DEPLOYMENT_NAME} deployment logs - Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc logs deployment.apps/${DEPLOYMENT_NAME} ${ADDITIONAL_PARAMS} --namespace ${NAMESPACE}
	@echo "==============================================================================================================="

.PHONY: setup-toolchainclusters
setup-toolchainclusters:
	echo ${MEMBER_NS_2}
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -mn $(MEMBER_NS)   -hn $(HOST_NS) -s
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -mn $(MEMBER_NS_2) -hn $(HOST_NS) -s -mm 2; fi
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host   -mn $(MEMBER_NS)   -hn $(HOST_NS) -s
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host   -mn $(MEMBER_NS_2) -hn $(HOST_NS) -s -mm 2; fi

.PHONY: e2e-service-account
e2e-service-account:
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -tn e2e -mn $(MEMBER_NS) -hn $(HOST_NS) -s
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host -tn e2e -mn $(MEMBER_NS) -hn $(HOST_NS) -s
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -tn e2e -mn $(MEMBER_NS_2) -hn $(HOST_NS) -s -mm 2; fi

###########################################################
#
# Fetching and building Member and Host Operators
#
###########################################################

.PHONY: build-with-operators
build-with-operators:
	@echo "ignore and just create empty files to make openshift-ci happy"
	mkdir -p ${E2E_REPO_PATH}/build/_output/bin/
	mkdir -p build/_output/bin/
	touch ${E2E_REPO_PATH}/build/_output/bin/host-operator
	touch ${E2E_REPO_PATH}/build/_output/bin/member-operator
	touch ${E2E_REPO_PATH}/build/_output/bin/member-operator-webhook
	touch ${E2E_REPO_PATH}/build/_output/bin/registration-service
	cp ${E2E_REPO_PATH}/build/_output/bin/* build/_output/bin/

.PHONY: publish-current-bundles-for-e2e
## Target that is supposed to be called from CI - it builds & publishes the current operator bundles
publish-current-bundles-for-e2e: get-and-publish-operators

.PHONY: get-and-publish-operators
get-and-publish-operators: PUBLISH_OPERATOR=true
get-and-publish-operators: INSTALL_OPERATOR=false
get-and-publish-operators: clean-e2e-files get-and-publish-host-operator get-and-publish-member-operator

.PHONY: get-and-publish-member-operator
get-and-publish-member-operator:
	PUBLISH_OPERATOR=${PUBLISH_OPERATOR} INSTALL_OPERATOR=${INSTALL_OPERATOR} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} MEMBER_REPO_PATH=${MEMBER_REPO_PATH} ENVIRONMENT=${ENVIRONMENT} DATE_SUFFIX=${DATE_SUFFIX} QUAY_NAMESPACE=${QUAY_NAMESPACE} ./scripts/manage-member-operator.sh

.PHONY: get-and-publish-host-operator
get-and-publish-host-operator:
	PUBLISH_OPERATOR=${PUBLISH_OPERATOR} INSTALL_OPERATOR=${INSTALL_OPERATOR} HOST_NS=${HOST_NS} HOST_REPO_PATH=${HOST_REPO_PATH} REG_REPO_PATH=${REG_REPO_PATH} ENVIRONMENT=${ENVIRONMENT} DATE_SUFFIX=${DATE_SUFFIX} QUAY_NAMESPACE=${QUAY_NAMESPACE} ./scripts/manage-host-operator.sh

###########################################################
#
# Deploying Member and Host Operators in Openshift CI Environment
#
###########################################################

.PHONY: deploy-members
deploy-members: create-member1 create-member2 get-and-publish-member-operator

.PHONY: create-member1
create-member1:
	@echo "Deploying member operator to $(MEMBER_NS)..."
	$(MAKE) create-project PROJECT_NAME=${MEMBER_NS}
	-oc label ns ${MEMBER_NS} app=member-operator

.PHONY: create-member2
create-member2:
ifeq ($(MEMBER_REPO_PATH),)
	$(eval MEMBER_REPO_PATH = /tmp/codeready-toolchain/member-operator)
endif
ifeq ($(SECOND_MEMBER_MODE),true)
	@echo "Deploying second member operator to ${MEMBER_NS_2}..."
	$(MAKE) create-project PROJECT_NAME=${MEMBER_NS_2}
	-oc label ns ${MEMBER_NS_2} app=member-operator
	oc apply -f ${MEMBER_REPO_PATH}/config/crd/bases/toolchain.dev.openshift.com_memberoperatorconfigs.yaml
	oc apply -f deploy/member2-operator/${ENVIRONMENT}/ -n ${MEMBER_NS_2}
endif

.PHONY: deploy-host
deploy-host: create-host-project get-and-publish-host-operator create-host-resources

.PHONY: create-host-project
create-host-project:
	@echo "Deploying host operator to ${HOST_NS}..."
	$(MAKE) create-project PROJECT_NAME=${HOST_NS}
	-oc label ns ${MEMBER_NS_2} app=host-operator

.PHONY: create-host-resources
create-host-resources:
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/codeready-toolchain/host-operator)
endif
	oc apply -f ${HOST_REPO_PATH}/config/crd/bases/toolchain.dev.openshift.com_toolchainconfigs.yaml
	oc apply -f deploy/host-operator/${ENVIRONMENT}/ -n ${HOST_NS}
	# patch toolchainconfig to prevent webhook deploy for 2nd member, a 2nd webhook deploy causes the webhook verification in e2e tests to fail
	# since e2e environment has 2 member operators running in the same cluster
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then \
		API_ENDPOINT=`oc get infrastructure cluster -o jsonpath='{.status.apiServerURL}'`; \
		TOOLCHAIN_CLUSTER_NAME=`echo "$${API_ENDPOINT}" | sed 's/.*api\.\([^:]*\):.*/\1/'`; \
		echo "API_ENDPOINT $${API_ENDPOINT}"; \
		echo "TOOLCHAIN_CLUSTER_NAME $${TOOLCHAIN_CLUSTER_NAME}"; \
		PATCH_FILE=/tmp/patch-toolchainconfig_${DATE_SUFFIX}.json; \
		echo "{\"spec\":{\"members\":{\"specificPerMemberCluster\":{\"member-$${TOOLCHAIN_CLUSTER_NAME}2\":{\"webhook\":{\"deploy\":false}}}}}}" > $$PATCH_FILE; \
		oc patch toolchainconfig config -n $(HOST_NS) --type=merge --patch "$$(cat $$PATCH_FILE)"; \
	fi;

.PHONY: create-project
create-project:
	-oc new-project ${PROJECT_NAME} 1>/dev/null
	-oc project ${PROJECT_NAME}
	-oc label ns ${PROJECT_NAME} toolchain.dev.openshift.com/provider=codeready-toolchain

.PHONY: display-eval
display-eval:
	@echo 'export HOST_NS=$(shell oc get projects -l app=host-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)'
	@echo 'export MEMBER_NS=$(shell oc get projects -l app=member-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)'
	@echo 'export REGISTRATION_SERVICE_NS=$$HOST_NS'
	@echo '# Run this command to configure your shell:'
	@echo '# eval $$(make display-eval)'
