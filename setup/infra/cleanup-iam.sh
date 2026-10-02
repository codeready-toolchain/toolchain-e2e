#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CREDENTIALS_FILE="${SCRIPT_DIR}/aws-credentials"

IAM_USER_NAME="openshift-installer"

if ! aws sts get-caller-identity &>/dev/null; then
  echo "ERROR: AWS credentials not configured or expired"
  echo "Configure credentials via: aws configure, AWS env vars, or aws sso login"
  exit 1
fi

if ! aws iam get-user --user-name "${IAM_USER_NAME}" &>/dev/null 2>&1; then
  echo "IAM user '${IAM_USER_NAME}' does not exist. Nothing to clean up."
  rm -f "${CREDENTIALS_FILE}"
  exit 0
fi

echo "Deleting access keys for '${IAM_USER_NAME}'..."
ACCESS_KEY_IDS=$(aws iam list-access-keys --user-name "${IAM_USER_NAME}" --query 'AccessKeyMetadata[].AccessKeyId' --output text)
for key_id in ${ACCESS_KEY_IDS}; do
  echo "  Deleting key: ${key_id}"
  aws iam delete-access-key --user-name "${IAM_USER_NAME}" --access-key-id "${key_id}"
done

echo "Detaching policies..."
POLICIES=$(aws iam list-attached-user-policies --user-name "${IAM_USER_NAME}" --query 'AttachedPolicies[].PolicyArn' --output text)
for policy_arn in ${POLICIES}; do
  echo "  Detaching: ${policy_arn}"
  aws iam detach-user-policy --user-name "${IAM_USER_NAME}" --policy-arn "${policy_arn}"
done

echo "Deleting IAM user '${IAM_USER_NAME}'..."
aws iam delete-user --user-name "${IAM_USER_NAME}"

rm -f "${CREDENTIALS_FILE}"

echo ""
echo "=== IAM User Removed ==="
echo "  User '${IAM_USER_NAME}' and credentials file deleted."