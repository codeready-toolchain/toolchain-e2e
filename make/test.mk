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
MEMBER_NS_2 ?= toolchain-member2-${DATE_SUFFIX}
endif

REGISTRATION_SERVICE_NS := $(HOST_NS)

ENVIRONMENT := e2e-tests
IMAGE_NAMES_DIR := /tmp/crt-e2e-image-names

DEPLOY_LATEST := false

ifneq ($(CLONEREFS_OPTIONS),)
PUBLISH_OPERATOR := false
else
PUBLISH_OPERATOR := true
endif

E2E_TEST_EXECUTION ?= true

ifeq ($(IS_OSD),true)
LETS_ENCRYPT_PARAM := --lets-encrypt
endif

E2E_PARALLELISM=1

TESTS_RUN_FILTER_REGEXP ?= ""

REPORT_PORTAL_DIR := rp_preproc/results

.PHONY: test-e2e
## Run the e2e tests
test-e2e: INSTALL_OPERATOR=true
test-e2e: prepare-e2e verify-migration-and-deploy-e2e e2e-run-parallel e2e-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: test-e2e-without-migration
## Run the e2e tests without migration tests
test-e2e-without-migration: prepare-e2e deploy-e2e e2e-run-parallel e2e-run
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: test-e2e-sequential-only
## Run the e2e tests without migration and without parallel tests
test-e2e-sequential-only: prepare-e2e deploy-e2e e2e-run
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: prepare-and-deploy-e2e
## Prepare and Deploy e2e environment. Usefull to reset without having to run a test
prepare-and-deploy-e2e: prepare-e2e deploy-e2e
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: verify-migration-and-deploy-e2e
verify-migration-and-deploy-e2e: prepare-projects e2e-deploy-latest e2e-migration-setup get-publish-and-install-operators e2e-migration-verify

.PHONY: e2e-migration-setup
e2e-migration-setup:
	@echo "Setting up the environment before testing the operator migration..."
	$(MAKE) execute-tests MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} TESTS_TO_EXECUTE="./test/migration/setup" REPORT_NAME="xunit_e2e_migration_setup.xml"
	@echo "Environment successfully setup."

.PHONY: e2e-migration-verify
e2e-migration-verify:
	@echo "Updating operators and verifying resources..."
	$(MAKE) execute-tests MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} TESTS_TO_EXECUTE="./test/migration/verify" REPORT_NAME="xunit_e2e_migration_verify.xml"
	@echo "Migration tests successfully finished"

.PHONY: e2e-deploy-latest
e2e-deploy-latest:
	$(MAKE) get-publish-install-and-register-operators MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} ENVIRONMENT=${ENVIRONMENT} INSTALL_OPERATOR=${INSTALL_OPERATOR} DEPLOY_LATEST=true LETS_ENCRYPT_PARAM=${LETS_ENCRYPT_PARAM}

.PHONY: prepare-e2e
prepare-e2e: build clean-e2e-files

.PHONY: deploy-published-operators-e2e
## Deploy operators that were already published
deploy-published-operators-e2e: PUBLISH_OPERATOR=false
deploy-published-operators-e2e: clean-e2e-files deploy-e2e

.PHONY: deploy-e2e
deploy-e2e: INSTALL_OPERATOR=true
deploy-e2e: prepare-projects get-publish-install-and-register-operators 
	@echo "Operators are successfuly deployed using the ${ENVIRONMENT} environment."
	@echo ""

label-olm-ns:
# adds a label on the oc label ns/openshift-operator-lifecycle-manager name=openshift-operator-lifecycle-manager
# so that deployment also works when network policies were configured with `sandbox-cli`
	@-oc label --overwrite=true ns/openshift-operator-lifecycle-manager name=openshift-operator-lifecycle-manager

.PHONY: test-e2e-local-without-migration
## Run the e2e tests with the local 'host', 'member', and 'registration-service' repositories but without migration tests
test-e2e-local-without-migration:
	$(MAKE) test-e2e-without-migration HOST_REPO_PATH=${PWD}/../host-operator MEMBER_REPO_PATH=${PWD}/../member-operator REG_REPO_PATH=${PWD}/../registration-service

