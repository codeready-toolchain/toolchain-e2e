package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func VerifyToolchainConfig(t *testing.T, hostAwait *wait.HostAwaitility, criteria ...wait.ToolchainConfigWaitCriterion) {
	_, err := hostAwait.WaitForToolchainConfig(criteria...)
	require.NoError(t, err, "failed while waiting for ToolchainConfig to meet the required criteria")
}

func VerifyMemberOperatorConfig(t *testing.T, memberAwait *wait.MemberAwaitility, criteria ...wait.MemberOperatorConfigWaitCriterion) {
	_, err := memberAwait.WaitForMemberOperatorConfig(criteria...)
	require.NoError(t, err, "failed while waiting for MemberOperatorConfig to meet the required criteria")
}
