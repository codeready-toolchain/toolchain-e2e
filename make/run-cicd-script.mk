
OWNER_AND_BRANCH_LOCATION=mfrancisc/toolchain-cicd/feature/ASC-245_cluster_labels
GH_SCRIPTS_URL=https://raw.githubusercontent.com/${OWNER_AND_BRANCH_LOCATION}
USE_LOCAL_SCRIPTS=false

.PHONY: run-cicd-script
## Run script from toolchain-cicd repo. If the USE_LOCAL_SCRIPTS var is true, then it runs the local version. If not, then it downloads the script from master
run-cicd-script:
ifeq ($(USE_LOCAL_SCRIPTS),true)
	@echo "running the script from local toolchain-cicd repo..."
	../toolchain-cicd/${SCRIPT_PATH} ${SCRIPT_PARAMS}
else
	@echo "running script from GH toolchain-cicd repo (using latest version in master)..."
	$(eval SCRIPT_NAME := $(shell basename ${SCRIPT_PATH}))
	curl -sSL ${GH_SCRIPTS_URL}/${SCRIPT_PATH} > /tmp/${SCRIPT_NAME} && chmod +x /tmp/${SCRIPT_NAME} && OWNER_AND_BRANCH_LOCATION=${OWNER_AND_BRANCH_LOCATION} /tmp/${SCRIPT_NAME} ${SCRIPT_PARAMS}
endif