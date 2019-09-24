###########################################################
#
# End-to-end Tests
#
###########################################################

MEMBER_NS := member-operator-$(shell date +'%s')
HOST_NS := host-operator-$(shell date +'%s')
TEST_NS := toolchain-e2e-$(shell date +'%s')
AUTHOR_LINK := $(shell jq -r '.refs[0].pulls[0].author_link' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')
PULL_SHA := $(shell jq -r '.refs[0].pulls[0].sha' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')

IS_CRC := $(shell crc status /dev/null 2>&1 | grep Running)
IS_KUBE_ADMIN := $(shell oc whoami | grep "kube:admin")

.PHONY: test-e2e-keep-namespaces
test-e2e-keep-namespaces: login-as-admin deploy-member deploy-host setup-kubefed e2e-run

.PHONY: deploy-ops
deploy-ops: deploy-member deploy-host

.PHONY: test-e2e-local
test-e2e-local:
	$(MAKE) test-e2e HOST_REPO_PATH=../host-operator MEMBER_REPO_PATH=../member-operator

.PHONY: test-e2e-member-local
test-e2e-member-local:
	$(MAKE) test-e2e MEMBER_REPO_PATH=../member-operator

.PHONY: test-e2e-host-local
test-e2e-host-local:
	$(MAKE) test-e2e HOST_REPO_PATH=../host-operator

.PHONY: test-e2e
test-e2e: build-with-operators test-e2e-keep-namespaces e2e-cleanup

.PHONY: e2e-run
e2e-run:
	oc get kubefedcluster -n $(HOST_NS)
	oc get kubefedcluster -n $(MEMBER_NS)
	oc new-project $(TEST_NS) --display-name e2e-tests
	MEMBER_NS=${MEMBER_NS} HOST_NS=${HOST_NS} operator-sdk test local ./e2e --no-setup --namespace $(TEST_NS) --verbose --go-test-flags "-timeout=15m" || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} && exit 1)

.PHONY: print-logs
print-logs:
	@echo "=====================================================================================" &
	@echo "================================ Host cluster logs =================================="
	@echo "====================================================================================="
	@oc logs deployment.apps/host-operator --namespace $(HOST_NS)
	@echo "====================================================================================="
	@echo "================================ Member cluster logs ================================"
	@echo "====================================================================================="
	@oc logs deployment.apps/member-operator --namespace $(MEMBER_NS)
	@echo "====================================================================================="

