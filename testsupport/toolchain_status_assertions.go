package testsupport

import (
	"fmt"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

func VerifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility) {
	err := memberAwait.WaitForMemberStatus(wait.UntilMemberStatusHasConditions(ToolchainStatusReady()))
	require.NoError(t, err, "failed while waiting for MemberStatus")
}

func VerifyToolchainStatus(t *testing.T, hostAwait *wait.HostAwaitility) {
	_, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReady()))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
}

func VerifyIncreaseOfUserAccountCount(t *testing.T, previous, current *v1alpha1.ToolchainStatus, memberClusterName string, increase int) {
	found := false
CurrentMembers:
	for _, currentMemberStatus := range current.Status.Members {
		for _, previousMemberStatus := range previous.Status.Members {
			if previousMemberStatus.ClusterName == currentMemberStatus.ClusterName {
				if currentMemberStatus.ClusterName == memberClusterName {
					assert.Equal(t, previousMemberStatus.CapacityUsage.UserAccountCount+increase, currentMemberStatus.CapacityUsage.UserAccountCount)
					found = true
				} else {
					assert.Equal(t, previousMemberStatus.CapacityUsage.UserAccountCount, currentMemberStatus.CapacityUsage.UserAccountCount)
				}
				continue CurrentMembers
			}
		}
		if currentMemberStatus.ClusterName == memberClusterName {
			assert.Equal(t, increase, currentMemberStatus.CapacityUsage.UserAccountCount)
			found = true
		} else {
			assert.Fail(t, fmt.Sprintf("There is an extra UserAccount count for member cluster %s", currentMemberStatus.ClusterName))
		}
	}
	assert.True(t, found, "There is missing UserAccount count for member cluster")
}
