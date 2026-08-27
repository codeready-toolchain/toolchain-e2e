#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/config"
CREDENTIALS_FILE="${SCRIPT_DIR}/aws-credentials"

IAM_USER_NAME="openshift-installer"

if [[ -f "${CREDENTIALS_FILE}" ]]; then
  echo "ERROR: ${CREDENTIALS_FILE} already exists."
  echo "If you need to recreate the IAM user, run ./cleanup-iam.sh first."
  exit 1
fi

if ! aws sts get-caller-identity &>/dev/null; then
  echo "ERROR: AWS credentials not configured or expired"
  echo "Configure credentials via: aws configure, AWS env vars, or aws sso login"
  exit 1
fi

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
echo "AWS Account: ${ACCOUNT_ID}"

if aws iam get-user --user-name "${IAM_USER_NAME}" &>/dev/null; then
  echo "ERROR: IAM user '${IAM_USER_NAME}' already exists in account ${ACCOUNT_ID}"
  echo "Run ./cleanup-iam.sh to remove it, or use the existing credentials."
  exit 1
fi

echo "Creating IAM user '${IAM_USER_NAME}'..."
aws iam create-user --user-name "${IAM_USER_NAME}" --output text --query 'User.Arn'

echo "Attaching AdministratorAccess policy..."
aws iam attach-user-policy \
  --user-name "${IAM_USER_NAME}" \
  --policy-arn "arn:aws:iam::aws:policy/AdministratorAccess"

echo "Creating access keys..."
KEYS_JSON=$(aws iam create-access-key --user-name "${IAM_USER_NAME}" --output json)

ACCESS_KEY_ID=$(echo "${KEYS_JSON}" | python3 -c "import sys,json; print(json.load(sys.stdin)['AccessKey']['AccessKeyId'])")
SECRET_ACCESS_KEY=$(echo "${KEYS_JSON}" | python3 -c "import sys,json; print(json.load(sys.stdin)['AccessKey']['SecretAccessKey'])")

cat > "${CREDENTIALS_FILE}" <<EOF
# Static AWS credentials for openshift-install.
# Created by setup-iam.sh — this file is gitignored.
# To remove the IAM user and this file, run: ./cleanup-iam.sh
export AWS_ACCESS_KEY_ID="${ACCESS_KEY_ID}"
export AWS_SECRET_ACCESS_KEY="${SECRET_ACCESS_KEY}"
unset AWS_SESSION_TOKEN
EOF

chmod 600 "${CREDENTIALS_FILE}"

echo ""
echo "=== IAM User Created ==="
echo "  User:             ${IAM_USER_NAME}"
echo "  Access Key ID:    ${ACCESS_KEY_ID}"
echo "  Credentials file: ${CREDENTIALS_FILE}"
echo ""
echo "create-cluster.sh and destroy-cluster.sh will source this file automatically."