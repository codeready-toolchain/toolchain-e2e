package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func VerifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility, expectedURL string) {
	memberAwait.WaitForMemberStatus(t,
		wait.UntilMemberStatusHasConditions(ToolchainStatusReady()),
		wait.UntilMemberStatusHasUsageSet(),
		wait.UntilMemberStatusHasConsoleURLSet(expectedURL, RoutesAvailable()))
}

func VerifyToolchainStatus(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility) {
	memberCluster, found := hostAwait.GetToolchainCluster(t, cluster.Member, memberAwait.Namespace, nil)
	require.True(t, found)
	hostAwait.WaitForToolchainStatus(t,
		wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
		wait.UntilAllMembersHaveUsageSet(),
		wait.UntilAllMembersHaveAPIEndpoint(memberCluster.Spec.APIEndpoint),
		wait.UntilProxyURLIsPresent(hostAwait.APIProxyURL))
}
