###########################################################
#
# End-to-end Tests
#
###########################################################

DATE_SUFFIX := $(shell date +'%s')
MEMBER_NS := member-operator-${DATE_SUFFIX}
HOST_NS := host-operator-${DATE_SUFFIX}
REGISTRATION_SERVICE_NS := $(HOST_NS)
TEST_NS := toolchain-e2e-${DATE_SUFFIX}
AUTHOR_LINK := $(shell jq -r '.refs[0].pulls[0].author_link' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')
PULL_SHA := $(shell jq -r '.refs[0].pulls[0].sha' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')

IS_CRC := $(shell oc config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>&1 | grep crc)
IS_KUBE_ADMIN := $(shell oc whoami | grep "kube:admin")
IS_OS_3 := $(shell curl -k -XGET -H "Authorization: Bearer $(shell oc whoami -t 2>/dev/null)" $(shell oc config view --minify -o jsonpath='{.clusters[0].cluster.server}')/version/openshift 2>/dev/null | grep paths)

ENVIRONMENT := e2e-tests

.PHONY: deploy-ops
deploy-ops: deploy-member deploy-host

.PHONY: test-e2e
test-e2e: deploy-e2e e2e-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: deploy-e2e
deploy-e2e: build-with-operators login-as-admin deploy-ops deploy-registration setup-kubefed

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
	oc get kubefedcluster -n $(HOST_NS)
	oc get kubefedcluster -n $(MEMBER_NS)
	-oc new-project $(TEST_NS) --display-name e2e-tests 1>/dev/null
	MEMBER_NS=${MEMBER_NS} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} operator-sdk test local ./test/e2e --no-setup --namespace $(TEST_NS) --verbose --go-test-flags "-timeout=30m" || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} && exit 1)

.PHONY: print-logs
print-logs:
	$(MAKE) print-operator-logs REPO_NAME=host-operator NAMESPACE=${HOST_NS}
	$(MAKE) print-operator-logs REPO_NAME=member-operator NAMESPACE=${MEMBER_NS}
	$(MAKE) print-operator-logs REPO_NAME=registration-service NAMESPACE=${REGISTRATION_SERVICE_NS}

.PHONY: print-operator-logs
print-operator-logs:
	@echo "====================================================================================="
	@echo "========================== ${REPO_NAME} deployment logs ============================="
	@echo "====================================================================================="
	-oc logs deployment.apps/${REPO_NAME} --namespace ${NAMESPACE}
	@echo "====================================================================================="

.PHONY: login-as-admin
login-as-admin:
ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
    ifeq ($(IS_CRC),)
        ifneq ($(IS_OS_3),)
        	# is running locally and against OS 3, so we assume that it's minishift
			$(info logging as system:admin")
			oc login -u system:admin 1>/dev/null
        endif
    else
        ifneq ($(IS_KUBE_ADMIN),)
			$(info logging as kube:admin")
			oc login -u=kubeadmin -p=`cat ~/.crc/cache/crc_libvirt_*/kubeadmin-password` 1>/dev/null
        endif
    endif
endif

.PHONY: setup-kubefed
setup-kubefed:
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -mn $(MEMBER_NS) -hn $(HOST_NS) -s
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host -mn $(MEMBER_NS) -hn $(HOST_NS) -s

.PHONY: clean-e2e-resources
clean-e2e-resources:
	$(Q)-oc get projects --output=name | grep -E "(toolchain\-)?(member|host)\-operator(\-[0-9]+)?|toolchain\-e2e\-[0-9]+" | xargs oc delete
	$(Q)-oc get catalogsource --output=name -n openshift-marketplace | grep "codeready-toolchain-saas" | xargs oc delete -n openshift-marketplace
	$(Q)-oc delete crd kubefedclusters.core.kubefed.io

###########################################################
#
# Fetching and building Member and Host Operators
#
###########################################################

.PHONY: build-with-operators
build-with-operators: build get-member-operator-repo get-host-operator-repo get-registration-service-repo

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
	$(eval BRANCH_REF := $(shell curl ${AUTHOR_LINK}/toolchain-e2e.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a ${PULL_SHA} | awk '{print $$2}'))
	@echo "detected branch ref ${BRANCH_REF}"
	if [[ -n "${BRANCH_REF}" ]]; then \
		# check if a branch with the same ref exists in the user's fork of ${REPO_NAME} repo \
		REMOTE_E2E_BRANCH=`curl ${AUTHOR_LINK}/${REPO_NAME}.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a "${BRANCH_REF}$$" | awk '{print $$2}'`; \
		echo "branch ref of the user's fork: \"$${REMOTE_E2E_BRANCH}\" - if empty then not found"; \
		# check if the branch with the same name exists, if so then merge it with master and use the merge branch, if not then use master \
		if [[ -n "$${REMOTE_E2E_BRANCH}" ]]; then \
			if [[ -n "$(OPENSHIFT_BUILD_NAMESPACE)" ]]; then \
				git config --global user.email "devtools@redhat.com"; \
				git config --global user.name "Devtools"; \
			fi; \
			# retrieve the branch name \
			BRANCH_NAME=`echo ${BRANCH_REF} | awk -F'/' '{print $$3}'`; \
			# add the user's fork as remote repo \
			git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} remote add external ${AUTHOR_LINK}/${REPO_NAME}.git; \
			# fetch the branch; \
			git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} fetch external ${BRANCH_REF}; \
			# merge the branch with master \
			git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} merge --allow-unrelated-histories --no-commit FETCH_HEAD; \
		fi; \
	fi;
	$(MAKE) -C ${E2E_REPO_PATH} build
	# operators are built, now copy the operators' binaries to make them available for CI
	cp ${E2E_REPO_PATH}/build/_output/bin/${REPO_NAME} build/_output/bin/${REPO_NAME}
