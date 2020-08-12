package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/require"
)

func VerifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility) {
	_, err := memberAwait.WaitForMemberStatus(wait.UntilMemberStatusHasConditions(ToolchainStatusReady()))
	require.NoError(t, err, "failed while waiting for MemberStatus")
}

func VerifyToolchainStatus(t *testing.T, hostAwait *wait.HostAwaitility) {
	_, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReady()))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
}