.PHONY: test-e2e-local-sequential-only
## Run the e2e tests with the local 'host', 'member', and 'registration-service' repositories but without migration and without parallel tests
test-e2e-local-sequential-only:
	$(MAKE) test-e2e-sequential-only HOST_REPO_PATH=${PWD}/../host-operator MEMBER_REPO_PATH=${PWD}/../member-operator REG_REPO_PATH=${PWD}/../registration-service

.PHONY: prepare-and-deploy-e2e-local
## Prepare and Depoy the e2e tests with the local 'host', 'member', and 'registration-service' repositories but without running any test
prepare-and-deploy-e2e-local:
	$(MAKE) prepare-and-deploy-e2e HOST_REPO_PATH=${PWD}/../host-operator MEMBER_REPO_PATH=${PWD}/../member-operator REG_REPO_PATH=${PWD}/../registration-service

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

.PHONY: e2e-run-parallel
e2e-run-parallel:
	@echo "Running e2e tests in parallel..."
	$(MAKE) execute-tests MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} TESTS_TO_EXECUTE="./test/e2e/parallel" E2E_PARALLELISM=100 REPORT_NAME="xunit_e2e_parallel.xml"
	@echo "The parallel e2e tests successfully finished"

.PHONY: e2e-run
e2e-run:
	@echo "Running e2e tests..."
	$(MAKE) execute-tests MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} TESTS_TO_EXECUTE="./test/e2e ./test/metrics"  REPORT_NAME="xunit_e2e.xml"
	@echo "The e2e tests successfully finished"

.PHONY: execute-tests
execute-tests:
	@echo "Present Spaces"
	-oc get Space -n ${HOST_NS}
	@echo "Status of ToolchainStatus"
	-oc get ToolchainStatus -n ${HOST_NS} -o yaml
	@echo "Check Reporting"
	$(MAKE) check-go-junit-report
	@echo "Starting test $(shell date)"
	MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} go test ${TESTS_TO_EXECUTE} -run ${TESTS_RUN_FILTER_REGEXP} -p 1 -parallel ${E2E_PARALLELISM} -v -timeout=90m -failfast 2>&1 | $(MAKE) generate-report REPORT_NAME=${REPORT_NAME}  || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} && exit 1)

.PHONY: print-logs
print-logs:
	@echo "Time: $(shell date)"
ifneq ($(OPENSHIFT_BUILD_NAMESPACE),)
	echo "artifact directory: ${ARTIFACT_DIR}"
	-oc adm must-gather --dest-dir=${ARTIFACT_DIR}
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=host-operator-controller-manager NAMESPACE=${HOST_NS} ADDITIONAL_PARAMS="-c manager"
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=member-operator-controller-manager NAMESPACE=${MEMBER_NS} ADDITIONAL_PARAMS="-c manager"
	$(MAKE) print-operator-logs DEPLOYMENT_NAME=member-operator-webhook NAMESPACE=${MEMBER_NS}
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then $(MAKE) print-operator-logs DEPLOYMENT_NAME=member-operator-controller-manager NAMESPACE=${MEMBER_NS_2} ADDITIONAL_PARAMS="-c manager"; fi
	$(MAKE) print-deployment-logs DEPLOYMENT_NAME=registration-service DEPLOYMENT_LABELS="-l name=registration-service" NAMESPACE=${REGISTRATION_SERVICE_NS}
else
	$(MAKE) print-local-debug-info  HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS}
endif

.PHONY: check-go-junit-report

check-go-junit-report:
	@command -v go-junit-report >/dev/null 2>&1 || { echo "go-junit-report is not installed. Installing..."; go install github.com/jstemmer/go-junit-report/v2@latest; }
	@echo "go-junit-report version:" && go-junit-report -version
	go env

.PHONY: generate-report
generate-report:
	@echo "Generating report"
