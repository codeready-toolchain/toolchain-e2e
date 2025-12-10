#!/usr/bin/env bash

user_help () {
    echo "Publishes Developer Sandbox Dashboard to quay and deploys it to an OpenShift cluster"
    echo "options:"
    echo "-pp, --publish-ui         Builds and pushes the UI to quay"
    echo "-qn, --quay-namespace     Quay namespace the images should be pushed to"
    echo "-ur, --ui-repo-path       Path to the UI repo"
    echo "-du, --deploy-ui          Deploys the UI to the OpenShift cluster"
    echo "-ds, --date-suffix        Date suffix to be added to some resources that are created"
    echo "-ft, --forced-tag         Forces a tag to be set to all built images. In the case deployment the tag is used for index image in the created CatalogSource"
    echo "-ns, --namespace          Namespace to deploy the Developer Sandbox Dashboard"
    echo "-os, --openid-secret      OpenID secret name"
    echo "-en, --environment        Environment name"
    echo "-dl, --deploy-latest      Deploys the latest version of the Developer Sandbox Dashboard"
    echo "-h,  --help               To show this help text"
    echo ""
    exit 0
}

read_arguments() {
    if [[ $# -lt 2 ]]
    then
        user_help
    fi

    while [[ $# -gt 0 ]]; do
        local arg="$1"
        case "$arg" in
                -h|--help)
                    user_help
                    ;;
                -pp|--publish-ui)
                    shift
                    PUBLISH_UI=$1
                    shift
                    ;;
                -qn|--quay-namespace)
                    shift
                    QUAY_NAMESPACE=$1
                    shift
                    ;;
                -ur|--ui-repo-path)
                    shift
                    UI_REPO_PATH=$1
                    shift
                    ;;
                -du|--deploy-ui)
                    shift
                    DEPLOY_UI=$1
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
                -ns|--namespace)
                    shift
                    DEVSANDBOX_DASHBOARD_NS=$1
                    shift
                    ;;
                -os|--openid-secret)
                    shift
                    OPENID_SECRET_NAME=$1
                    shift
                    ;;
                -en|--environment)
                    shift
                    ENVIRONMENT=$1
                    shift
                    ;;
                -dl|--deploy-latest)
                    shift
                    DEPLOY_LATEST=$1
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

check_sso_credentials() {
    echo "checking SSO credentials..."
    
    if [[ -z "${SSO_USERNAME_READ}" ]] || [[ -z "${SSO_PASSWORD_READ}" ]]; then
        if [[ -n "${CI}" ]]; then
            echo "SSO credential files not found or empty in CI environment"
        else
            echo "SSO_USERNAME or SSO_PASSWORD environment variables not set"
        fi
        exit 1
    fi
    
    echo "Validating SSO credentials..."
    
    status=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST "https://sso.devsandbox.dev/auth/realms/sandbox-dev/protocol/openid-connect/token" \
        -d "grant_type=password" \
        -d "client_id=sandbox-public" \
        -d "username=${SSO_USERNAME_READ}" \
        -d "password=${SSO_PASSWORD_READ}")
    
    if [[ "${status}" != "200" ]]; then
        echo "failed trying to login to 'https://sso.devsandbox.dev/auth/realms/sandbox-dev' (${status}) — check your SSO credentials."
        exit 1
    fi
    
    echo "SSO credentials validated successfully"
}

configure_oauth_idp() {
    echo "configuring DevSandbox identity provider"
    
    oc create secret generic ${OPENID_SECRET_NAME} \
        --from-literal=clientSecret=dummy \
        --namespace=openshift-config \
        --dry-run=client -o yaml | oc apply -f -
    
    OPENID_SECRET_NAME=${OPENID_SECRET_NAME} envsubst < deploy/devsandbox-dashboard/ui-e2e-tests/oauth-idp-patch.yaml | \
        oc patch oauths.config.openshift.io/cluster --type=merge --patch-file=/dev/stdin
}

create_namespace() {
    if ! oc get project ${DEVSANDBOX_DASHBOARD_NS} >/dev/null 2>&1; then
        echo "Creating namespace ${DEVSANDBOX_DASHBOARD_NS}"
        oc new-project ${DEVSANDBOX_DASHBOARD_NS} >/dev/null 2>&1 || true
    else
        echo "Namespace ${DEVSANDBOX_DASHBOARD_NS} already exists"
    fi
    oc project ${DEVSANDBOX_DASHBOARD_NS} >/dev/null 2>&1
}

