USE_INSTALLED_KSCTL = false

BIN_DIR := $(shell pwd)/build/_output/bin

ifneq ($(USE_INSTALLED_KSCTL),true)
KSCTL_BIN_DIR = ${BIN_DIR}/
endif

.PHONY: ksctl
ksctl:
ifneq ($(USE_INSTALLED_KSCTL),true)
	@$(MAKE) get-ksctl-and-install --no-print-directory
else
	@echo "Using local version of ksctl"
endif

get-ksctl-and-install:
ifeq ($(strip $(KSCTL_REPO_PATH)),)
    ifneq ($(CI),)
        ifneq ($(GITHUB_ACTIONS),)
			$(eval BRANCH_NAME = ${GITHUB_HEAD_REF})
			$(eval AUTHOR_LINK = https://github.com/${AUTHOR})
        else
			$(eval AUTHOR_LINK = $(shell jq -r '.refs[0].pulls[0].author_link' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]'))
			@echo "found author link ${AUTHOR_LINK}"
			$(eval BRANCH_NAME := $(shell jq -r '.refs[0].pulls[0].head_ref' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]'))
        endif
		@echo "using author link ${AUTHOR_LINK}"
		@echo "detected branch ${BRANCH_NAME}"
		# check if a branch with the same ref exists in the user's fork of ksctl repo
		@echo "branches of ${AUTHOR_LINK}/ksctl - checking if there is a branch ${BRANCH_NAME} we could pair with."
		curl ${AUTHOR_LINK}/ksctl.git/info/refs?service=git-upload-pack --output -
		$(eval REMOTE_KSCTL_BRANCH := $(shell curl ${AUTHOR_LINK}/ksctl.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a "refs/heads/${BRANCH_NAME}$$" | awk '{print $$2}'))
		@echo "branch ref of the user's fork: \"${REMOTE_KSCTL_BRANCH}\" - if empty then not found"
		# check if the branch with the same name exists, if so then merge it with master and use the merge branch, if not then use master
        ifneq ($(REMOTE_KSCTL_BRANCH),"")
			# define temp dir
			$(eval KSCTL_REPO_PATH := /tmp/ksctl)
			# delete to have clear environment
			rm -rf ${KSCTL_REPO_PATH}

			git config --global user.email "devtools@redhat.com"
			git config --global user.name "Devtools"
			# clone
			git clone --depth=1 https://github.com/kubesaw/ksctl.git ${KSCTL_REPO_PATH}
			# add the user's fork as remote repo
			git --git-dir=${KSCTL_REPO_PATH}/.git --work-tree=${KSCTL_REPO_PATH} remote add external ${AUTHOR_LINK}/ksctl.git
			# fetch the branch
			git --git-dir=${KSCTL_REPO_PATH}/.git --work-tree=${KSCTL_REPO_PATH} fetch external ${REMOTE_KSCTL_BRANCH}
			# merge the branch with master
			git --git-dir=${KSCTL_REPO_PATH}/.git --work-tree=${KSCTL_REPO_PATH} merge --allow-unrelated-histories --no-commit FETCH_HEAD
        endif
    endif
endif
	@$(MAKE) install-ksctl KSCTL_REPO_PATH=${KSCTL_REPO_PATH}

install-ksctl:
ifneq ($(strip $(KSCTL_REPO_PATH)),)
	@echo "Installing ksctl from directory ${KSCTL_REPO_PATH}"
	$(MAKE) -C ${KSCTL_REPO_PATH} GOBIN=${BIN_DIR} install
else
	@echo "Installing ksctl from master"
	GOBIN=${BIN_DIR} CGO_ENABLED=0 go install github.com/kubesaw/ksctl/cmd/ksctl@master
endif
