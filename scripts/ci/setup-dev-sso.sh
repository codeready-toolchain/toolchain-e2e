#!/usr/bin/env bash

user_help() {
    echo "Deploy in cluster keycloak and configure registration service to use it."
    echo "options:"
    echo "-sn, --sso-ns  namespace where the SSO provider will be installed"
    echo "-r,  --rosa-cluster      The name of the target ROSA cluster (optional). Use this flag to properly configure OAuth on ROSA Classic clusters. Requires Rosa CLI to be installed."
    echo "-h,  --help              To show this help text"
    echo ""
}

read_arguments() {
    if [[ $# -lt 2 ]]
    then
        echo "There are missing parameters"
        user_help
    fi

    while test $# -gt 0; do
           case "$1" in
                -h|--help)
                    user_help
                    exit 0
                    ;;
                -sn|--sso-ns)
                    shift
                    DEV_SSO_NS=$1
                    shift
                    ;;
                -r|--rosa-cluster)
                    shift
                    ROSA_CLUSTER_NAME="$1"
                    shift
                    ;;
                *)
                   echo "$1 is not a recognized flag!" >> /dev/stderr
                   user_help
                   exit 1
                   ;;
          esac
    done

    [[ -n "${DEV_SSO_NS}" ]] || { printf "SSO namespace is required\n"; user_help; exit 1; }
}

check_commands()
{
    for cmd in "$@"
    do
        check_command "$cmd"
    done
}

check_command()
{
    command -v "$1" > /dev/null && return 0

    printf "please install '%s' before running this script\n" "$1"
    exit 1
}

run_wait_until_is_installed() {
    WAIT_UNTIL_IS_INSTALLED=scripts/ci/wait-until-is-installed.sh
    PARAMS="-crd keycloak.org -cs '' -n ${DEV_SSO_NS} -s ${SUBSCRIPTION_NAME}"
    SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
    SCRIPT_NAME=$(basename ${WAIT_UNTIL_IS_INSTALLED})

    if [[ -f ${SCRIPT_DIR}/${SCRIPT_NAME} ]]; then
        ${SCRIPT_DIR}/${SCRIPT_NAME} ${PARAMS}
    else
        curl -sSL https://raw.githubusercontent.com/${OWNER_AND_BRANCH_LOCATION}/${WAIT_UNTIL_IS_INSTALLED} > /tmp/${SCRIPT_NAME} && chmod +x /tmp/${SCRIPT_NAME} && OWNER_AND_BRANCH_LOCATION=${OWNER_AND_BRANCH_LOCATION} /tmp/${SCRIPT_NAME} ${PARAMS}
    fi
}

setup_oauth_generic()
{
  set -e -o pipefail

  RHSSO_URL="$1"

  printf "configuring OAuth authentication for keycloak '%s'\n" "${RHSSO_URL}"
  KEYCLOAK_SECRET="$2" envsubst < "dev-sso/openid-secret.yaml" | oc apply -f -

  # Certificate used by keycloak is self-signed, we need to import and grant for it
  printf "creating configmap with keycloak certificates\n"
  oc get secrets -n openshift-ingress-operator router-ca -o jsonpath='{.data.tls\.crt}' | base64 -d > /tmp/ca.crt
  oc create configmap ca-config-map --from-file="ca.crt=/tmp/ca.crt" -n openshift-config || true

  printf "applying patch for oauths configuration\n"

  oc patch oauths.config.openshift.io/cluster --type=merge --patch-file=/dev/stdin << EOF
spec:
  identityProviders:
  - mappingMethod: lookup
    name: rhd
    openID:
      ca:
        name: ca-config-map
      claims:
        preferredUsername:
        - preferred_username
        email:
        - email
        name:
        - username
      clientID: kubesaw
      clientSecret:
        name: openid-client-secret-kubesaw
      issuer: ${RHSSO_URL}/auth/realms/kubesaw-dev
    type: OpenID
EOF
}

setup_oauth_rosa()
{
  ISSUER_URL="$1/auth/realms/kubesaw-dev"
  KEYCLOAK_SECRET="$2"
  ROSA_CLUSTER_NAME="$3"

  printf "Setting up OAuth in ROSA cluster '%s' for issuer '%s'\n" "${ROSA_CLUSTER_NAME}" "${ISSUER_URL}"
  rosa create idp \
    --cluster="${ROSA_CLUSTER_NAME}" \
    --type='openid' \
    --client-id='kubesaw' \
    --client-secret="${KEYCLOAK_SECRET}" \
    --mapping-method='lookup' \
    --issuer-url="${ISSUER_URL}" \
    --email-claims='email' \
    --name-claims='username' \
    --username-claims='username' \
    --name='rhd' \
    --yes
}

