package testsupport

import (
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

func VerifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility, expectedURL string) {
	err := memberAwait.WaitForMemberStatus(
		wait.UntilMemberStatusHasConditions(ToolchainStatusReady()),
		wait.UntilMemberStatusHasUsageSet(),
		wait.UntilMemberStatusHasConsoleURLSet(expectedURL, RoutesAvailable()))
	require.NoError(t, err, "failed while waiting for MemberStatus")
}

func VerifyToolchainStatus(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility) {
	memberCluster, found, err := hostAwait.GetToolchainCluster(cluster.Member, memberAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, found)
	_, err = hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
		wait.UntilAllMembersHaveUsageSet(),
		wait.UntilAllMembersHaveAPIEndpoint(memberCluster.Spec.APIEndpoint))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
}

func VerifyIncreaseOfUserAccountCount(t *testing.T, previous, current *toolchainv1alpha1.ToolchainStatus, memberClusterName string, increase int) {
	found := false
CurrentMembers:
	for _, currentMemberStatus := range current.Status.Members {
		for _, previousMemberStatus := range previous.Status.Members {
			if previousMemberStatus.ClusterName == currentMemberStatus.ClusterName {
				if currentMemberStatus.ClusterName == memberClusterName {
					assert.Equal(t, previousMemberStatus.UserAccountCount+increase, currentMemberStatus.UserAccountCount)
					found = true
				}
				continue CurrentMembers
			}
		}
		if currentMemberStatus.ClusterName == memberClusterName {
			assert.Equal(t, increase, currentMemberStatus.UserAccountCount)
			found = true
		}
	}
	assert.True(t, found, "There is a missing UserAccount count for member cluster '%s'", memberClusterName)
}