ifneq ($(OPENSHIFT_BUILD_NAMESPACE),) 
	mkdir -p  ${ARTIFACT_DIR}/${REPORT_PORTAL_DIR}
	go-junit-report > ${ARTIFACT_DIR}/${REPORT_PORTAL_DIR}/${REPORT_NAME}
	@echo "xUnit Report Generation Successful"
else 
	@echo "Skipping Report as its a local run"
endif

.PHONY: deploy-e2e-local-and-print-local-debug
deploy-e2e-local-and-print-local-debug: deploy-e2e-local print-local-debug-info

.PHONY: deploy-e2e-and-print-local-debug
deploy-e2e-and-print-local-debug: deploy-e2e print-local-debug-info

.PHONY: print-local-debug-info
print-local-debug-info:
	@echo "You can print logs using the commands:"
	@echo "oc logs deployment.apps/host-operator-controller-manager -c manager --namespace ${HOST_NS}"
	@echo "oc logs deployment.apps/member-operator-controller-manager -c manager --namespace ${MEMBER_NS}"
	@if [[ ${SECOND_MEMBER_MODE} == true ]]; then echo "oc logs deployment.apps/member-operator-controller-manager -c manager --namespace ${MEMBER_NS_2}"; fi
	@echo "oc logs deployment.apps/member-operator-webhook --namespace ${MEMBER_NS}"
	@echo "oc logs -l name=registration-service --namespace ${REGISTRATION_SERVICE_NS} --all-containers=true --prefix=true --tail=-1"
	@echo ""
	@echo "Add the following lines at the very beginning of the test/suite that you want to run/debug from your IDE:"
	@echo 'os.Setenv("KUBECONFIG","$(or ${KUBECONFIG}, ${HOME}/.kube/config)")'
	@echo 'os.Setenv("MEMBER_NS","${MEMBER_NS}")'
	@echo 'os.Setenv("MEMBER_NS_2","${MEMBER_NS_2}")'
	@echo 'os.Setenv("HOST_NS","${HOST_NS}")'
	@echo 'os.Setenv("REGISTRATION_SERVICE_NS","${REGISTRATION_SERVICE_NS}")'

.PHONY: print-deployment-logs
print-deployment-logs:
	@echo "==============================================================================================================="
	@echo "=========================== ${DEPLOYMENT_NAME} pod YAML - Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc get pods ${DEPLOYMENT_LABELS} --namespace ${NAMESPACE} -o yaml
	@echo ""
	@echo ""
	@echo "==============================================================================================================="
	@echo "======================= ${DEPLOYMENT_NAME} deployment logs - Namespace: ${NAMESPACE} =========================="
	@echo "==============================================================================================================="
	-oc logs ${DEPLOYMENT_LABELS} --namespace ${NAMESPACE} --all-containers=true --prefix=true --tail=-1 > ${ARTIFACT_DIR}/${DEPLOYMENT_NAME}.log
	@echo "==============================================================================================================="
	@echo ""
	@echo ""

.PHONY: print-operator-logs
print-operator-logs:
	@echo "==============================================================================================================="
	@echo "============================== CatalogSources  - Namespace: ${NAMESPACE} ======================================"
	@echo "==============================================================================================================="
	-oc get catalogsources --namespace ${NAMESPACE} -o yaml
	@echo ""
	@echo ""
	@echo "==============================================================================================================="
	@echo "============================== Subscriptions  - Namespace: ${NAMESPACE} ======================================="
	@echo "==============================================================================================================="
	-oc get subscriptions --namespace ${NAMESPACE} -o yaml
	@echo ""
	@echo ""
	@echo "==============================================================================================================="
	@echo "============================== InstallPlans  - Namespace: ${NAMESPACE} ========================================"
	@echo "==============================================================================================================="
	-oc get installplans --namespace ${NAMESPACE} -o yaml
	@echo ""
	@echo ""
	@echo "==============================================================================================================="
	@echo "======================= ${DEPLOYMENT_NAME} deployment YAML - Namespace: ${NAMESPACE} =========================="
	@echo "==============================================================================================================="
	-oc get deployment.apps/${DEPLOYMENT_NAME} --namespace ${NAMESPACE} -o yaml
	@echo ""
	@echo ""
	@echo "==============================================================================================================="
	@echo "=========================== ${DEPLOYMENT_NAME} pod YAML - Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc get pods -l control-plane=controller-manager --namespace ${NAMESPACE} -o yaml
	@echo ""
	@echo ""
	@echo "==============================================================================================================="
	@echo "======================= ${DEPLOYMENT_NAME} deployment logs - Namespace: ${NAMESPACE} =========================="
	@echo "==============================================================================================================="
	-oc logs deployment.apps/${DEPLOYMENT_NAME} ${ADDITIONAL_PARAMS} --namespace ${NAMESPACE} > ${ARTIFACT_DIR}/${DEPLOYMENT_NAME}_${NAMESPACE}.log
	@echo "==============================================================================================================="
	@echo ""
	@echo ""

