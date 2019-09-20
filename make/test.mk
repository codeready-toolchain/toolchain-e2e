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
test-e2e: test-e2e-keep-namespaces e2e-cleanup

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
# Deploying Member and Host Operators in Openshift CI Environment
#
###########################################################

.PHONY: deploy-member
deploy-member:
ifeq ($(MEMBER_REPO_PATH),)
	$(eval MEMBER_REPO_PATH = /tmp/member-operator)
	rm -rf ${MEMBER_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/member-operator.git --depth 1 ${MEMBER_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(MEMBER_REPO_PATH) REPO_NAME=member-operator
endif
	oc new-project $(MEMBER_NS)
	oc apply -f ${MEMBER_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/role.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/role_binding.yaml
	oc apply -f ${MEMBER_REPO_PATH}/deploy/cluster_role.yaml
	cat ${MEMBER_REPO_PATH}/deploy/cluster_role_binding.yaml | sed s/\REPLACE_NAMESPACE/$(MEMBER_NS)/ | oc apply -f -
	oc apply -f ${MEMBER_REPO_PATH}/deploy/crds
ifeq ($(HOST_IMAGE_NAME),)
    ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
		$(eval IMAGE_NAME := docker.io/${GO_PACKAGE_ORG_NAME}/member-operator:${GIT_COMMIT_ID_SHORT})
		$(MAKE) -C ${MEMBER_REPO_PATH} build
		docker build -f ${MEMBER_REPO_PATH}/build/Dockerfile -t ${IMAGE_NAME} ${MEMBER_REPO_PATH}
    else
		$(eval IMAGE_NAME := registry.svc.ci.openshift.org/codeready-toolchain/member-operator-v0.1:member-operator)
    endif
else
	$(eval IMAGE_NAME := $(OPERATOR_IMAGE_NAME))
endif
	sed -e 's|REPLACE_IMAGE|${IMAGE_NAME}|g' ${MEMBER_REPO_PATH}/deploy/operator.yaml | oc apply -f -

.PHONY: deploy-host
deploy-host:
ifeq ($(HOST_REPO_PATH),)
	$(eval HOST_REPO_PATH = /tmp/host-operator)
	rm -rf ${HOST_REPO_PATH}
	# clone
	git clone https://github.com/codeready-toolchain/host-operator.git --depth 1 ${HOST_REPO_PATH}
	$(MAKE) prepare-e2e-repo E2E_REPO_PATH=$(HOST_REPO_PATH) REPO_NAME=host-operator
endif
	oc new-project $(HOST_NS)
	oc apply -f ${HOST_REPO_PATH}/deploy/service_account.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/role.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/role_binding.yaml
	oc apply -f ${HOST_REPO_PATH}/deploy/cluster_role.yaml
	cat ${HOST_REPO_PATH}/deploy/cluster_role_binding.yaml | sed s/\REPLACE_NAMESPACE/$(HOST_NS)/ | oc apply -f -
	oc apply -f ${HOST_REPO_PATH}/deploy/crds
ifeq ($(MEMBER_IMAGE_NAME),)
    ifeq ($(OPENSHIFT_BUILD_NAMESPACE),)
		$(eval IMAGE_NAME := docker.io/${GO_PACKAGE_ORG_NAME}/host-operator:${GIT_COMMIT_ID_SHORT})
		$(MAKE) -C ${HOST_REPO_PATH} build
		docker build -f ${HOST_REPO_PATH}/build/Dockerfile -t ${IMAGE_NAME} ${HOST_REPO_PATH}
    else
		$(eval IMAGE_NAME := registry.svc.ci.openshift.org/codeready-toolchain/host-operator-v0.1:host-operator)
    endif
else
	$(eval IMAGE_NAME := $(OPERATOR_IMAGE_NAME))
endif
	sed -e 's|REPLACE_IMAGE|${IMAGE_NAME}|g' ${HOST_REPO_PATH}/deploy/operator.yaml | oc apply -f -

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
endif