endif

###########################################################
#
# Deploying Member and Host Operators in Openshift CI Environment
#
###########################################################

.PHONY: deploy-member
deploy-member:
ifeq ($(MEMBER_REPO_PATH),)
	$(eval MEMBER_REPO_PATH = /tmp/codeready-toolchain/member-operator)
endif
	@echo "Deploying member operator to $(MEMBER_NS)..."
	-oc new-project $(MEMBER_NS) 1>/dev/null
	-oc project $(MEMBER_NS)
ifneq ($(IS_OS_3),)
	oc apply -f ${MEMBER_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/role.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/role_binding.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/cluster_role.yaml
	cat ${MEMBER_REPO_PATH}/deploy/cluster_role_binding.yaml | sed s/\REPLACE_NAMESPACE/$(MEMBER_NS)/ | oc apply -f -
	oc apply -f ${MEMBER_REPO_PATH}/deploy/crds
endif
	$(MAKE) build-and-deploy-operator E2E_REPO_PATH=${MEMBER_REPO_PATH} REPO_NAME=member-operator SET_IMAGE_NAME=${MEMBER_IMAGE_NAME} IS_OTHER_IMAGE_SET=${HOST_IMAGE_NAME}${REG_IMAGE_NAME} NAMESPACE=$(MEMBER_NS)

.PHONY: deploy-host
deploy-host:
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/codeready-toolchain/host-operator)
endif
	@echo "Deploying host operator to $(HOST_NS)..."
	-oc new-project $(HOST_NS) 1>/dev/null
	-oc project $(HOST_NS)