set -e

read_arguments "$@"

if [[ -n "${CI}" ]]; then
    set -ex
else
    set -e
fi

source scripts/ci/manage-operator.sh

# Global variables for the script
IMAGE_LOC="quay.io/codeready-toolchain/sandbox-rhdh-plugin:latest"

if [[ "${DEPLOY_LATEST}" != "true" ]] && [[ -n "${CI}${UI_REPO_PATH}" ]] && [[ $(echo ${REPO_NAME} | sed 's/"//g') != "release" ]]; then
    REPOSITORY_NAME=devsandbox-dashboard
    PROVIDED_REPOSITORY_PATH=${UI_REPO_PATH}
    get_repo
    set_tags

    if is_provided_or_paired; then
        IMAGE_LOC="quay.io/${QUAY_NAMESPACE}/sandbox-rhdh-plugin:${TAGS}"
        if [[ ${PUBLISH_UI} == "true" ]]; then
            # push image if provided or paired, otherwise use default image
            echo "Going to push Developer Sandbox Dashboard image..."
            IMAGE_BUILDER=${IMAGE_BUILDER:-"podman"}
            make -C ${REPOSITORY_PATH} ${IMAGE_BUILDER}-push QUAY_NAMESPACE=${QUAY_NAMESPACE} IMAGE_TAG=${TAGS}
        fi
    fi
fi

if [[ ${DEPLOY_UI} == "true" ]]; then
    # Get the HOST_NS (host operator namespace)
    HOST_NS=$(oc get projects -l app=host-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)

    # Get the Registration Service API URL
    REGISTRATION_SERVICE_API="https://$(oc get route registration-service -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')/api/v1"

    # Get the Host Operator API URL
    HOST_OPERATOR_API="https://$(oc get route api -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')"

    # Get the RHDH URL
    RHDH="https://rhdh-${DEVSANDBOX_DASHBOARD_NS}.$(oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')"

    echo "Developer Sandbox Dashboard will be deployed in '${DEVSANDBOX_DASHBOARD_NS}' namespace"

    SSO_USERNAME_READ=$(if [[ -n "${CI}" ]]; then cat /usr/local/sandbox-secrets/SSO_USERNAME 2>/dev/null || echo ""; else echo "${SSO_USERNAME}"; fi)
    SSO_PASSWORD_READ=$(if [[ -n "${CI}" ]]; then cat /usr/local/sandbox-secrets/SSO_PASSWORD 2>/dev/null || echo ""; else echo "${SSO_PASSWORD}"; fi)

    # Call check-sso-credentials
    check_sso_credentials

    # Create namespace
    create_namespace

    # Apply kustomize with envsubst
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

    oc kustomize ${REPO_ROOT}/deploy/devsandbox-dashboard/${ENVIRONMENT} | \
        REGISTRATION_SERVICE_API=${REGISTRATION_SERVICE_API} \
        HOST_OPERATOR_API=${HOST_OPERATOR_API} \
        DEVSANDBOX_DASHBOARD_NS=${DEVSANDBOX_DASHBOARD_NS} \
        SANDBOX_PLUGIN_IMAGE=${IMAGE_LOC} \
        RHDH=${RHDH} envsubst | oc apply -f -

    # Configure OAuth IDP
    configure_oauth_idp

    # Conditionally apply toolchainconfig changes
    if [[ "${ENVIRONMENT}" == "ui-e2e-tests" ]]; then
        echo "applying toolchainconfig changes"
        oc apply -f ${REPO_ROOT}/deploy/host-operator/ui-e2e-tests/toolchainconfig.yaml -n ${HOST_NS}
        echo "restarting registration-service to apply toolchainconfig changes"
        oc -n ${HOST_NS} rollout restart deploy/registration-service
    else
        echo "skipping toolchainconfig changes - environment is not ui-e2e-tests"
    fi

    # Wait for RHDH deployment to be ready
    oc -n ${DEVSANDBOX_DASHBOARD_NS} rollout status deploy/rhdh --timeout 5m
    echo "Developer Sandbox Dashboard running at ${RHDH}"
fi
