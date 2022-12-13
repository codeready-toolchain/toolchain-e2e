package testsupport

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
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

func VerifyIncreaseOfSpaceCount(t *testing.T, previous, current *toolchainv1alpha1.ToolchainStatus, memberClusterName string, increase int) {
	found := false
CurrentMembers:
	for _, currentMemberStatus := range current.Status.Members {
		for _, previousMemberStatus := range previous.Status.Members {
			if previousMemberStatus.ClusterName == currentMemberStatus.ClusterName {
				if currentMemberStatus.ClusterName == memberClusterName {
					assert.Equal(t, previousMemberStatus.SpaceCount+increase, currentMemberStatus.SpaceCount)
					found = true
				}
				continue CurrentMembers
			}
		}
		if currentMemberStatus.ClusterName == memberClusterName {
			assert.Equal(t, increase, currentMemberStatus.SpaceCount)
			found = true
		}
	}
	assert.True(t, found, "There is a missing Space count for member cluster '%s'", memberClusterName)
}
