# By default the project should be build under GOPATH/src/github.com/<orgname>/<reponame>
GO_PACKAGE_ORG_NAME ?= $(shell basename $$(dirname $$PWD))
GO_PACKAGE_REPO_NAME ?= $(shell basename $$PWD)
GO_PACKAGE_PATH ?= github.com/${GO_PACKAGE_ORG_NAME}/${GO_PACKAGE_REPO_NAME}

GO111MODULE?=on
export GO111MODULE
goarch?=$(shell go env GOARCH) 

.PHONY: build
## Build e2e test files
build:
	@go version
	mkdir -p $(OUT_DIR)/bin || true
	$(Q)CGO_ENABLED=0 GOARCH=${goarch} GOOS=linux \
		go build ${V_FLAG} ./...

.PHONY: verify-dependencies
## Runs commands to verify after the updated dependecies of toolchain-common/API(go mod replace), if the repo needs any changes to be made
verify-dependencies: tidy vet go-test-skip-all test lint-go-code

.PHONY: tidy
tidy: 
	go mod tidy

.PHONY: vet
vet:
	go vet ./...

.PHONY: go-test-skip-all
go-test-skip-all:
	go test ./... -skip '.*'