.PHONY: setup-toolchainclusters
setup-toolchainclusters:
	$(MAKE) run-cicd-script SCRIPT_PATH=scripts/add-cluster.sh  SCRIPT_PARAMS="-t member -mn $(MEMBER_NS) -hn $(HOST_NS) ${LETS_ENCRYPT_PARAM}"
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then $(MAKE) run-cicd-script SCRIPT_PATH=scripts/add-cluster.sh  SCRIPT_PARAMS="-t member -mn $(MEMBER_NS_2) -hn $(HOST_NS) -mm 2 ${LETS_ENCRYPT_PARAM}"; fi
	$(MAKE) run-cicd-script SCRIPT_PATH=scripts/add-cluster.sh  SCRIPT_PARAMS="-t host   -mn $(MEMBER_NS) -hn $(HOST_NS) ${LETS_ENCRYPT_PARAM}"
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then $(MAKE) run-cicd-script SCRIPT_PATH=scripts/add-cluster.sh  SCRIPT_PARAMS="-t host   -mn $(MEMBER_NS_2) -hn $(HOST_NS) -mm 2 ${LETS_ENCRYPT_PARAM}"; fi
	echo "Restart host operator pods so it can get the ToolchainCluster CRs while it's starting up".
	oc delete pods --namespace ${HOST_NS} -l control-plane=controller-manager


###########################################################
#
# Fetching and building Member and Host Operators
#
###########################################################

.PHONY: publish-current-bundles-for-e2e
## Target that is supposed to be called from CI - it builds & publishes the current operator bundles
publish-current-bundles-for-e2e: get-and-publish-operators

.PHONY: get-and-publish-operators
get-and-publish-operators: PUBLISH_OPERATOR=true
get-and-publish-operators: INSTALL_OPERATOR=false
get-and-publish-operators: clean-e2e-files get-and-publish-host-operator get-and-publish-member-operator

.PHONY: get-publish-install-and-register-operators
# IMPORTANT: The host operator needs to be installed first.
#			 The reason is that when the host operator is installed, then the logic creates ToolchainConfig CR which
#			 defines that the webhook should be deployed from the first member instance (and not from the second one).
#			 This is important to set before the member operators are installed, otherwise, it can lead to flaky e2e tests.
get-publish-install-and-register-operators: get-and-publish-host-operator setup-toolchainclusters create-host-resources get-and-publish-member-operator

.PHONY: get-publish-and-install-operators
# IMPORTANT: The host operator needs to be installed first.
#			 The reason is that when the host operator is installed, then the logic creates ToolchainConfig CR which
#			 defines that the webhook should be deployed from the first member instance (and not from the second one).
#			 This is important to set before the member operators are installed, otherwise, it can lead to flaky e2e tests.
get-publish-and-install-operators: get-and-publish-host-operator create-host-resources get-and-publish-member-operator

