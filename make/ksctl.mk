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
    ifneq ($(CI_DISABLE_PAIRING),true)
		${PAIRING_EXEC} pair --clone-dir /tmp/ksctl --organization kubesaw --repository ksctl
		$(eval KSCTL_REPO_PATH := /tmp/ksctl)
    else
		@echo "Pairing explicitly disabled"
    endif
else
	@echo "KSCTL_REPO_PATH is set to ${KSCTL_REPO_PATH} - no pairing"
endif
	@$(MAKE) install-ksctl KSCTL_REPO_PATH=${KSCTL_REPO_PATH} REMOTE_KSCTL_BRANCH=${REMOTE_KSCTL_BRANCH}

install-ksctl:
ifneq ($(strip $(KSCTL_REPO_PATH)$(REMOTE_KSCTL_BRANCH)),)
	@echo "Installing ksctl from directory $(or ${KSCTL_REPO_PATH}, /tmp/ksctl)"
	$(MAKE) -C $(or ${KSCTL_REPO_PATH}, /tmp/ksctl) GOBIN=${BIN_DIR} install
else
	@echo "Installing ksctl from master"
	GOBIN=${BIN_DIR} CGO_ENABLED=0 go install github.com/kubesaw/ksctl/cmd/ksctl@master
endif
