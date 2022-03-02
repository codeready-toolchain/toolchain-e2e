package e2e

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomaticClusterAssignment(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait1 := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	// TODO: we need to create some users to be able to limit the capacity of the clusters. The code won't be needed as soon as we start counting Spaces instead of UserAccounts https://issues.redhat.com/browse/CRT-1427
	_, mur := NewSignupRequest(t, awaitilities).
		Username("for-member1").
		Email("for-member1@redhat.com").
		TargetCluster(memberAwait1).
		ManuallyApprove().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		EnsureMUR().
		Execute().
		Resources()
	NewSignupRequest(t, awaitilities).
		Username("for-member2").
		Email("for-member2@redhat.com").
		TargetCluster(memberAwait2).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		ManuallyApprove().
		EnsureMUR().
		Execute()
	hostAwait.UpdateToolchainConfig(testconfig.Tiers().DefaultSpaceTier("appstudio"))

	t.Run("set low capacity threshold and expect that space will have default tier, but won't have target cluster so it won't be provisioned", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().ResourceCapacityThreshold(1))

		// when
		space, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithTierName(""))

		// then
		space = waitUntilSpaceIsPendingCluster(t, hostAwait, space.Name)
		assert.Equal(t, "appstudio", space.Spec.TierName)

		t.Run("reset the threshold and expect the space will be have the targetCluster set and will be also provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().ResourceCapacityThreshold(80))

			// then
			VerifyResourcesProvisionedForSpace(t, awaitilities, space)
		})
	})

	t.Run("set low max number of users and expect that space won't be provisioned but added on waiting list", func(t *testing.T) {
		// given
		toolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
		require.NoError(t, err)
		originalMursPerDomainCount := toolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().MaxNumberOfUsers(originalMursPerDomainCount["internal"] + originalMursPerDomainCount["external"]))

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-waitinglist1"))

		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		space2, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-waitinglist2"))

		// then
		waitUntilSpaceIsPendingCluster(t, hostAwait, space1.Name)
		waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)

		t.Run("increment the max number of users and expect the both of the spaces will be provisioned. When we count the spaces, then this test will change", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().MaxNumberOfUsers(originalMursPerDomainCount["internal"] + originalMursPerDomainCount["external"] + 1))

			// then
			VerifyResourcesProvisionedForSpace(t, awaitilities, space1)
			VerifyResourcesProvisionedForSpace(t, awaitilities, space2)
			// TODO: when we count the number of provisioned spaces, then the second space won't be provisioned immediately https://issues.redhat.com/browse/CRT-1427
			// waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)
			//
			//t.Run("reset the max number and expect the second space will be provisioned as well", func(t *testing.T) {
			//	// when
			//	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().MaxNumberOfUsers(1000))
			//
			//	// then
			//	VerifyResourcesProvisionedForSpace(t, awaitilities, space2)
			//})
		})
	})

	t.Run("mark the first member cluster as full and for the second keep some capacity - expect that the space will be provisioned to the second one", func(t *testing.T) {
		// given
		var memberLimits []testconfig.PerMemberClusterOptionInt
		toolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
		require.NoError(t, err)
		for _, m := range toolchainStatus.Status.Members {
			if memberAwait1.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait1.ClusterName, m.UserAccountCount))
			} else if memberAwait2.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait2.ClusterName, m.UserAccountCount+1))
			}
		}
		require.Len(t, memberLimits, 2)

		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().MaxNumberOfUsers(0, memberLimits...))

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-multimember-1"))

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space1, wait.UntilSpaceHasStatusTargetCluster(memberAwait2.ClusterName))

		t.Run("after both members marking as full then the new space won't be provisioned", func(t *testing.T) {
			// given
			var memberLimits []testconfig.PerMemberClusterOptionInt
			for _, m := range toolchainStatus.Status.Members {
				if memberAwait1.ClusterName == m.ClusterName {
					memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait1.ClusterName, m.UserAccountCount))
				} else if memberAwait2.ClusterName == m.ClusterName {
					memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait2.ClusterName, m.UserAccountCount))
				}
			}
			require.Len(t, memberLimits, 2)
			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().MaxNumberOfUsers(0, memberLimits...))

			// when
			space2, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-multimember-2"))

			// then
			waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)

			t.Run("when target cluster is set manually, then the limits will be ignored", func(t *testing.T) {
				// when & then
				space3, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-multimember-3"), WithTargetCluster(memberAwait1))
				VerifyResourcesProvisionedForSpace(t, awaitilities, space3)
				// and still
				waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)
			})
		})
	})
}

func waitUntilSpaceIsPendingCluster(t *testing.T, hostAwait *wait.HostAwaitility, name string) *toolchainv1alpha1.Space {
	space, err := hostAwait.WaitForSpace(name,
		wait.UntilSpaceHasTier("appstudio"),
		wait.UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValuePending),
		wait.UntilSpaceHasConditionForTime(ProvisioningPending("unspecified target member cluster"), time.Second))
	require.NoError(t, err)
	return space
}