.PHONY: login-as-admin
login-as-admin:
ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
    ifeq ($(IS_CRC),)
		$(info logging as system:admin")
		oc login -u system:admin
    else
        ifneq ($(IS_KUBE_ADMIN),)
			$(info logging as kube:admin")
			oc login -u=kubeadmin -p=`cat ~/.crc/cache/crc_libvirt_*/kubeadmin-password`
        endif
    endif
endif

.PHONY: setup-kubefed
setup-kubefed:
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t member -mn $(MEMBER_NS) -hn $(HOST_NS) -s
	curl -sSL https://raw.githubusercontent.com/codeready-toolchain/toolchain-common/master/scripts/add-cluster.sh | bash -s -- -t host -mn $(MEMBER_NS) -hn $(HOST_NS) -s

.PHONY: e2e-cleanup
e2e-cleanup:
	oc delete project ${MEMBER_NS} ${HOST_NS} ${TEST_NS} --wait=false || true

.PHONY: clean-e2e-namespaces
clean-e2e-namespaces:
	$(Q)-oc get projects --output=name | grep -E "(member|host)\-operator\-[0-9]+|toolchain\-e2e\-[0-9]+" | xargs oc delete

###########################################################
#
# Fetching and building Member and Host Operators
#
###########################################################

.PHONY: build-with-operators
build-with-operators: build get-member-operator-repo get-host-operator-repo

.PHONY: get-member-operator-repo
get-member-operator-repo:
ifeq ($(MEMBER_REPO_PATH),)
	$(eval MEMBER_REPO_PATH = /tmp/member-operator)
	rm -rf ${MEMBER_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/member-operator.git --depth 1 ${MEMBER_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(MEMBER_REPO_PATH) REPO_NAME=member-operator
endif

.PHONY: get-host-operator-repo
get-host-operator-repo:
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/host-operator)
	rm -rf ${HOST_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/host-operator.git --depth 1 ${HOST_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(HOST_REPO_PATH) REPO_NAME=host-operator
endif

.PHONY: prepare-e2e-repo
prepare-e2e-repo:
ifneq ($(CLONEREFS_OPTIONS),)
	@echo "using author link ${AUTHOR_LINK}"
	@echo "using pull sha ${PULL_SHA}"
	# get branch ref of the fork the PR was created from
	$(eval BRANCH_REF := $(shell curl ${AUTHOR_LINK}/toolchain-e2e.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a ${PULL_SHA} | awk '{print $$2}'))
	@echo "detected branch ref ${BRANCH_REF}"
	# check if a branch with the same ref exists in the user's fork of ${REPO_NAME} repo
	$(eval REMOTE_E2E_BRANCH := $(shell curl ${AUTHOR_LINK}/${REPO_NAME}.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a ${BRANCH_REF} | awk '{print $$2}'))
	@echo "branch ref of the user's fork: \"${REMOTE_E2E_BRANCH}\" - if empty then not found"
	# check if the branch with the same name exists, if so then merge it with master and use the merge branch, if not then use master
	if [[ -n "${REMOTE_E2E_BRANCH}" ]]; then \
		# retrieve the branch name \
		BRANCH_NAME=`echo ${BRANCH_REF} | awk -F'/' '{print $$3}'`; \
		# add the user's fork as remote repo \
		git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} remote add external ${AUTHOR_LINK}/${REPO_NAME}.git; \
		# fetch the branch; \
		git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} fetch external ${BRANCH_REF}; \
		# merge the branch with master \
		git --git-dir=${E2E_REPO_PATH}/.git --work-tree=${E2E_REPO_PATH} merge FETCH_HEAD; \
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
	$(eval MEMBER_REPO_PATH = /tmp/member-operator)
endif
	oc new-project $(MEMBER_NS)
	oc apply -f ${MEMBER_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/role.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/role_binding.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/cluster_role.yaml
	cat ${MEMBER_REPO_PATH}/deploy/cluster_role_binding.yaml | sed s/\REPLACE_NAMESPACE/$(MEMBER_NS)/ | oc apply -f -
	oc apply -f ${MEMBER_REPO_PATH}/deploy/crds
	$(MAKE) build-and-deploy-operator E2E_REPO_PATH=${MEMBER_REPO_PATH} REPO_NAME=member-operator SET_IMAGE_NAME=${MEMBER_IMAGE_NAME} IS_OTHER_IMAGE_SET=${HOST_IMAGE_NAME}

.PHONY: deploy-host
deploy-host:
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/host-operator)
endif
	oc new-project $(HOST_NS)
	oc apply -f ${HOST_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/role.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/role_binding.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/cluster_role.yaml
	cat ${HOST_REPO_PATH}/deploy/cluster_role_binding.yaml | sed s/\REPLACE_NAMESPACE/$(HOST_NS)/ | oc apply -f -
	oc apply -f ${HOST_REPO_PATH}/deploy/crds
	$(MAKE) build-and-deploy-operator E2E_REPO_PATH=${HOST_REPO_PATH} REPO_NAME=host-operator SET_IMAGE_NAME=${HOST_IMAGE_NAME} IS_OTHER_IMAGE_SET=${MEMBER_IMAGE_NAME}

.PHONY: build-and-deploy-operator
build-and-deploy-operator:
# when e2e tests are triggered from different repo - eg. as part of PR in host-operator repo - and the image of the operator is (not) provided
ifeq ($(SET_IMAGE_NAME),)
    # now we know that the image of the targeted operator is not provided, but can be still triggered in the same use case, but the image of the other operator can be provided
    ifeq ($(IS_OTHER_IMAGE_SET),)
    	# check if it is running locally
        ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
			# if it is running locally, then build the image via docker
			$(eval IMAGE_NAME := docker.io/${GO_PACKAGE_ORG_NAME}/${REPO_NAME}:${GIT_COMMIT_ID_SHORT})
			$(MAKE) -C ${E2E_REPO_PATH} build
			docker build -f ${E2E_REPO_PATH}/build/Dockerfile -t ${IMAGE_NAME} ${E2E_REPO_PATH}
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
	sed -e 's|REPLACE_IMAGE|${IMAGE_NAME}|g' ${E2E_REPO_PATH}/deploy/operator.yaml | oc apply -f -

