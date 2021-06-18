###########################################################
#
# End-to-end Tests
#
###########################################################

QUAY_NAMESPACE ?= codeready-toolchain
DATE_SUFFIX := $(shell date +'%d%H%M%S')
RESOURCES_SUFFIX := ${DATE_SUFFIX}
HOST_NS ?= ${QUAY_NAMESPACE}-host-${DATE_SUFFIX}
MEMBER_NS ?= ${QUAY_NAMESPACE}-member-${DATE_SUFFIX}
MEMBER_NS_2 ?= ${QUAY_NAMESPACE}-member2-${DATE_SUFFIX}
REGISTRATION_SERVICE_NS := $(HOST_NS)
TEST_NS := ${QUAY_NAMESPACE}-toolchain-e2e-${DATE_SUFFIX}
AUTHOR_LINK := $(shell jq -r '.refs[0].pulls[0].author_link' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')
PULL_SHA := $(shell jq -r '.refs[0].pulls[0].sha' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')

ENVIRONMENT := e2e-tests
IMAGE_NAMES_DIR := /tmp/crt-e2e-image-names

SECOND_MEMBER_MODE ?= true

WAS_ALREADY_PAIRED_FILE := /tmp/${GO_PACKAGE_ORG_NAME}_${GO_PACKAGE_REPO_NAME}_already_paired

.PHONY: test-e2e
## Run the e2e tests
test-e2e: deploy-e2e e2e-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: deploy-e2e
deploy-e2e: build-and-pre-clean get-host-and-reg-service deploy-host get-member-operator-repo deploy-members e2e-service-account setup-toolchainclusters

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
	-oc new-project $(TEST_NS) --display-name e2e-tests 1>/dev/null
	MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} go test ./test/e2e -v -timeout=60m -failfast || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} && exit 1)

.PHONY: print-logs
print-logs:
ifneq ($(OPENSHIFT_BUILD_NAMESPACE),)
	$(MAKE) print-operator-logs REPO_NAME=host-operator NAMESPACE=${HOST_NS}
	$(MAKE) print-operator-logs REPO_NAME=member-operator NAMESPACE=${MEMBER_NS}
	$(MAKE) print-operator-logs REPO_NAME=member-operator-webhook NAMESPACE=${MEMBER_NS}
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then $(MAKE) print-operator-logs REPO_NAME=member-operator NAMESPACE=${MEMBER_NS_2}; fi
	$(MAKE) print-operator-logs REPO_NAME=registration-service NAMESPACE=${REGISTRATION_SERVICE_NS}
else
	@echo "you can print logs using the commands:"
	@echo "oc logs deployment.apps/host-operator --namespace ${HOST_NS}"
	@echo "oc logs deployment.apps/member-operator --namespace ${MEMBER_NS}"
	@if [[ ${SECOND_MEMBER_MODE} == true ]]; then echo "oc logs deployment.apps/member-operator --namespace ${MEMBER_NS_2}"; fi
	@echo "oc logs deployment.apps/member-operator-webhook --namespace ${MEMBER_NS}"
	@echo "oc logs deployment.apps/registration-service --namespace ${REGISTRATION_SERVICE_NS}"
endif

.PHONY: print-operator-logs
print-operator-logs:
	@echo "==============================================================================================================="
	@echo "========================== ${REPO_NAME} deployment YAML- Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc get deployment.apps/${REPO_NAME} --namespace ${NAMESPACE} -o yaml
	@echo "==============================================================================================================="
	@echo "========================== ${REPO_NAME} deployment logs - Namespace: ${NAMESPACE} ============================="
	@echo "==============================================================================================================="
	-oc logs deployment.apps/${REPO_NAME} --namespace ${NAMESPACE}
	@echo "==============================================================================================================="

.PHONY: setup-toolchainclusters
setup-toolchainclusters:
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -mn $(MEMBER_NS)   -hn $(HOST_NS) -s
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -mn $(MEMBER_NS_2) -hn $(HOST_NS) -s -mm 2; fi
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host   -mn $(MEMBER_NS)   -hn $(HOST_NS) -s
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host   -mn $(MEMBER_NS_2) -hn $(HOST_NS) -s -mm 2; fi

###########################################################
#
# Fetching and building Member and Host Operators
#
###########################################################

.PHONY: build-with-operators
build-with-operators: build-and-pre-clean get-host-and-reg-service get-member-operator-repo

.PHONY: build-and-pre-clean
build-and-pre-clean: build clean-e2e-files

.PHONY: get-host-and-reg-service
get-host-and-reg-service: get-registration-service-repo get-host-operator-repo

