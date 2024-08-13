package e2e

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	testSpc "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
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
	user := NewSignupRequest(awaitilities).
		Username("for-member1").
		Email("for-member1@redhat.com").
		TargetCluster(memberAwait1).
		ManuallyApprove().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		EnsureMUR().
		Execute(t)
	mur := user.MUR
	NewSignupRequest(awaitilities).
		Username("for-member2").
		Email("for-member2@redhat.com").
		TargetCluster(memberAwait2).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		ManuallyApprove().
		EnsureMUR().
		Execute(t)
	hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultSpaceTier("appstudio"))

	t.Run("set low max number of spaces and expect that space won't be provisioned but added on waiting list", func(t *testing.T) {
		// given
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.Enabled(false))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.Enabled(false))
		// some short time to get the cache populated with the change
		// sometimes the ToolchainConfig doesn't have the new values in the CapacityThresholds section before the creation of Spaces is issued
		// so Spaces were still created while Capacity was updated with the above values.
		time.Sleep(1 * time.Second)

		// when
		space1, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-waitinglist1"), testspace.WithTierName("appstudio"))

		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		space2, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-waitinglist2"), testspace.WithTierName("appstudio"))

		// then
		waitUntilSpaceIsPendingCluster(t, hostAwait, space1.Name)
		waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)

		t.Run("increment the max number of spaces and expect that first space will be provisioned.", func(t *testing.T) {
			// when
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(2), testSpc.Enabled(true))
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.Enabled(false))

			// then
			VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name)
			// the second space won't be provisioned immediately
			waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)
			t.Run("reset the max number and expect the second space will be provisioned as well", func(t *testing.T) {
				// when
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500), testSpc.Enabled(true))
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(500), testSpc.Enabled(true))

				// then
				VerifyResourcesProvisionedForSpace(t, awaitilities, space2.Name)
			})
		})
	})

	t.Run("set low capacity threshold and expect that space will have default tier, but won't have target cluster so it won't be provisioned", func(t *testing.T) {
		// given
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(1))
		// some short time to get the cache populated with the change
		time.Sleep(1 * time.Second)

		// when
		space, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithTierName(""))

		// then
		space = waitUntilSpaceIsPendingCluster(t, hostAwait, space.Name)
		assert.Equal(t, "appstudio", space.Spec.TierName)

		t.Run("reset the threshold and expect the space will be have the targetCluster set and will be also provisioned", func(t *testing.T) {
			// when
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(80))
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(80))

			// then
			VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
		})
	})

	t.Run("mark the first member cluster as full and for the second keep some capacity - expect that the space will be provisioned to the second one", func(t *testing.T) {
		// given
		toolchainStatus, err := hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(wait.ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
			wait.UntilToolchainStatusUpdatedAfter(time.Now()))
		require.NoError(t, err)
		for _, m := range toolchainStatus.Status.Members {
			if memberAwait1.ClusterName == m.ClusterName {
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(uint(m.SpaceCount)))
			} else if memberAwait2.ClusterName == m.ClusterName {
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(uint(m.SpaceCount+1)))
			}
		}

		// when
		space1, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-multimember-1"), testspace.WithTierName("appstudio"))

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait2.ClusterName))

		t.Run("after both members marking as full then the new space won't be provisioned", func(t *testing.T) {
			// given
			for _, m := range toolchainStatus.Status.Members {
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, m.ClusterName, testSpc.MaxNumberOfSpaces(uint(m.SpaceCount)))
			}

			// when
			space2, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-multimember-2"), testspace.WithTierName("appstudio"))

			// then
			waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)

			t.Run("when target cluster is set manually, then the limits will be ignored", func(t *testing.T) {
				// when & then
				space3, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-multimember-3"), testspace.WithSpecTargetCluster(memberAwait1.ClusterName))
				VerifyResourcesProvisionedForSpace(t, awaitilities, space3.Name)
				// and still
				waitUntilSpaceIsPendingCluster(t, hostAwait, space2.Name)
			})
		})
	})

	t.Run("set cluster-role label only on member2 cluster and expect it will be selected", func(t *testing.T) {
		// given
		// both cluster have room for more spaces ...
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(500), testSpc.WithPlacementRoles(testSpc.PlacementRole("workspace")))

		// when
		space1, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-clusterole-tenant"),
			testspace.WithTierName("appstudio"),
			testspace.WithSpecTargetClusterRoles([]string{cluster.RoleLabel("workspace")})) // request that specific cluster role

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space1.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait2.ClusterName))
	})

	t.Run("set cluster-role label only on member2 cluster but mark it as full so that no cluster will be available", func(t *testing.T) {
		// given
		// only member1 as room for spaces
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.Enabled(false), testSpc.WithPlacementRoles(testSpc.PlacementRole("workspace")))

		// when
		space1, _ := createSpaceWithAdminBinding(t, awaitilities, mur, testspace.WithName("space-clusterole-tenant-pending"),
			testspace.WithTierName("appstudio"),
			testspace.WithSpecTargetClusterRoles([]string{cluster.RoleLabel("workspace")})) // request that specific cluster role

		// then
		waitUntilSpaceIsPendingCluster(t, hostAwait, space1.Name)
	})

	t.Run("provision space on the required cluster even if it doesn't match the specified cluster-role", func(t *testing.T) {
		// given
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(500), testSpc.WithPlacementRoles(testSpc.PlacementRole("workspace")))

		// when
		space1, _ := createSpaceWithAdminBinding(t, awaitilities, mur,
			testspace.WithName("space-required-tenant"),
			testspace.WithTierName("appstudio"),
			testspace.WithSpecTargetClusterRoles([]string{cluster.RoleLabel("workspace")}),
			testspace.WithSpecTargetCluster(memberAwait1.ClusterName)) // request that specific cluster role and preferred cluster which will have priority on the roles

		// then
		// space should end up on the required cluster even if it doesn't have the specified cluster roles
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

// createSpaceWithAdminBinding initializes a new Space object using the NewSpace function, and then creates it in the cluster
// It also automatically creates SpaceBinding with the given role for it and for the given MasterUserRecord
func createSpaceWithAdminBinding(t *testing.T, awaitilities wait.Awaitilities, mur *toolchainv1alpha1.MasterUserRecord, opts ...testspace.Option) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBinding) {
	space := testspace.NewSpaceWithGeneratedName(awaitilities.Host().Namespace, util.NewObjectNamePrefix(t), opts...)
	space, spaceBinding, err := awaitilities.Host().CreateSpaceAndSpaceBinding(t, mur, space, "admin")
	require.NoError(t, err)

	return space, spaceBinding
}
