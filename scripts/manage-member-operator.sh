#!/usr/bin/env bash

set -ex

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

. ${SCRIPTS_DIR}/manage-operator.sh $@

REPOSITORY_NAME=member-operator
PROVIDED_REPOSITORY_PATH=${MEMBER_REPO_PATH}
get_repo
set_tags

if [[ ${PUBLISH_OPERATOR} == "true" ]] && [[ -n ${BUNDLE_AND_INDEX_TAG} ]]; then
    push_image

    OPERATOR_IMAGE_LOC=${IMAGE_LOC}
    COMPONENT_IMAGE_LOC=$(echo ${IMAGE_LOC} | sed 's/\/member-operator/\/member-operator-webhook/')

    make -C ${REPOSITORY_PATH} publish-current-bundle ENV=${ENVIRONMENT} INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} BUNDLE_TAG=${BUNDLE_AND_INDEX_TAG} QUAY_NAMESPACE=${QUAY_NAMESPACE} COMPONENT_IMAGE=${COMPONENT_IMAGE_LOC} IMAGE=${OPERATOR_IMAGE_LOC}
fi

if [[ ${INSTALL_OPERATOR} == "true" ]]; then
    if [[ -z ${BUNDLE_AND_INDEX_TAG} ]]; then
        BUNDLE_AND_INDEX_TAG=latest
        QUAY_NAMESPACE=codeready-toolchain
    fi

    make -C ${REPOSITORY_PATH} install-current-operator INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} NAMESPACE=${MEMBER_NS} QUAY_NAMESPACE=${QUAY_NAMESPACE}
    if [[ -n ${MEMBER_NS_2} ]]; then
        make -C ${REPOSITORY_PATH} install-current-operator INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} NAMESPACE=${MEMBER_NS_2} QUAY_NAMESPACE=${QUAY_NAMESPACE}
    fi
fi