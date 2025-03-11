#!/usr/bin/env bash

if [[ -n "${CI}" ]]; then
    set -ex
else
    set -e
fi

WAS_ALREADY_PAIRED_FILE=/tmp/toolchain_e2e_already_paired

get_repo() {
    if [[ -z ${PROVIDED_REPOSITORY_PATH} ]]; then
        REPOSITORY_PATH="/tmp/codeready-toolchain/${REPOSITORY_NAME}"
        
        ${PAIRING_EXEC} pair --clone-dir ${REPOSITORY_PATH} --organization codeready-toolchain --repository ${REPOSITORY_NAME}
    else
        REPOSITORY_PATH=${PROVIDED_REPOSITORY_PATH}
    fi
}

set_tags() {
    TAGS=${DATE_SUFFIX}
    COMMIT_ID_SUFFIX=${PULL_PULL_SHA:0:7}

    if [[ -z "${FORCED_TAG}" ]]; then
        if [[ -n "${CI}${CLONEREFS_OPTIONS}" ]]; then
            if [[ -n ${GITHUB_ACTIONS} ]]; then
                OPERATOR_REPO_NAME=${GITHUB_REPOSITORY##*/}
                TAGS=from.${OPERATOR_REPO_NAME}.PR${PULL_NUMBER}.${COMMIT_ID_SUFFIX}
            else
                AUTHOR=$(jq -r '.refs[0].pulls[0].author' <<< ${CLONEREFS_OPTIONS} | tr -d '[:space:]')
                PULL_PULL_SHA=${PULL_PULL_SHA:-$(jq -r '.refs[0].pulls[0].sha' <<< ${CLONEREFS_OPTIONS} | tr -d '[:space:]')}
                TAGS="from.$(echo ${REPO_NAME} | sed 's/"//g').PR${PULL_NUMBER}.${COMMIT_ID_SUFFIX}"
            fi
        fi
    else
        TAGS=${FORCED_TAG}
    fi
    BUNDLE_AND_INDEX_TAG=${TAGS}
}

push_image() {
    GIT_COMMIT_ID=$(git --git-dir=${REPOSITORY_PATH}/.git --work-tree=${REPOSITORY_PATH} rev-parse --short HEAD)
    IMAGE_LOC=quay.io/codeready-toolchain/${REPOSITORY_NAME}:${GIT_COMMIT_ID}
    IMAGE_BUILDER=${IMAGE_BUILDER:-"podman"}
    make -C ${REPOSITORY_PATH} ${IMAGE_BUILDER}-push QUAY_NAMESPACE=${QUAY_NAMESPACE} IMAGE_TAG=${TAGS}
    IMAGE_LOC=quay.io/${QUAY_NAMESPACE}/${REPOSITORY_NAME}:${TAGS}
}

install_operator() {
    DISPLAYNAME=$(echo ${OPERATOR_NAME} | tr '-' ' ' | awk '{for (i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2)} 1')

    if [[ -z "${INDEX_IMAGE_LOC}" ]]; then
        GIT_COMMIT_ID=`git --git-dir=${REPOSITORY_PATH}/.git --work-tree=${REPOSITORY_PATH} rev-parse --short HEAD`
        INDEX_IMAGE=quay.io/${QUAY_NAMESPACE}/${INDEX_IMAGE_NAME}:${BUNDLE_AND_INDEX_TAG}
        CHANNEL=alpha
    else
        GIT_COMMIT_ID="latest"
        INDEX_IMAGE=${INDEX_IMAGE_LOC}
        CHANNEL=staging
    fi
    CATALOGSOURCE_NAME=${OPERATOR_RESOURCE_NAME}-${GIT_COMMIT_ID}
    SUBSCRIPTION_NAME=${OPERATOR_RESOURCE_NAME}-${GIT_COMMIT_ID}

    # if the operator was already installed in the cluster, then delete all OLM related resources
    for OG in $(oc get OperatorGroup -n ${NAMESPACE} -o name | grep  "${OPERATOR_RESOURCE_NAME}"); do
        oc delete ${OG} -n ${NAMESPACE}
        UNINSTALLED=true
    done
    for SUB in $(oc get Subscription -n ${NAMESPACE} -o name | grep  "${OPERATOR_RESOURCE_NAME}"); do
        oc delete ${SUB} -n ${NAMESPACE}
        UNINSTALLED=true
    done
    for CAT in $(oc get CatalogSource -n ${NAMESPACE} -o name | grep  "${OPERATOR_RESOURCE_NAME}"); do
        oc delete ${CAT} -n ${NAMESPACE}
        UNINSTALLED=true
    done
    for CSV in $(oc get csv -n ${NAMESPACE} -o name | grep  "${OPERATOR_RESOURCE_NAME}"); do
        oc delete ${CSV} -n ${NAMESPACE}
        UNINSTALLED=true
    done

    if [[ ${UNINSTALLED} == "true" ]]; then
        echo "Waiting for the already installed operator ${OPERATOR_NAME} to be uninstalled from the cluster, so the new version can be installed..."
        NEXT_WAIT_TIME=0
        while [[ -n `oc get pods -l control-plane=controller-manager -n ${NAMESPACE} 2>/dev/null || true` ]]; do
            if [[ ${NEXT_WAIT_TIME} -eq 30 ]]; then
               echo "reached timeout of waiting for the already installed operator ${OPERATOR_NAME} to be uninstalled from the cluster, so the new version can be installed..."
               exit 1
            fi
            echo "$(( NEXT_WAIT_TIME++ )). attempt (out of 30) of waiting for operator ${OPERATOR_NAME} to be uninstalled from the cluster."
            sleep 1
        done
    fi
    

    CATALOG_SOURCE_OBJECT="
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${CATALOGSOURCE_NAME}
  namespace: ${NAMESPACE}
spec:
  sourceType: grpc
  image: ${INDEX_IMAGE}
  displayName: ${DISPLAYNAME}
  publisher: Red Hat
  grpcPodConfig:
    securityContextConfig: restricted
  updateStrategy:
    registryPoll:
      interval: 1m0s"
    echo "objects to be created in order to create CatalogSource"
    cat <<EOF | oc apply -f -
${CATALOG_SOURCE_OBJECT}
EOF

    echo "Waiting until the CatalogSource ${CATALOGSOURCE_NAME} in the namespace ${NAMESPACE} gets ready"
    NEXT_WAIT_TIME=0
    while [[ -z `oc get catalogsource ${CATALOGSOURCE_NAME} -n ${NAMESPACE} -o jsonpath='${.status.connectionState.lastObservedState}' 2>/dev/null | grep READY || true` ]]; do
        if [[ ${NEXT_WAIT_TIME} -eq 100 ]]; then
           echo "reached timeout of waiting for the CatalogSource ${CATALOGSOURCE_NAME} in the namespace ${NAMESPACE} to be ready..."
           if [[ -n ${ARTIFACT_DIR} ]]; then
             oc adm must-gather --dest-dir=${ARTIFACT_DIR}
           fi
           exit 1
        fi
        echo "$(( NEXT_WAIT_TIME++ )). attempt (out of 100) of waiting for the CatalogSource ${CATALOGSOURCE_NAME} in the namespace ${NAMESPACE} to be ready."
        sleep 1
    done

    echo "The CatalogSource ${CATALOGSOURCE_NAME} in the namespace ${NAMESPACE} is ready - installing the operator"

    INSTALL_OBJECTS="apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${OPERATOR_RESOURCE_NAME}
  namespace: ${NAMESPACE}
spec:
  targetNamespaces:
  - ${NAMESPACE}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ${SUBSCRIPTION_NAME}
  namespace: ${NAMESPACE}
spec:
  channel: ${CHANNEL}
  installPlanApproval: Automatic
  name: ${OPERATOR_NAME}
  source: ${CATALOGSOURCE_NAME}
  sourceNamespace: ${NAMESPACE}"
    echo "objects to be created in order to install operator"
    cat <<EOF | oc apply -f -
${INSTALL_OBJECTS}
EOF

wait_until_is_installed
}

wait_until_is_installed() {
    PARAMS="-crd ${EXPECT_CRD} -cs ${CATALOGSOURCE_NAME} -n ${NAMESPACE} -s ${SUBSCRIPTION_NAME}"
    scripts/ci/wait-until-is-installed.sh ${PARAMS}
}