.PHONY: get-and-publish-member-operator
get-and-publish-member-operator:
ifneq (${MEMBER_NS_2},"")
    ifneq (${MEMBER_NS_2},)
		$(eval MEMBER_NS_2_PARAM = -mn2 ${MEMBER_NS_2})
    endif
endif
ifneq (${MEMBER_REPO_PATH},"")
    ifneq (${MEMBER_REPO_PATH},)
		$(eval MEMBER_REPO_PATH_PARAM = -mr ${MEMBER_REPO_PATH})
    endif
endif
ifneq (${FORCED_TAG},"")
    ifneq (${FORCED_TAG},)
		$(eval FORCED_TAG_PARAM = -ft ${FORCED_TAG})
    endif
endif
	$(MAKE) run-cicd-script SCRIPT_PATH=scripts/ci/manage-member-operator.sh SCRIPT_PARAMS="-po ${PUBLISH_OPERATOR} -io ${INSTALL_OPERATOR} -mn ${MEMBER_NS} ${MEMBER_REPO_PATH_PARAM} -qn ${QUAY_NAMESPACE} -ds ${DATE_SUFFIX} -dl ${DEPLOY_LATEST} ${MEMBER_NS_2_PARAM} ${FORCED_TAG_PARAM}"

.PHONY: get-and-publish-host-operator
get-and-publish-host-operator:
ifneq (${REG_REPO_PATH},"")
    ifneq (${REG_REPO_PATH},)
		$(eval REG_REPO_PATH_PARAM = -rr ${REG_REPO_PATH})
    endif
endif
ifneq (${HOST_REPO_PATH},"")
    ifneq (${HOST_REPO_PATH},)
		$(eval HOST_REPO_PATH_PARAM = -hr ${HOST_REPO_PATH})
    endif
endif
ifneq (${FORCED_TAG},"")
    ifneq (${FORCED_TAG},)
		$(eval FORCED_TAG_PARAM = -ft ${FORCED_TAG})
    endif
endif
	$(MAKE) run-cicd-script SCRIPT_PATH=scripts/ci/manage-host-operator.sh SCRIPT_PARAMS="-po ${PUBLISH_OPERATOR} -io ${INSTALL_OPERATOR} -hn ${HOST_NS} ${HOST_REPO_PATH_PARAM} -ds ${DATE_SUFFIX} -qn ${QUAY_NAMESPACE} -dl ${DEPLOY_LATEST} ${REG_REPO_PATH_PARAM} ${FORCED_TAG_PARAM}"

###########################################################
#
# Deploying Member and Host Operators in Openshift CI Environment
#
###########################################################

.PHONY: prepare-projects
prepare-projects: create-host-project create-member1 create-member2 create-thirdparty-crds

.PHONY: create-member1
create-member1:
	@echo "Preparing namespace for member operator: $(MEMBER_NS)..."
	$(MAKE) create-project PROJECT_NAME=${MEMBER_NS}
	-oc label ns --overwrite=true ${MEMBER_NS} app=member-operator
	oc apply -f deploy/member-operator/${ENVIRONMENT}/ -n ${MEMBER_NS} || true

.PHONY: create-member2
create-member2:
ifeq ($(SECOND_MEMBER_MODE),true)
	@echo "Preparing namespace for second member operator: ${MEMBER_NS_2}..."
	$(MAKE) create-project PROJECT_NAME=${MEMBER_NS_2}
	-oc label ns --overwrite=true ${MEMBER_NS_2} app=member-operator
	oc apply -f deploy/member-operator/${ENVIRONMENT}/ -n ${MEMBER_NS_2} || true
endif

.PHONY: deploy-host
deploy-host: create-host-project get-and-publish-host-operator create-host-resources

.PHONY: create-host-project
create-host-project:
	@echo "Preparing namespace for host operator ${HOST_NS}..."
	$(MAKE) create-project PROJECT_NAME=${HOST_NS}
	-oc label ns --overwrite=true ${HOST_NS} app=host-operator

