package e2e

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestAutomaticClusterAssignment(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait1 := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	_, mur := NewSignupRequest(awaitilities).
		Username("for-member1").
		Email("for-member1@redhat.com").
		TargetCluster(memberAwait1).
		ManuallyApprove().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		EnsureMUR().
		Execute(t).
		Resources()
	NewSignupRequest(awaitilities).
		Username("for-member2").
		Email("for-member2@redhat.com").
		TargetCluster(memberAwait2).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		ManuallyApprove().
		EnsureMUR().
		Execute(t)
	hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultSpaceTier("appstudio"))

	t.Run("set low max number of spaces and expect that space won't be provisioned but added on waiting list", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t,
			testconfig.CapacityThresholds().
				MaxNumberOfSpaces(
					testconfig.PerMemberCluster(memberAwait1.ClusterName, -1),
					testconfig.PerMemberCluster(memberAwait2.ClusterName, -1),
				),
		)
		// some short time to get the cache populated with the change
		time.Sleep(1 * time.Second)

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-waitinglist1"))

		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		space2, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-waitinglist2"))

		// then
		waitUntilSpaceIsPendingCluster(t, hostAwait, space1.Name)
		waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)

		t.Run("increment the max number of spaces and expect that first space will be provisioned.", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(t,
				testconfig.CapacityThresholds().
					MaxNumberOfSpaces(
						// increment max spaces only on member1
						testconfig.PerMemberCluster(memberAwait1.ClusterName, 2),
						testconfig.PerMemberCluster(memberAwait2.ClusterName, -1),
					),
			)

			// then
			VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name)
			// the second space won't be provisioned immediately
			waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)
			t.Run("reset the max number and expect the second space will be provisioned as well", func(t *testing.T) {
				// when
				hostAwait.UpdateToolchainConfig(t,
					testconfig.CapacityThresholds().
						MaxNumberOfSpaces(
							testconfig.PerMemberCluster(memberAwait1.ClusterName, 500),
							testconfig.PerMemberCluster(memberAwait2.ClusterName, 500),
						),
				)

				// then
				VerifyResourcesProvisionedForSpace(t, awaitilities, space2.Name)
			})
		})
	})

	t.Run("set low capacity threshold and expect that space will have default tier, but won't have target cluster so it won't be provisioned", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t,
			testconfig.CapacityThresholds().ResourceCapacityThreshold(1),
		)
		// some short time to get the cache populated with the change
		time.Sleep(1 * time.Second)

		// when
		space, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithTierName(""))

		// then
		space = waitUntilSpaceIsPendingCluster(t, hostAwait, space.Name)
		assert.Equal(t, "appstudio", space.Spec.TierName)

		t.Run("reset the threshold and expect the space will be have the targetCluster set and will be also provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(t, testconfig.CapacityThresholds().ResourceCapacityThreshold(80))

			// then
			VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
		})
	})

	t.Run("mark the first member cluster as full and for the second keep some capacity - expect that the space will be provisioned to the second one", func(t *testing.T) {
		// given
		var memberLimits []testconfig.PerMemberClusterOptionInt
		toolchainStatus, err := hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
			wait.UntilToolchainStatusUpdatedAfter(time.Now()))
		require.NoError(t, err)
		for _, m := range toolchainStatus.Status.Members {
			if memberAwait1.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait1.ClusterName, m.SpaceCount))
			} else if memberAwait2.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait2.ClusterName, m.SpaceCount+1))
			}
		}
		require.Len(t, memberLimits, 2)

		hostAwait.UpdateToolchainConfig(t, testconfig.CapacityThresholds().MaxNumberOfSpaces(memberLimits...))

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-multimember-1"))

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait2.ClusterName))

		t.Run("after both members marking as full then the new space won't be provisioned", func(t *testing.T) {
			// given
			var memberLimits []testconfig.PerMemberClusterOptionInt
			for _, m := range toolchainStatus.Status.Members {
				if memberAwait1.ClusterName == m.ClusterName {
					memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait1.ClusterName, m.SpaceCount))
				} else if memberAwait2.ClusterName == m.ClusterName {
					memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait2.ClusterName, m.SpaceCount))
				}
			}
			require.Len(t, memberLimits, 2)
			hostAwait.UpdateToolchainConfig(t, testconfig.CapacityThresholds().MaxNumberOfSpaces(memberLimits...))

			// when
			space2, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-multimember-2"))

			// then
			waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)

			t.Run("when target cluster is set manually, then the limits will be ignored", func(t *testing.T) {
				// when & then
				space3, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-multimember-3"), WithTargetCluster(memberAwait1.ClusterName))
				VerifyResourcesProvisionedForSpace(t, awaitilities, space3.Name)
				// and still
				waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)
			})
		})
	})

	t.Run("set cluster-role label only on member2 cluster and expect it will be selected", func(t *testing.T) {
		// given
		// both cluster have room for more spaces ...
		hostAwait.UpdateToolchainConfig(t, testconfig.CapacityThresholds().MaxNumberOfSpaces(
			testconfig.PerMemberCluster(memberAwait1.ClusterName, 500),
			testconfig.PerMemberCluster(memberAwait2.ClusterName, 500),
		))
		// let's add a custom cluster-role for member2
		memberCluster2, found, err := hostAwait.GetToolchainCluster(t, memberAwait2.Type, memberAwait2.Namespace, nil)
		require.NoError(t, err)
		require.True(t, found)
		_, err = hostAwait.UpdateToolchainCluster(t, memberCluster2.Name, func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Labels[cluster.RoleLabel("workspace")] = "" // add a new cluster-role label, the value is blank since only key matters.
		})
		require.NoError(t, err)

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-clusterole-tenant"),
			WithTargetClusterRoles([]string{cluster.RoleLabel("workspace")})) // request that specific cluster role

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait2.ClusterName))
	})

	t.Run("set cluster-role label only on member2 cluster but mark it as full so that no cluster will be available", func(t *testing.T) {
		// given
		// only member1 as room for spaces
		hostAwait.UpdateToolchainConfig(t, testconfig.CapacityThresholds().MaxNumberOfSpaces(
			testconfig.PerMemberCluster(memberAwait1.ClusterName, 500), // member1 has more room
			testconfig.PerMemberCluster(memberAwait2.ClusterName, -1),  // member2 is full
		))
		// let's add a custom cluster-role for member2
		memberCluster2, found, err := hostAwait.GetToolchainCluster(t, memberAwait2.Type, memberAwait2.Namespace, nil)
		require.NoError(t, err)
		require.True(t, found)
		_, err = hostAwait.UpdateToolchainCluster(t, memberCluster2.Name, func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Labels[cluster.RoleLabel("workspace")] = "" // add a new cluster-role label, the value is blank since only key matters.
		})
		require.NoError(t, err)

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-clusterole-tenant-pending"),
			WithTargetClusterRoles([]string{cluster.RoleLabel("workspace")})) // request that specific cluster role

		// then
		waitUntilSpaceIsPendingCluster(t, hostAwait, space1.Name)
	})

	t.Run("provision space on preferred cluster even if it doesn't match the specified cluster-role", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.CapacityThresholds().MaxNumberOfSpaces(
			testconfig.PerMemberCluster(memberAwait1.ClusterName, 500), // member1 has more room
			testconfig.PerMemberCluster(memberAwait2.ClusterName, 500), // member2 has more room as well
		))
		// let's add a custom cluster-role for member2
		memberCluster2, found, err := hostAwait.GetToolchainCluster(t, memberAwait2.Type, memberAwait2.Namespace, nil)
		require.NoError(t, err)
		require.True(t, found)
		_, err = hostAwait.UpdateToolchainCluster(t, memberCluster2.Name, func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Labels[cluster.RoleLabel("workspace")] = "" // add a new cluster-role label, the value is blank since only key matters.
		})
		require.NoError(t, err)

		// when
		space1, _ := CreateSpaceWithBinding(t, awaitilities, mur, WithName("space-preferred-tenant"),
			WithTargetClusterRoles([]string{cluster.RoleLabel("workspace")}), WithTargetCluster(memberAwait1.ClusterName)) // request that specific cluster role and preferred cluster which will have priority on the roles

		// then
		// space should end up on preferred cluster even if it doesn't have the specified cluster roles
		VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait1.ClusterName))
	})
}

func waitUntilSpaceIsPendingCluster(t *testing.T, hostAwait *wait.HostAwaitility, name string) *toolchainv1alpha1.Space {
	space, err := hostAwait.WaitForSpace(t, name,
		wait.UntilSpaceHasTier("appstudio"),
		wait.UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValuePending),
		wait.UntilSpaceHasConditionForTime(ProvisioningPending("unspecified target member cluster"), time.Second))
	require.NoError(t, err)
	return space
}

func ProvisioningPending(msg string) toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.SpaceProvisioningPendingReason,
		Message: msg,
	}
}
