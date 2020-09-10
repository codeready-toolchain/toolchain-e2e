package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/require"
)

func VerifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility) {
	err := memberAwait.WaitForMemberStatus(wait.UntilMemberStatusHasConditions(ToolchainStatusReady()), wait.UntilMemberStatusHasUsageSet())
	require.NoError(t, err, "failed while waiting for MemberStatus")
}

func VerifyToolchainStatus(t *testing.T, hostAwait *wait.HostAwaitility) {
	err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReady()), wait.UntilAllMembersHaveUsageSet())
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
}
