#!/usr/bin/env bash

set -ex


SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

. ${SCRIPTS_DIR}/manage-operator.sh $@

create_host_resources() {
    oc apply -f ${REPOSITORY_PATH}/config/crd/bases/toolchain.dev.openshift.com_toolchainconfigs.yaml
	oc apply -f deploy/host-operator/${ENVIRONMENT}/ -n ${HOST_NS}
	# patch toolchainconfig to prevent webhook deploy for 2nd member, a 2nd webhook deploy causes the webhook verification in e2e tests to fail
	# since e2e environment has 2 member operators running in the same cluster
	if [[ -n ${MEMBER_NS_2} ]]; then
		API_ENDPOINT=`oc get infrastructure cluster -o jsonpath='{.status.apiServerURL}'`
		TOOLCHAIN_CLUSTER_NAME=`echo "$${API_ENDPOINT}" | sed 's/.*api\.\([^:]*\):.*/\1/'`
		echo "API_ENDPOINT $${API_ENDPOINT}"
		echo "TOOLCHAIN_CLUSTER_NAME $${TOOLCHAIN_CLUSTER_NAME}"
		PATCH_FILE=/tmp/patch-toolchainconfig_${DATE_SUFFIX}.json
		echo "{\"spec\":{\"members\":{\"specificPerMemberCluster\":{\"member-$${TOOLCHAIN_CLUSTER_NAME}2\":{\"webhook\":{\"deploy\":false}}}}}}" > $$PATCH_FILE
		oc patch toolchainconfig config -n $(HOST_NS) --type=merge --patch "$$(cat $$PATCH_FILE)"
	fi
}

REPOSITORY_NAME=registration-service
PROVIDED_REPOSITORY_PATH=${REG_REPO_PATH}
get_repo
set_tags

if [[ ${PUBLISH_OPERATOR} == "true" ]]; then
    push_image
    REG_SERV_IMAGE_LOC=${IMAGE_LOC}
    REG_REPO_PATH=${REPOSITORY_PATH}
fi


REPOSITORY_NAME=host-operator
PROVIDED_REPOSITORY_PATH=${HOST_REPO_PATH}
get_repo
set_tags

# can be used only when the operator CSV doesn't bundle the environment information, but now we want to build bundle for both operators
# if [[ ${PUBLISH_OPERATOR} == "true" ]] && [[ -n ${BUNDLE_AND_INDEX_TAG} ]]; then
if [[ ${PUBLISH_OPERATOR} == "true" ]]; then
    push_image
    OPERATOR_IMAGE_LOC=${IMAGE_LOC}
    make -C ${REPOSITORY_PATH} publish-current-bundle ENV=${ENVIRONMENT} INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} BUNDLE_TAG=${BUNDLE_AND_INDEX_TAG} QUAY_NAMESPACE=${QUAY_NAMESPACE} OTHER_REPO_PATH=${REG_REPO_PATH} OTHER_REPO_IMAGE_LOC=${REG_SERV_IMAGE_LOC} IMAGE=${OPERATOR_IMAGE_LOC}
fi

if [[ ${INSTALL_OPERATOR} == "true" ]]; then
#    can be used only when the operator CSV doesn't bundle the environment information, but now we want to build bundle for both operators
#    if [[ -z ${BUNDLE_AND_INDEX_TAG} ]]; then
#        BUNDLE_AND_INDEX_TAG=latest
#        QUAY_NAMESPACE=codeready-toolchain
#    fi

    make -C ${REPOSITORY_PATH} install-current-operator INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} NAMESPACE=${HOST_NS} QUAY_NAMESPACE=${QUAY_NAMESPACE}
fi

create_host_resources
