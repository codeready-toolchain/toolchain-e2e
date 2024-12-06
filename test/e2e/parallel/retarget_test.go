package parallel

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func TestRetargetUserByChangingSpaceTargetClusterWhenSpaceIsNotShared(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	user := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	// when
	space, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
		Update(user.Space.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
			s.Spec.TargetCluster = member2Await.ClusterName
		})
	require.NoError(t, err)

	// then
	// expect NSTemplateSet to be deleted on member-1 cluster
	err = member1Await.WaitUntilNSTemplateSetDeleted(t, space.Name)
	require.NoError(t, err)

	// and provisioned on member-2
	_, err = member2Await.WaitForNSTmplSet(t, space.Name)
	require.NoError(t, err)
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, wait.UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
	VerifyUserRelatedResources(t, awaitilities, user.UserSignup, user.MUR.Spec.TierName, ExpectUserAccountIn(member2Await))
}

func TestRetargetUserByChangingSpaceTargetClusterWhenSpaceIsShared(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	userToShareWith1 := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		WaitForMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	murToShareWith1 := userToShareWith1.MUR

	userToShareWith2 := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member2Await).
		WaitForMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	murToShareWith2 := userToShareWith2.MUR

	user := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	ownerMur := user.MUR
	spaceToMove := user.Space

	spacebinding.CreateSpaceBinding(t, hostAwait, murToShareWith1, spaceToMove, "admin")
	spacebinding.CreateSpaceBinding(t, hostAwait, murToShareWith2, spaceToMove, "admin")

	tier, err := hostAwait.WaitForNSTemplateTier(t, spaceToMove.Spec.TierName)
	require.NoError(t, err)
	_, err = member1Await.WaitForNSTmplSet(t, spaceToMove.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
	require.NoError(t, err)

	// when
	spaceToMove, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
		Update(spaceToMove.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
			s.Spec.TargetCluster = member2Await.ClusterName
		})
	require.NoError(t, err)

	// then
	// expect NSTemplateSet to be deleted on member-1 cluster
	err = member1Await.WaitUntilNSTemplateSetDeleted(t, spaceToMove.Name)
	require.NoError(t, err)

	// and provisioned with the set of usernames for admin role as before on member-2
	_, err = member2Await.WaitForNSTmplSet(t, spaceToMove.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
	require.NoError(t, err)
	VerifyResourcesProvisionedForSpace(t, awaitilities, spaceToMove.Name, wait.UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
	VerifyUserRelatedResources(t, awaitilities, user.UserSignup, ownerMur.Spec.TierName, ExpectUserAccountIn(member2Await))

	// the move doesn't have any effect on the other signups and MURs
	VerifyUserRelatedResources(t, awaitilities, userToShareWith1.UserSignup, murToShareWith1.Spec.TierName, ExpectUserAccountIn(member1Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith1.Name, wait.UntilSpaceHasStatusTargetCluster(member1Await.ClusterName))

	VerifyUserRelatedResources(t, awaitilities, userToShareWith2.UserSignup, murToShareWith2.Spec.TierName, ExpectUserAccountIn(member2Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith2.Name, wait.UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
}

// NOTE:
// This test is just documenting the current situation/gap with SBRs and retargeting feature.
// When there are users added to a Space via SpaceBindingRequests, and the space is retargeted to different cluster, the namespace with the SBRs get deleted,
// and the SBRs are not being recreated into the new cluster as of now, causing those user to not have access to the Space anymore. However, the owner of the Space will still have access.
func TestRetargetUserWithSBRByChangingSpaceTargetClusterWhenSpaceIsShared(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	userToShareWith1 := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		WaitForMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	murToShareWith1 := userToShareWith1.MUR

	userToShareWith2 := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member2Await).
		WaitForMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	murToShareWith2 := userToShareWith2.MUR

	user := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	ownerMur := user.MUR
	spaceToMove := user.Space

	spacebinding.CreateSpaceBindingRequest(t, awaitilities, member1Await.ClusterName,
		spacebinding.WithSpecSpaceRole("admin"),
		spacebinding.WithSpecMasterUserRecord(murToShareWith1.GetName()),
		spacebinding.WithNamespace(GetDefaultNamespace(spaceToMove.Status.ProvisionedNamespaces)))
	spacebinding.CreateSpaceBindingRequest(t, awaitilities, member1Await.ClusterName,
		spacebinding.WithSpecSpaceRole("admin"),
		spacebinding.WithSpecMasterUserRecord(murToShareWith2.GetName()),
		spacebinding.WithNamespace(GetDefaultNamespace(spaceToMove.Status.ProvisionedNamespaces)))

	tier, err := hostAwait.WaitForNSTemplateTier(t, spaceToMove.Spec.TierName)
	require.NoError(t, err)
	_, err = member1Await.WaitForNSTmplSet(t, spaceToMove.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
	require.NoError(t, err)

	// when
	spaceToMove, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
		Update(spaceToMove.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
			s.Spec.TargetCluster = member2Await.ClusterName
		})
	require.NoError(t, err)

	// then
	// expect NSTemplateSet to be deleted on member-1 cluster
	err = member1Await.WaitUntilNSTemplateSetDeleted(t, spaceToMove.Name)
	require.NoError(t, err)

	// the users added via SBR will lose access to the Space, since SBRs where persisted on the namespace on member1 which was deleting while retargeting
	_, err = member2Await.WaitForNSTmplSet(t, spaceToMove.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name)))
	require.NoError(t, err)
	VerifyResourcesProvisionedForSpace(t, awaitilities, spaceToMove.Name, wait.UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
	VerifyUserRelatedResources(t, awaitilities, user.UserSignup, ownerMur.Spec.TierName, ExpectUserAccountIn(member2Await))

	// the move doesn't have any effect on the other signups and MURs
	VerifyUserRelatedResources(t, awaitilities, userToShareWith1.UserSignup, murToShareWith1.Spec.TierName, ExpectUserAccountIn(member1Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith1.Name, wait.UntilSpaceHasStatusTargetCluster(member1Await.ClusterName))

	VerifyUserRelatedResources(t, awaitilities, userToShareWith2.UserSignup, murToShareWith2.Spec.TierName, ExpectUserAccountIn(member2Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith2.Name, wait.UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))

	// no SBRs are present
	sbrsFound, err := awaitilities.Member2().ListSpaceBindingRequests(GetDefaultNamespace(spaceToMove.Status.ProvisionedNamespaces))
	require.NoError(t, err)
	require.Empty(t, sbrsFound)
}