.PHONY: create-host-resources
create-host-resources: create-spaceprovisionerconfigs-for-members
	# ignore if these resources already exist (nstemplatetiers may have already been created by operator)
	-oc create -f deploy/host-operator/${ENVIRONMENT}/ -n ${HOST_NS}
	# patch toolchainconfig to prevent webhook deploy for 2nd member, a 2nd webhook deploy causes the webhook verification in e2e tests to fail
	# since e2e environment has 2 member operators running in the same cluster
	# for details on how the TOOLCHAINCLUSTER_NAME is composed see https://github.com/codeready-toolchain/toolchain-cicd/blob/master/scripts/add-cluster.sh
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then \
		TOOLCHAIN_CLUSTER_NAME=`oc get toolchaincluster -n ${HOST_NS} --no-headers -o custom-columns=":metadata.name" | grep "2$$"`; \
		if [[ -z $${TOOLCHAIN_CLUSTER_NAME} ]]; then \
			echo "ERROR: no ToolchainCluster for member 2 found"; \
			exit 1; \
		fi; \
		echo "TOOLCHAIN_CLUSTER_NAME $${TOOLCHAIN_CLUSTER_NAME}"; \
		echo "ENVIRONMENT ${ENVIRONMENT}"; \
		PATCH_FILE=/tmp/patch-toolchainconfig_${DATE_SUFFIX}.json; \
		echo "{\"spec\":{\"members\":{\"specificPerMemberCluster\":{\"$${TOOLCHAIN_CLUSTER_NAME}\":{\"webhook\":{\"deploy\":false},\"webConsolePlugin\":{\"deploy\":true},\"environment\":\"${ENVIRONMENT}\"}}}}}" > $$PATCH_FILE; \
		oc patch toolchainconfig config -n ${HOST_NS} --type=merge --patch "$$(cat $$PATCH_FILE)"; \
	fi;
	echo "Restart host operator pods so that configuration referenced in main.go can get the updated ToolchainConfig CRs at startup"
	oc delete pods --namespace ${HOST_NS} -l control-plane=controller-manager
ifneq ($(E2E_TEST_EXECUTION),true)
	# if it's not part of e2e test execution, then delete registration-service pods in case they already exist so that the ToolchainConfig will be reloaded
	oc delete pods --namespace ${HOST_NS} -l name=registration-service || true
endif

.PHONY: create-spaceprovisionerconfigs-for-members
create-spaceprovisionerconfigs-for-members:
	for MEMBER_NAME in `oc get toolchaincluster -n ${HOST_NS} --no-headers -o custom-columns=":metadata.name"`; do \
	  oc process -p TOOLCHAINCLUSTER_NAME=$${MEMBER_NAME} -p SPACEPROVISIONERCONFIG_NAME=$${MEMBER_NAME} -p SPACEPROVISIONERCONFIG_NS=${HOST_NS} -f deploy/host-operator/default-spaceprovisionerconfig.yaml | oc apply -f -; \
	done

.PHONY: create-thirdparty-crds
create-thirdparty-crds:
	oc create -f deploy/crds/ || true

.PHONY: create-project
create-project:
	@-oc new-project ${PROJECT_NAME} 1>/dev/null
	@-oc project ${PROJECT_NAME}
	@echo "adding network policies in $(PROJECT_NAME) namespace"
	@-oc process -p NAMESPACE=$(PROJECT_NAME) -f deploy/default-network-policies.yaml | oc apply -f -
	

.PHONY: display-eval
display-eval:
	@echo 'export HOST_NS=$(shell oc get projects -l app=host-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)'
	@echo 'export MEMBER_NS=$(shell oc get projects -l app=member-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)'
	@echo 'export REGISTRATION_SERVICE_NS=$$HOST_NS'
	@echo '# Run this command to configure your shell:'
	@echo '# eval $$(make display-eval)'


###########################################################
#
# Unit Tests (to verify the assertions and other utilities
# in the `testsupport` package)
#
###########################################################

.PHONY: test
## Run the unit tests in the 'testsupport/...' packages
test:
	@go test github.com/codeready-toolchain/toolchain-e2e/testsupport/... -failfast
