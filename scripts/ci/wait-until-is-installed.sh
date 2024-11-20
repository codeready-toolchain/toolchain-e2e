#!/usr/bin/env bash


user_help () {
    echo "Waits until the given CRD is present in the cluster."
    echo "options:"
    echo "-crd, --expect-crd         CRD name to be present in the cluster - it's a sign that the operator is installed."
    echo "-cs,  --catalogsource      CatalogSource name of the operator that is being installed."
    echo "-n,   --namespace          The target namespace the operator is being installed in."
    echo "-s,   --subscription       Subscription name of the operator that is being installed."
    echo "-h,   --help               To show this help text"
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
                -crd|--expect-crd)
                    shift
                    EXPECT_CRD=$1
                    shift
                    ;;
                -cs|--catalogsource)
                    shift
                    CATALOGSOURCE_NAME=$1
                    shift
                    ;;
                -n|--namespace)
                    shift
                    NAMESPACE=$1
                    shift
                    ;;
                -s|--subscription)
                    shift
                    SUBSCRIPTION_NAME=$1
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

wait_until_is_installed() {
    echo "Waiting for CRD ${EXPECT_CRD} to be available in the cluster..."

    ATTEMPT=0
    MAX_NUM_ATTEMPTS=100
    SLEEP_TIME=1
    if [[ -n "${CI}${CLONEREFS_OPTIONS}" ]]; then
        MAX_NUM_ATTEMPTS=200
        SLEEP_TIME=3
    fi

    while [[ -z `oc get crd | grep ${EXPECT_CRD} || true` ]] || [[ -z $(oc get installplans -o jsonpath='{.items[*].status.phase}' -n ${NAMESPACE} | grep "Complete" || true) ]]; do
        if [[ ${ATTEMPT} != 0 ]] && [[ $(( ${ATTEMPT} % 40 )) == 0 ]]; then
            echo "The installation takes suspiciously too long - deleting jobs. This may happen and is caused by flaky OLM installation."
            oc delete jobs --all -n ${NAMESPACE}
        fi
        if [[ ${ATTEMPT} -eq ${MAX_NUM_ATTEMPTS} ]]; then
           echo "reached timeout of waiting for CRD ${EXPECT_CRD} to be available in the cluster and the InstallPlan to be complete - see following info for debugging:"
           if [[ -n "${CATALOGSOURCE_NAME}" ]]; then
              echo "================================ CatalogSource =================================="
              oc get catalogsource ${CATALOGSOURCE_NAME} -n ${NAMESPACE} -o yaml
           fi
           if [[ -n "${SUBSCRIPTION_NAME}" ]]; then
              echo "================================ Subscription =================================="
              oc get subscription ${SUBSCRIPTION_NAME} -n ${NAMESPACE} -o yaml
              echo "================================ InstallPlans =================================="
           fi
           oc get installplans -n ${NAMESPACE} -o yaml
           if [[ -n ${ARTIFACT_DIR} ]]; then
             oc adm must-gather --dest-dir=${ARTIFACT_DIR}
             oc get jobs -o yaml -n ${NAMESPACE} > ${ARTIFACT_DIR}/jobs_${NAMESPACE}
             oc get jobs -o yaml -A > ${ARTIFACT_DIR}/all_jobs_${NAMESPACE}
           fi
           exit 1
        fi
        echo "$(( ATTEMPT++ )). attempt (out of ${MAX_NUM_ATTEMPTS}) of waiting for CRD ${EXPECT_CRD} to be available in the cluster and the InstallPlan to be complete"
        sleep ${SLEEP_TIME}
    done
}

set -e

read_arguments $@
wait_until_is_installed