ifneq ($(IS_OS_3),)
	# is using OS 3, so we need to deploy the manifests manually
	oc apply -f ${HOST_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/role.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/role_binding.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/cluster_role.yaml
	cat ${HOST_REPO_PATH}/deploy/cluster_role_binding.yaml | sed s/\REPLACE_NAMESPACE/$(HOST_NS)/ | oc apply -f -
	oc apply -f ${HOST_REPO_PATH}/deploy/crds
endif
	$(MAKE) build-and-deploy-operator E2E_REPO_PATH=${HOST_REPO_PATH} REPO_NAME=host-operator SET_IMAGE_NAME=${HOST_IMAGE_NAME} IS_OTHER_IMAGE_SET=${MEMBER_IMAGE_NAME}${REG_IMAGE_NAME} NAMESPACE=$(HOST_NS)
	# also, add a single `NSTemplateTier` resource before the host-operator controller is deployed. This resource will be updated
	# as the controller starts (which is a use-case for CRT-231)
	oc apply -f test/e2e/nstemplatetier-basic.yaml -n $(HOST_NS)

.PHONY: deploy-registration
deploy-registration:
ifeq ($(REG_REPO_PATH),)
	$(eval REG_REPO_PATH = /tmp/codeready-toolchain/registration-service)
endif
	@echo "Deploying registration-service to $(HOST_NS)..."
	-oc new-project $(HOST_NS) 1>/dev/null
	-oc project $(HOST_NS)
	# deploy resources
	oc apply -f ${REG_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${REG_REPO_PATH}/deploy/role.yaml
	oc apply -f ${REG_REPO_PATH}/deploy/role_binding.yaml
	$(MAKE) build-and-deploy-operator E2E_REPO_PATH=${REG_REPO_PATH} REPO_NAME=registration-service SET_IMAGE_NAME=${REG_IMAGE_NAME} IS_OTHER_IMAGE_SET=${MEMBER_IMAGE_NAME}${HOST_IMAGE_NAME} NAMESPACE=$(HOST_NS)

.PHONY: build-and-deploy-operator
build-and-deploy-operator:
# when e2e tests are triggered from different repo - eg. as part of PR in host-operator repo - and the image of the operator is (not) provided
ifeq ($(SET_IMAGE_NAME),)
    # now we know that the image of the targeted operator is not provided, but can be still triggered in the same use case, but the image of the other operator can be provided
    ifeq ($(IS_OTHER_IMAGE_SET),)
    	# check if it is running locally
        ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
                # if it is running locally, then build the image via docker
            ifneq ($(IS_OS_3),)
            	# is running locally and against OS 3, so we assume that it's minishift - it will use local docker registry
				$(eval IMAGE_NAME := docker.io/${GO_PACKAGE_ORG_NAME}/${REPO_NAME}:${DATE_SUFFIX})
				$(MAKE) -C ${E2E_REPO_PATH} build
				docker build -f ${E2E_REPO_PATH}/build/Dockerfile -t ${IMAGE_NAME} ${E2E_REPO_PATH}
            else
            	# if is using OS4 then use quay registry
				$(eval IMAGE_NAME := quay.io/${QUAY_NAMESPACE}/${REPO_NAME}:${DATE_SUFFIX})
				$(MAKE) -C ${E2E_REPO_PATH} build
				docker build -f ${E2E_REPO_PATH}/build/Dockerfile -t ${IMAGE_NAME} ${E2E_REPO_PATH}
				docker push ${IMAGE_NAME}
            endif
        else
			# if is running in CI than we expect that it's PR for toolchain-e2e repo (none of the images was provided), so use name that was used by openshift-ci
			$(eval IMAGE_NAME := registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:${REPO_NAME})
        endif
    else
		# an image name of the other operator was provided, then we don't have anything built for this one => use image built from master
		$(eval IMAGE_NAME := registry.svc.ci.openshift.org/codeready-toolchain/${REPO_NAME}-v0.1:${REPO_NAME})
    endif
else
	# use the provided image name
	$(eval IMAGE_NAME := ${SET_IMAGE_NAME})
endif
	@echo Using image ${IMAGE_NAME} and namespace ${NAMESPACE} for the repository ${REPO_NAME}
ifeq ($(REPO_NAME),registration-service)
	# registration service is not integrated with OLM yet, so deploy it directly
	$(Q)oc process -f ${E2E_REPO_PATH}/deploy/deployment.yaml \
	    -p IMAGE=${IMAGE_NAME} \
	    -p ENVIRONMENT=${ENVIRONMENT} \
        | oc apply -f -
else
    ifeq ($(IS_OS_3),)
		# it is not using OS 3 so we will install operator via CSV
		sed -e 's|REPLACE_IMAGE|${IMAGE_NAME}|g;s|^  name: .*|&-${DATE_SUFFIX}|;s|^  configMap: .*|&-${DATE_SUFFIX}|' ${E2E_REPO_PATH}/hack/deploy_csv.yaml > /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}.yaml
		cat /tmp/${REPO_NAME}_deploy_csv_${DATE_SUFFIX}.yaml | oc apply -f -
		sed -e 's|REPLACE_NAMESPACE|${NAMESPACE}|g;s|^  source: .*|&-${DATE_SUFFIX}|' ${E2E_REPO_PATH}/hack/install_operator.yaml > /tmp/${REPO_NAME}_install_operator_${DATE_SUFFIX}.yaml
		cat /tmp/${REPO_NAME}_install_operator_${DATE_SUFFIX}.yaml | oc apply -f -
		while [[ -z `oc get sa ${REPO_NAME} -n ${NAMESPACE} 2>/dev/null` ]] || [[ -z `oc get crd kubefedclusters.core.kubefed.io 2>/dev/null` ]]; do \
			if [[ $${NEXT_WAIT_TIME} -eq 300 ]]; then \
			   CATALOGSOURCE_NAME=`oc get catalogsource --output=name -n openshift-marketplace | grep "codeready-toolchain-saas.*${DATE_SUFFIX}"`; \
			   SUBSCRIPTION_NAME=`oc get subscription --output=name -n ${NAMESPACE} | grep "codeready-toolchain-saas"`; \
			   echo "reached timeout of waiting for ServiceAccount ${REPO_NAME} to be available in namespace ${NAMESPACE} and CRD kubefedclusters.core.kubefed.io to be available in the cluster - see following info for debugging:"; \
			   echo "================================ CatalogSource =================================="; \
			   oc get $${CATALOGSOURCE_NAME} -n openshift-marketplace -o yaml; \
			   echo "================================ CatalogSource Pod Logs =================================="; \
			   oc logs `oc get pods -l "olm.catalogSource=$${CATALOGSOURCE_NAME#*/}" -n openshift-marketplace -o name` -n openshift-marketplace; \
			   echo "================================ Subscription =================================="; \
			   oc get $${SUBSCRIPTION_NAME} -n ${NAMESPACE} -o yaml; \
			   $(MAKE) print-operator-logs REPO_NAME=${REPO_NAME} NAMESPACE=${NAMESPACE}; \
			   exit 1; \
			fi; \
			echo "$$(( NEXT_WAIT_TIME++ )). attempt of waiting for ServiceAccount ${REPO_NAME} in namespace ${NAMESPACE}" and CRD kubefedclusters.core.kubefed.io to be available in the cluster; \
			sleep 1; \
		done
    else
		sed -e 's|REPLACE_IMAGE|${IMAGE_NAME}|g' ${E2E_REPO_PATH}/deploy/operator.yaml | oc apply -f -
    endif
endif
