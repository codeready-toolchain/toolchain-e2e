package testsupport

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func VerifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility, expectedURL string) {
	err := memberAwait.WaitForMemberStatus(t,
		wait.UntilMemberStatusHasConditions(wait.ToolchainStatusReady()),
		wait.UntilMemberStatusHasUsageSet(),
		wait.UntilMemberStatusHasConsoleURLSet(expectedURL, wait.RoutesAvailable()))
	require.NoError(t, err, "failed while waiting for MemberStatus")
}

func VerifyToolchainStatus(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility) {
	memberCluster, found, err := hostAwait.GetToolchainCluster(t, memberAwait.Namespace, toolchainv1alpha1.ConditionReady)
	require.NoError(t, err)
	require.True(t, found)
	_, err = hostAwait.WaitForToolchainStatus(t, wait.UntilToolchainStatusHasConditions(wait.ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
		wait.UntilAllMembersHaveUsageSet(),
		wait.UntilAllMembersHaveAPIEndpoint(memberCluster.Status.APIEndpoint),
		wait.UntilProxyURLIsPresent(hostAwait.APIProxyURL))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
}
