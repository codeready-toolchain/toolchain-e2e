#!/usr/bin/env bash

user_help () {
    echo "Publishes member operator to quay and deploys it to an OpenShift cluster"
    echo "options:"
    echo "-po, --publish-operator   Builds and pushes the operator to quay"
    echo "-qn, --quay-namespace     Quay namespace the images should be pushed to"
    echo "-io, --install-operator   Installs the operator to an OpenShift cluster"
    echo "-mn, --member-namespace   Namespace the operator should be installed to"
    echo "-mn2,--member-namespace-2 Namespace name of the second installation of member operator, if needed"
    echo "-mr, --member-repo-path   Path to the member operator repo"
    echo "-ds, --date-suffix        Date suffix to be added to some resources that are created"
    echo "-ft, --forced-tag         Forces a tag to be set to all built images. In the case deployment the tag is used for index image in the created CatalogSource"
    echo "-h,  --help               To show this help text"
    echo ""
    exit 0
}

read_arguments() {
    if [[ $# -lt 2 ]]
    then
        user_help
    fi

    while test $# -gt 0; do
           case "$1" in
                -h|--help)
                    user_help
                    ;;
                -po|--publish-operator)
                    shift
                    PUBLISH_OPERATOR=$1
                    shift
                    ;;
                -qn|--quay-namespace)
                    shift
                    QUAY_NAMESPACE=$1
                    shift
                    ;;
                -io|--install-operator)
                    shift
                    INSTALL_OPERATOR=$1
                    shift
                    ;;
                -mn|--member-namespace)
                    shift
                    MEMBER_NS=$1
                    shift
                    ;;
                -mn2|--member-namespace-2)
                    shift
                    MEMBER_NS_2=$1
                    shift
                    ;;
                -mr|--member-repo-path)
                    shift
                    MEMBER_REPO_PATH=$1
                    shift
                    ;;
                -ds|--date-suffix)
                    shift
                    DATE_SUFFIX=$1
                    shift
                    ;;
                -ft|--forced-tag)
                    shift
                    FORCED_TAG=$1
                    shift
                    ;;
                *)
                   echo "$1 is not a recognized flag!" >> /dev/stderr
                   user_help
                   exit -1
                   ;;
          esac
    done
}

set -e

read_arguments $@

if [[ -n "${CI}" ]]; then
    set -ex
else
    set -e
fi

MANAGE_OPERATOR_FILE=scripts/ci/manage-operator.sh
OWNER_AND_BRANCH_LOCATION=${OWNER_AND_BRANCH_LOCATION:-codeready-toolchain/toolchain-cicd/master}
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
SCRIPT_NAME=$(basename ${MANAGE_OPERATOR_FILE})

if [[ -f ${SCRIPT_DIR}/${SCRIPT_NAME} ]]; then
    source ${SCRIPT_DIR}/${SCRIPT_NAME}
else
    source /dev/stdin <<< "$(curl -sSL https://raw.githubusercontent.com/${OWNER_AND_BRANCH_LOCATION}/${MANAGE_OPERATOR_FILE})"
fi

if [[ -n "${CI}${MEMBER_REPO_PATH}" ]] && [[ $(echo ${REPO_NAME} | sed 's/"//g') != "release" ]]; then
    REPOSITORY_NAME=member-operator
    PROVIDED_REPOSITORY_PATH=${MEMBER_REPO_PATH}
    get_repo
    set_tags

    if [[ ${PUBLISH_OPERATOR} == "true" ]]; then
        push_image

        OPERATOR_IMAGE_LOC=${IMAGE_LOC}
        make -C ${REPOSITORY_PATH} publish-current-bundle INDEX_IMAGE_TAG=${BUNDLE_AND_INDEX_TAG} BUNDLE_TAG=${BUNDLE_AND_INDEX_TAG} QUAY_NAMESPACE=${QUAY_NAMESPACE} IMAGE=${OPERATOR_IMAGE_LOC}
    fi
else
    INDEX_IMAGE_LOC="quay.io/codeready-toolchain/member-operator-index:latest"
fi


if [[ ${INSTALL_OPERATOR} == "true" ]]; then
    OPERATOR_RESOURCE_NAME=member-operator
    OPERATOR_NAME=toolchain-member-operator
    INDEX_IMAGE_NAME=member-operator-index
    NAMESPACE=${MEMBER_NS}
    EXPECT_CRD=memberoperatorconfigs.toolchain.dev.openshift.com
    install_operator
    if [[ -n ${MEMBER_NS_2} ]]; then
        NAMESPACE=${MEMBER_NS_2}
        install_operator
    fi
fi
