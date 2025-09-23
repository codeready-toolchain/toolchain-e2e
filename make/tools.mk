# Tool versions
KUSTOMIZE_VERSION=v5.4.3

# Tool binaries
KUSTOMIZE = $(shell pwd)/bin/kustomize

.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

# go-get-tool will 'go get' any package $2 with version $3 and install it to $1.
PROJECT_DIR := $(shell pwd)
VERSIONS_FILE := $(PROJECT_DIR)/bin/version

define go-get-tool
@if [[ ! -f ${1} ]] || [[ ! -f ${VERSIONS_FILE} ]] || [[ -z $$(grep ${2} ${VERSIONS_FILE} | grep '${3}$$') ]]; then \
	set -e ;\
	TMP_DIR=$$(mktemp -d) ;\
	cd $${TMP_DIR} ;\
	go mod init tmp ;\
	echo "Downloading ${2}" ;\
	GOBIN=$(PROJECT_DIR)/bin go install ${2}@${3} ;\
	mkdir -p $(PROJECT_DIR)/bin ;\
	touch ${VERSIONS_FILE} ;\
	sed '\|${2}|d' ${VERSIONS_FILE} > $${TMP_DIR}/versions ;\
	mv $${TMP_DIR}/versions ${VERSIONS_FILE} ;\
	echo "${2} ${3}" >> ${VERSIONS_FILE} ;\
	rm -rf $$TMP_DIR ;\
fi
endef