setup_oauth()
{
  set -e

  if [[ -n "$3" ]]; then
    setup_oauth_rosa "$1" "$2" "$3"
  else
    setup_oauth_generic "$1" "$2"
  fi
}

read_arguments "$@"

set -e


check_commands oc base64 openssl
[ -n "${ROSA_CLUSTER_NAME}" ] && check_commands rosa

parent_path=$(readlink -f "$(dirname "${BASH_SOURCE[0]}")")
cd "$parent_path"

printf "creating %s namespace\n" "${DEV_SSO_NS}"
DEV_SSO_NS=${DEV_SSO_NS} envsubst < "dev-sso/namespace.yaml" | oc apply -f -

# Install rhsso operator
SUBSCRIPTION_NAME=${DEV_SSO_NS}
printf "installing RH SSO operator\n"
DEV_SSO_NS=${DEV_SSO_NS} SUBSCRIPTION_NAME=${SUBSCRIPTION_NAME} envsubst < "dev-sso/rhsso-operator.yaml" | oc apply -f -

run_wait_until_is_installed

printf "installing dev Keycloak in namespace %s\n" "${DEV_SSO_NS}"
KEYCLOAK_SECRET=$(openssl rand -base64 32)
export KEYCLOAK_SECRET
DEV_SSO_NS=${DEV_SSO_NS} KEYCLOAK_SECRET=${KEYCLOAK_SECRET} envsubst < "dev-sso/keycloak.yaml" | oc apply -f -

while ! oc get statefulset -n "${DEV_SSO_NS}" keycloak &> /dev/null ; do
    printf "waiting for keycloak statefulset in %s to exist...\n" "${DEV_SSO_NS}"
    sleep 10
done

printf "waiting for keycloak in %s to be ready...\n" "${DEV_SSO_NS}"
TIMEOUT=200s
oc wait --for=jsonpath='{.status.ready}'=true keycloak/kubesaw-dev -n "${DEV_SSO_NS}" --timeout "${TIMEOUT}"  || \
{
  oc get keycloak kubesaw-dev -n ${DEV_SSO_NS} -o yaml && exit 1
}

BASE_URL=$(oc get ingresses.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
RHSSO_URL="https://keycloak-${DEV_SSO_NS}.$BASE_URL"

oc rollout status statefulset -n ${DEV_SSO_NS} keycloak --timeout 20m

printf "Setup OAuth\n"
setup_oauth "${RHSSO_URL}" "${KEYCLOAK_SECRET}" "${ROSA_CLUSTER_NAME}"

## Configure toolchain to use the internal keycloak
printf "patching toolchainconfig"
oc patch ToolchainConfig/config -n toolchain-host-operator --type=merge --patch-file=/dev/stdin << EOF
spec:
  host:
    registrationService:
      auth:
        authClientConfigRaw: '{
                  "realm": "kubesaw-dev",
                  "auth-server-url": "$RHSSO_URL/auth",
                  "ssl-required": "none",
                  "resource": "kubesaw-public",
                  "clientId": "kubesaw-public",
                  "public-client": true,
                  "confidential-port": 0
                }'
        authClientLibraryURL: $RHSSO_URL/auth/js/keycloak.js
        authClientPublicKeysURL: $RHSSO_URL/auth/realms/kubesaw-dev/protocol/openid-connect/certs
        ssoBaseURL: $RHSSO_URL
        ssoRealm: kubesaw-dev
EOF

# Restart the registration-service to ensure the new configuration is used
oc delete pods -n toolchain-host-operator --selector=name=registration-service

KEYCLOAK_ADMIN_PASSWORD=$(oc get secrets -n ${DEV_SSO_NS} credential-kubesaw-dev -o jsonpath='{.data.ADMIN_PASSWORD}' | base64 -d)
printf "================================================= DEV SSO ACCESS ==============================================================================================\n"
printf "to login into keycloak use user 'admin' and password '%s' at '%s/auth'\n" "${KEYCLOAK_ADMIN_PASSWORD}" "${RHSSO_URL}"
printf "use user 'user1@user.us' with password 'user1' to login at 'https://registration-service-toolchain-host-operator.%s'\n" "${BASE_URL}"
printf "================================================================================================================================================================\n"
