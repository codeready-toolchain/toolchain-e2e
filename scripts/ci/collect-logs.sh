#!/usr/bin/env bash


user_help () {
    echo "Collects all logs from the given namespace"
    echo "options:"
    echo "-n,   --namespace          The namespace the logs should be collected from."
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
                -n|--namespace)
                    shift
                    NAMESPACE=$1
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


start_collecting_logs() {
    if [[ -n ${ARTIFACT_DIR} ]]; then
        if [[ ! -d ${ARTIFACT_DIR} ]]; then
            echo "ERROR: the ARTIFACT_DIR env var is set to ${ARTIFACT_DIR}, but the directory does not exist"
            exit 1
        fi
        COLLECTING_FILE="${ARTIFACT_DIR}/collecting_${NAMESPACE}"

        if [[ ! -f ${COLLECTING_FILE} ]]; then
            touch ${COLLECTING_FILE}
            echo "Collecting logs from namespace ${NAMESPACE}"

            LOGS_DIR=${ARTIFACT_DIR}/logs_${NAMESPACE}
            mkdir ${LOGS_DIR} || true

            COUNTER=0
            PROCESSES=""
            while [[ -n "$(oc whoami 2>/dev/null)" ]] && [[ -f ${COLLECTING_FILE} ]]; do
                if [[ -z "${CI}" ]] && [[ $((  ${COUNTER} % 20 )) == 0 ]]; then
                    echo "Collecting logs from namespace ${NAMESPACE} - to stop the process please delete ${COLLECTING_FILE} file or log out from the cluster"
                fi

                for POD in $(oc get pods -o name -n ${NAMESPACE});
                do

                    for CONTAINER in $(oc get ${POD} -n ${NAMESPACE} -o jsonpath="{.spec.containers[*].name}");
                    do
                        LOG_FILE_NAME=$(echo "${POD}-${CONTAINER}" | sed 's|/|-|g')
                        LOG_FILE=${LOGS_DIR}/${LOG_FILE_NAME}

                        if [[ ! -f ${LOG_FILE} ]]; then
                            if [[ -n $(oc logs ${POD} -c ${CONTAINER} -n ${NAMESPACE} 2>/dev/null || true) ]]; then
                                echo "collecting logs from container ${CONTAINER} in pod ${POD} in namespace ${NAMESPACE} to file ${LOG_FILE}"
                                oc logs ${POD} -c ${CONTAINER} -n ${NAMESPACE} -f > ${LOG_FILE} &
                                PROCESSES="${PROCESSES}$! "
                            fi
                        fi
                    done
                done
                sleep 1
                COUNTER=$(( COUNTER + 1 ))
            done
            echo "killing the daemon processes ${PROCESSES}"
            kill ${PROCESSES}
        fi
    else
        if [[ -n "${CI}" ]]; then
            echo "ARTIFACT_DIR env var is not set - no logs will be collected"
        fi
    fi
}

set -e

read_arguments $@
start_collecting_logs