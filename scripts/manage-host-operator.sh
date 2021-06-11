#!/usr/bin/env bash

set -ex


SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

. ${SCRIPTS_DIR}/manage-operator.sh $@

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

if [[ ${PUBLISH_OPERATOR} == "true" ]] && [[ -n ${BUNDLE_AND_INDEX_TAG} ]]; then
    push_image
    OPERATOR_IMAGE_LOC=${IMAGE_LOC}
    make -C ${REPOSITORY_PATH} publish-current-bundle ENV=${ENVIRONMENT} INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} BUNDLE_TAG=${BUNDLE_AND_INDEX_TAG} QUAY_NAMESPACE=${QUAY_NAMESPACE} OTHER_REPO_PATH=${REG_REPO_PATH} OTHER_REPO_IMAGE_LOC=${REG_SERV_IMAGE_LOC} IMAGE=${OPERATOR_IMAGE_LOC}
fi

if [[ ${INSTALL_OPERATOR} == "true" ]]; then
    if [[ -z ${BUNDLE_AND_INDEX_TAG} ]]; then
        BUNDLE_AND_INDEX_TAG=latest
        QUAY_NAMESPACE=codeready-toolchain
    fi

    make -C ${REPOSITORY_PATH} install-current-operator INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} NAMESPACE=${HOST_NS} QUAY_NAMESPACE=${QUAY_NAMESPACE}
    oc apply -f ${SCRIPTS_DIR}/../deploy/host-operator/ -n ${HOST_NS}
fi