.PHONY: get-member-operator-repo
get-member-operator-repo:
ifeq ($(MEMBER_REPO_PATH),)
	$(eval MEMBER_REPO_PATH = /tmp/codeready-toolchain/member-operator)
	rm -rf ${MEMBER_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/member-operator.git ${MEMBER_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(MEMBER_REPO_PATH) REPO_NAME=member-operator
endif

.PHONY: get-host-operator-repo
get-host-operator-repo:
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/codeready-toolchain/host-operator)
	rm -rf ${HOST_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/host-operator.git ${HOST_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(HOST_REPO_PATH) REPO_NAME=host-operator
endif

.PHONY: get-registration-service-repo
get-registration-service-repo:
ifeq ($(REG_REPO_PATH),)
	$(eval REG_REPO_PATH = /tmp/codeready-toolchain/registration-service)
	rm -rf ${REG_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/registration-service.git ${REG_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(REG_REPO_PATH) REPO_NAME=registration-service
endif

.PHONY: prepare-e2e-repo
prepare-e2e-repo:
ifneq ($(CLONEREFS_OPTIONS),)
	@echo "using author link ${AUTHOR_LINK}"
	@echo "using pull sha ${PULL_SHA}"
	# get branch ref of the fork the PR was created from
	$(eval REPO_URL := ${AUTHOR_LINK}/toolchain-e2e)
	$(eval GET_BRANCH_NAME := curl ${REPO_URL}.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a ${PULL_SHA})
	if [[ `${GET_BRANCH_NAME} | wc -l` > 1 ]]; then \
		echo "###################################  ERROR DURING THE E2E TEST SETUP  ###################################"; \
		echo "There were found more branches with the same latest commit '${PULL_SHA}' in the repo ${REPO_URL} - see:"; \
		echo "`${GET_BRANCH_NAME}`"; \
		echo "It's not possible to detect the correct branch this PR is made for."; \
		echo "Please delete the unrelated branch from your fork and rerun the e2e tests."; \
		echo "Note: If you have already deleted the unrelated branch from your fork, it can take a few hours before the"; \
		echo "      github api is updated so the e2e tests may still fail with the same error until then."; \
		echo "##########################################################################################################"; \
		exit 1; \
	fi; \
	BRANCH_REF=`${GET_BRANCH_NAME} | awk '{print $$2}'`; \
	echo "detected branch ref $${BRANCH_REF}"; \
	if [[ -n "$${BRANCH_REF}" ]]; then \
		# check if a branch with the same ref exists in the user's fork of ${REPO_NAME} repo \
		REMOTE_E2E_BRANCH=`curl ${AUTHOR_LINK}/${REPO_NAME}.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a "$${BRANCH_REF}$$" | awk '{print $$2}'`; \
		echo "branch ref of the user's fork: \"$${REMOTE_E2E_BRANCH}\" - if empty then not found"; \
		# check if the branch with the same name exists, if so then merge it with master and use the merge branch, if not then use master \
		if [[ -n "$${REMOTE_E2E_BRANCH}" ]]; then \
			if [[ -f ${WAS_ALREADY_PAIRED_FILE} ]]; then \
                echo "####################################  ERROR WHILE TRYING TO PAIR PRs  ####################################"; \
				echo "There was an error while trying to pair this e2e PR with ${REPO_URL}@$${BRANCH_REF}"; \
				echo "The reason is that there was alraedy detected a branch from another repo this PR could be paired with - see:"; \
				cat ${WAS_ALREADY_PAIRED_FILE}; \
				echo "It's not possible to pair a PR with multiple branches from other repositories."; \
				echo "Please delete one of the braches from your fork and rerun the e2e tests"; \
				echo "Note: If you have already deleted one of the branches from your fork, it can take a few hours before the"; \
				echo "      github api is updated so the e2e tests may still fail with the same error until then."; \
				echo "##########################################################################################################"; \
				exit 1; \
            fi; \
			if [[ -n "$(OPENSHIFT_BUILD_NAMESPACE)" ]]; then \
				git config --global user.email "devtools@redhat.com"; \
				git config --global user.name "Devtools"; \
			fi; \
			# retrieve the branch name \
			BRANCH_NAME=`echo $${BRANCH_REF} | awk -F'/' '{print $$3}'`; \
			echo -e "repository: ${AUTHOR_LINK}/${REPO_NAME} \nbranch: $${BRANCH_NAME}" > ${WAS_ALREADY_PAIRED_FILE}; \
			# add the user's fork as remote repo \
			git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} remote add external ${AUTHOR_LINK}/${REPO_NAME}.git; \
			# fetch the branch; \
			git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} fetch external $${BRANCH_REF}; \
			# merge the branch with master \
			git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} merge --allow-unrelated-histories --no-commit FETCH_HEAD; \
		fi; \
	fi;
	$(MAKE) -C ${E2E_REPO_PATH} build
	# operators are built, now copy the operators' binaries to make them available for CI
	mkdir -p ${E2E_REPO_PATH}/build/_output/bin/ || true
	mkdir -p build/_output/bin/ || true
	cp ${E2E_REPO_PATH}/build/_output/bin/* build/_output/bin/
endif

###########################################################
#
# Deploying Member and Host Operators in Openshift CI Environment
#
###########################################################

.PHONY: deploy-members
deploy-members:
ifeq ($(MEMBER_REPO_PATH),)
	$(eval MEMBER_REPO_PATH = /tmp/codeready-toolchain/member-operator)
endif
	$(MAKE) build-operator E2E_REPO_PATH=${MEMBER_REPO_PATH} REPO_NAME=member-operator SET_IMAGE_NAME=${MEMBER_IMAGE_NAME} IS_OTHER_IMAGE_SET=${HOST_IMAGE_NAME}${REG_IMAGE_NAME}

	$(MAKE) deploy-member MEMBER_REPO_PATH=${MEMBER_REPO_PATH} MEMBER_NS_TO_DEPLOY=$(MEMBER_NS)

	@echo "Deploying second member without a deploy webhook since it can cause problems with the tests"
	$(eval TMP_ENV_YAML := /tmp/${ENVIRONMENT}_${DATE_SUFFIX}.yaml)
	sed 's|member-operator:|member-operator:\n  deploy-webhook: 'false'|' ${MEMBER_REPO_PATH}/deploy/env/${ENVIRONMENT}.yaml > ${TMP_ENV_YAML}
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then $(MAKE) deploy-member MEMBER_REPO_PATH=${MEMBER_REPO_PATH} MEMBER_NS_TO_DEPLOY=$(MEMBER_NS_2) ENV_YAML=${TMP_ENV_YAML}; fi

.PHONY: deploy-member
deploy-member:
	@echo "Deploying member operator to $(MEMBER_NS_TO_DEPLOY)..."
	-oc new-project $(MEMBER_NS_TO_DEPLOY) 1>/dev/null
	-oc label ns $(MEMBER_NS_TO_DEPLOY) app=member-operator
	-oc project $(MEMBER_NS_TO_DEPLOY)
	$(MAKE) deploy-operator E2E_REPO_PATH=${MEMBER_REPO_PATH} REPO_NAME=member-operator NAMESPACE=$(MEMBER_NS_TO_DEPLOY)

.PHONY: e2e-service-account
e2e-service-account:
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -tn e2e -mn $(MEMBER_NS) -hn $(HOST_NS) -s
	if [[ ${SECOND_MEMBER_MODE} == true ]]; then curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -tn e2e -mn $(MEMBER_NS_2) -hn $(HOST_NS) -s -mm 2; fi

.PHONY: deploy-host
deploy-host: build-registration
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/codeready-toolchain/host-operator)
endif
	@echo "Deploying host operator to $(HOST_NS)..."
	-oc new-project $(HOST_NS) 1>/dev/null
	-oc label ns $(HOST_NS) app=host-operator
	-oc project $(HOST_NS)
	-oc apply -f deploy/host-operator/secrets.yaml -n $(HOST_NS)
	-oc apply -f deploy/host-operator/host-operator-config-map.yaml -n $(HOST_NS)
	# also, add a single `NSTemplateTier` resource before the host-operator controller is deployed. This resource will be updated
	# as the controller starts (which is a use-case for CRT-231)
	oc apply -f ${HOST_REPO_PATH}/deploy/crds/toolchain.dev.openshift.com_nstemplatetiers.yaml
	oc apply -f deploy/host-operator/nstemplatetier-base.yaml -n $(HOST_NS)
	$(MAKE) build-operator E2E_REPO_PATH=${HOST_REPO_PATH} REPO_NAME=host-operator SET_IMAGE_NAME=${HOST_IMAGE_NAME} IS_OTHER_IMAGE_SET=${MEMBER_IMAGE_NAME}${REG_IMAGE_NAME}
	$(MAKE) deploy-operator E2E_REPO_PATH=${HOST_REPO_PATH} REPO_NAME=host-operator NAMESPACE=$(HOST_NS)

.PHONY: build-registration
build-registration:
ifeq ($(REG_REPO_PATH),)
	$(eval REG_REPO_PATH = /tmp/codeready-toolchain/registration-service)
endif
	@echo "Deploying registration-service to $(HOST_NS)..."
	-oc new-project $(HOST_NS) 1>/dev/null
	-oc project $(HOST_NS)
	-oc label ns $(HOST_NS) app=host-operator
	$(MAKE) build-operator E2E_REPO_PATH=${REG_REPO_PATH} REPO_NAME=registration-service SET_IMAGE_NAME=${REG_IMAGE_NAME} IS_OTHER_IMAGE_SET=${MEMBER_IMAGE_NAME}${HOST_IMAGE_NAME}

.PHONY: build-operator
build-operator:
	mkdir ${IMAGE_NAMES_DIR} || true
# when e2e tests are triggered from different repo - eg. as part of PR in host-operator repo - and the image of the operator is (not) provided
ifeq ($(SET_IMAGE_NAME),)
    # now we know that the image of the targeted operator is not provided, but can be still triggered in the same use case, but the image of the other operator can be provided
    ifeq ($(IS_OTHER_IMAGE_SET),)
    	# check if it is running locally
        ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
			#if is running locally, then build the image and push to quay repository
			$(eval IMAGE_NAME := quay.io/${QUAY_NAMESPACE}/${REPO_NAME}:${DATE_SUFFIX})
			$(MAKE) -C ${E2E_REPO_PATH} docker-push QUAY_NAMESPACE=${QUAY_NAMESPACE} IMAGE_TAG=${DATE_SUFFIX}
			curl https://quay.io/api/v1/repository/${QUAY_NAMESPACE}/${REPO_NAME} 2>/dev/null | jq -r '.tags."${DATE_SUFFIX}".manifest_digest' > ${IMAGE_NAMES_DIR}/${REPO_NAME}_digest
			if [[ ${REPO_NAME} == "member-operator" ]]; then \
				curl https://quay.io/api/v1/repository/${QUAY_NAMESPACE}/${REPO_NAME}-webhook 2>/dev/null | jq -r '.tags."${DATE_SUFFIX}".manifest_digest' > ${IMAGE_NAMES_DIR}/${REPO_NAME}-webhook_digest; \
			fi
        else
			# if is running in CI than we expect that it's PR for toolchain-e2e repo (none of the images was provided), so use name that was used by openshift-ci
			$(eval IMAGE_NAME := ${IMAGE_FORMAT}${REPO_NAME})
        endif
    else
		# an image name of the other operator was provided, then we don't have anything built for this one => use image built from master
		$(eval IMAGE_NAME := registry.ci.openshift.org/codeready-toolchain/${REPO_NAME}-v0.1:${REPO_NAME})
    endif
else
	# use the provided image name
	$(eval IMAGE_NAME := ${SET_IMAGE_NAME})
endif
	if [[ -f ${IMAGE_NAMES_DIR}/${REPO_NAME}_digest ]]; then \
	    DIGEST=`cat ${IMAGE_NAMES_DIR}/${REPO_NAME}_digest`; \
	    echo quay.io/${QUAY_NAMESPACE}/${REPO_NAME}@$${DIGEST} > ${IMAGE_NAMES_DIR}/${REPO_NAME}; \
	    if [[ ${REPO_NAME} == "member-operator" ]]; then \
	        DIGEST=`cat ${IMAGE_NAMES_DIR}/${REPO_NAME}-webhook_digest`; \
	    	echo quay.io/${QUAY_NAMESPACE}/${REPO_NAME}-webhook@$${DIGEST} > ${IMAGE_NAMES_DIR}/${REPO_NAME}-webhook; \
	    fi \
	else \
		echo "${IMAGE_NAME}" > ${IMAGE_NAMES_DIR}/${REPO_NAME}; \
        if [[ ${REPO_NAME} == "member-operator" ]]; then \
	    	echo `echo "${IMAGE_NAME}" | cut -d ":" -f1`":${REPO_NAME}-webhook" > ${IMAGE_NAMES_DIR}/${REPO_NAME}-webhook; \
	    fi \
    fi


.PHONY: deploy-operator
deploy-operator:
	$(eval IMAGE_NAME := $(shell cat ${IMAGE_NAMES_DIR}/${REPO_NAME}))
	@echo Using image ${IMAGE_NAME} and namespace ${NAMESPACE} for the repository ${REPO_NAME}
	$(eval REGISTRATION_SERVICE_IMAGE_NAME := $(shell cat ${IMAGE_NAMES_DIR}/registration-service))
	$(eval COMPONENT_IMAGE_REPLACEMENT := ;s|REPLACE_REGISTRATION_SERVICE_IMAGE|${REGISTRATION_SERVICE_IMAGE_NAME}|g)
	$(eval MEMBER_OPERATOR_WEBHOOK_IMAGE := $(shell cat ${IMAGE_NAMES_DIR}/member-operator-webhook))
	$(eval COMPONENT_IMAGE_REPLACEMENT := ${COMPONENT_IMAGE_REPLACEMENT};s|REPLACE_MEMBER_OPERATOR_WEBHOOK_IMAGE|${MEMBER_OPERATOR_WEBHOOK_IMAGE}|g)
	# install operator via CSV
	$(eval NAME_SUFFIX := ${QUAY_NAMESPACE}-${RESOURCES_SUFFIX})
ifeq ($(ENV_YAML),)
	# ENV_YAML is not set
	$(eval ENV_YAML := ${E2E_REPO_PATH}/deploy/env/${ENVIRONMENT}.yaml)
endif
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/api/master/scripts/enrich-by-envs-from-yaml.sh | bash -s -- ${E2E_REPO_PATH}/hack/deploy_csv.yaml ${ENV_YAML} > /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}_source.yaml
	sed -e 's|REPLACE_IMAGE|${IMAGE_NAME}|g;s|^  name: .*|&-${NAME_SUFFIX}|;s|^  configMap: .*|&-${NAME_SUFFIX}|${COMPONENT_IMAGE_REPLACEMENT}' /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}_source.yaml > /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}.yaml
	cat /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}.yaml | oc apply -f -
	# if the namespace already contains the CSV then update it
	if [[ -n `oc get csv 2>/dev/null || true | grep 'toolchain-${REPO_NAME}'` ]]; then \
		oc get cm `grep "^  name: cm" /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}.yaml | awk '{print $$2}'` -n openshift-marketplace --template '{{.data.clusterServiceVersions}}' | sed 's/^. //g;s/namespace: placeholder/namespace: ${NAMESPACE}/'  | oc apply -f -; \
	fi
	sed -e 's|REPLACE_NAMESPACE|${NAMESPACE}|g;s|^  source: .*|&-${NAME_SUFFIX}|' ${E2E_REPO_PATH}/hack/install_operator.yaml > /tmp/${REPO_NAME}_install_operator_${DATE_SUFFIX}.yaml
	cat /tmp/${REPO_NAME}_install_operator_${DATE_SUFFIX}.yaml | oc apply -f -
	while [[ -z `oc get sa ${REPO_NAME} -n ${NAMESPACE} 2>/dev/null` ]] || [[ -z `oc get ClusterRoles | grep "^toolchain-${REPO_NAME}\.v"` ]]; do \
		if [[ $${NEXT_WAIT_TIME} -eq 300 ]]; then \
		   CATALOGSOURCE_NAME=`oc get catalogsource --output=name -n openshift-marketplace | grep "source-toolchain-.*${NAME_SUFFIX}"`; \
		   SUBSCRIPTION_NAME=`oc get subscription --output=name -n ${NAMESPACE} | grep "subscription-toolchain"`; \
		   echo "reached timeout of waiting for ServiceAccount ${REPO_NAME} to be available in namespace ${NAMESPACE} - see following info for debugging:"; \
		   echo "================================ CatalogSource =================================="; \
		   oc get $${CATALOGSOURCE_NAME} -n openshift-marketplace -o yaml; \
		   echo "================================ CatalogSource Pod Logs =================================="; \
		   oc logs `oc get pods -l "olm.catalogSource=$${CATALOGSOURCE_NAME#*/}" -n openshift-marketplace -o name` -n openshift-marketplace; \
		   echo "================================ Subscription =================================="; \
		   oc get $${SUBSCRIPTION_NAME} -n ${NAMESPACE} -o yaml; \
		   echo "================================ InstallPlans =================================="; \
		   oc get installplans -n ${NAMESPACE} -o yaml; \
		   $(MAKE) print-operator-logs REPO_NAME=${REPO_NAME} NAMESPACE=${NAMESPACE}; \
		   exit 1; \
		fi; \
		echo "$$(( NEXT_WAIT_TIME++ )). attempt of waiting for ServiceAccount ${REPO_NAME} in namespace ${NAMESPACE}"; \
		sleep 1; \
	done

.PHONY: display-eval
display-eval:
	@echo 'export HOST_NS=$(shell oc get projects -l app=host-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)'
	@echo 'export MEMBER_NS=$(shell oc get projects -l app=member-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)'
	@echo 'export REGISTRATION_SERVICE_NS=$$HOST_NS'
	@echo '# Run this command to configure your shell:'
	@echo '# eval $$(make display-eval)'
