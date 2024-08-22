
GH_OWNER=mfrancisc
GH_REPO=toolchain-cicd
GH_BRANCH=fixolmresroucenames
STRIPS_FOLDER=3
OWNER_AND_BRANCH_LOCATION=${GH_OWNER}/${GH_REPO}/${GH_BRANCH}
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

.PHONY: download-assets
## download under /tmp a given folder from github
download-assets:
	@echo "downloading folder ${ASSETS_FOLDER} from https://codeload.github.com/${GH_OWNER}/${GH_REPO}/tar.gz/${GH_BRANCH} repo ..."
	curl https://codeload.github.com/${GH_OWNER}/${GH_REPO}/tar.gz/${GH_BRANCH} | tar -xz --strip=${STRIPS_FOLDER} --directory /tmp toolchain-cicd-${GH_BRANCH}/${ASSETS_FOLDER}