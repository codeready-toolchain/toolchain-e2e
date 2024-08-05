KSCTL_GH_OWNER=mfrancisc
KSCTL_GH_BRANCH=fixclientrestconfig

BIN_DIR := $(shell pwd)/build/_output/bin

ksctl:
	GOBIN=${BIN_DIR} CGO_ENABLED=0 go install github.com/${KSCTL_GH_OWNER}/ksctl/cmd/ksctl@${KSCTL_GH_BRANCH}
