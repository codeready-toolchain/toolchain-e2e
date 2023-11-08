package parallel

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func TestRetargetUserByChangingSpaceTargetClusterWhenSpaceIsNotShared(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	userSignup, mur := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	space, err := hostAwait.WaitForSpace(t, userSignup.Status.CompliantUsername,
		UntilSpaceHasStatusTargetCluster(member1Await.ClusterName),
		UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)

	// when
	space, err = hostAwait.UpdateSpace(t, space.Name, func(s *toolchainv1alpha1.Space) {
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
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
	VerifyUserRelatedResources(t, awaitilities, userSignup, mur.Spec.TierName, ExpectUserAccountIn(member2Await))
}

// FIXME: temporarily disabled since the SpaceBindingRequest migration controller will replace SBs with SBRs and retargeting of spaces will cause deletion of the namespace that has the SBRs.
// the test will be enabled again once we delete the SpaceBindingRequest migration controller
//func TestRetargetUserByChangingSpaceTargetClusterWhenSpaceIsShared(t *testing.T) {
//	// given
//	t.Parallel()
//	awaitilities := WaitForDeployments(t)
//	hostAwait := awaitilities.Host()
//	member1Await := awaitilities.Member1()
//	member2Await := awaitilities.Member2()
//
//	signupToShareWith1, murToShareWith1 := NewSignupRequest(awaitilities).
//		ManuallyApprove().
//		TargetCluster(member1Await).
//		WaitForMUR().
//		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
//		Execute(t).Resources()
//	signupToShareWith2, murToShareWith2 := NewSignupRequest(awaitilities).
//		ManuallyApprove().
//		TargetCluster(member2Await).
//		WaitForMUR().
//		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
//		Execute(t).Resources()
//
//	userSignup, ownerMur := NewSignupRequest(awaitilities).
//		ManuallyApprove().
//		TargetCluster(member1Await).
//		EnsureMUR().
//		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
//		Execute(t).Resources()
//
//	spaceToMove, err := hostAwait.WaitForSpace(t, userSignup.Status.CompliantUsername,
//		UntilSpaceHasStatusTargetCluster(member1Await.ClusterName),
//		UntilSpaceHasAnyTierNameSet())
//	require.NoError(t, err)
//
//	spacebinding.CreateSpaceBinding(t, hostAwait, murToShareWith1, spaceToMove, "admin")
//	spacebinding.CreateSpaceBinding(t, hostAwait, murToShareWith2, spaceToMove, "admin")
//
//	tier, err := hostAwait.WaitForNSTemplateTier(t, spaceToMove.Spec.TierName)
//	require.NoError(t, err)
//	_, err = member1Await.WaitForNSTmplSet(t, spaceToMove.Name,
//		UntilNSTemplateSetHasSpaceRoles(
//			SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
//	require.NoError(t, err)
//
//	// when
//	spaceToMove, err = hostAwait.UpdateSpace(t, spaceToMove.Name, func(s *toolchainv1alpha1.Space) {
//		s.Spec.TargetCluster = member2Await.ClusterName
//	})
//	require.NoError(t, err)
//
//	// then
//	// expect NSTemplateSet to be deleted on member-1 cluster
//	err = member1Await.WaitUntilNSTemplateSetDeleted(t, spaceToMove.Name)
//	require.NoError(t, err)
//
//	// and provisioned with the set of usernames for admin role as before on member-2
//	_, err = member2Await.WaitForNSTmplSet(t, spaceToMove.Name,
//		UntilNSTemplateSetHasSpaceRoles(
//			SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
//	require.NoError(t, err)
//	VerifyResourcesProvisionedForSpace(t, awaitilities, spaceToMove.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
//	VerifyUserRelatedResources(t, awaitilities, userSignup, ownerMur.Spec.TierName, ExpectUserAccountIn(member2Await))
//
//	// the move doesn't have any effect on the other signups and MURs
//	VerifyUserRelatedResources(t, awaitilities, signupToShareWith1, murToShareWith1.Spec.TierName, ExpectUserAccountIn(member1Await))
//	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith1.Name, UntilSpaceHasStatusTargetCluster(member1Await.ClusterName))
//
//	VerifyUserRelatedResources(t, awaitilities, signupToShareWith2, murToShareWith2.Spec.TierName, ExpectUserAccountIn(member2Await))
//	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith2.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
//}

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

	signupToShareWith1, murToShareWith1 := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		WaitForMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()
	signupToShareWith2, murToShareWith2 := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member2Await).
		WaitForMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	userSignup, ownerMur := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(member1Await).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	spaceToMove, err := hostAwait.WaitForSpace(t, userSignup.Status.CompliantUsername,
		UntilSpaceHasStatusTargetCluster(member1Await.ClusterName),
		UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)

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
		UntilNSTemplateSetHasSpaceRoles(
			SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
	require.NoError(t, err)

	// when
	spaceToMove, err = hostAwait.UpdateSpace(t, spaceToMove.Name, func(s *toolchainv1alpha1.Space) {
		s.Spec.TargetCluster = member2Await.ClusterName
	})
	require.NoError(t, err)

	// then
	// expect NSTemplateSet to be deleted on member-1 cluster
	err = member1Await.WaitUntilNSTemplateSetDeleted(t, spaceToMove.Name)
	require.NoError(t, err)

	// the users added via SBR will lose access to the Space, since SBRs where persisted on the namespace on member1 which was deleting while retargeting
	_, err = member2Await.WaitForNSTmplSet(t, spaceToMove.Name,
		UntilNSTemplateSetHasSpaceRoles(
			SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name)))
	require.NoError(t, err)
	VerifyResourcesProvisionedForSpace(t, awaitilities, spaceToMove.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
	VerifyUserRelatedResources(t, awaitilities, userSignup, ownerMur.Spec.TierName, ExpectUserAccountIn(member2Await))

	// the move doesn't have any effect on the other signups and MURs
	VerifyUserRelatedResources(t, awaitilities, signupToShareWith1, murToShareWith1.Spec.TierName, ExpectUserAccountIn(member1Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith1.Name, UntilSpaceHasStatusTargetCluster(member1Await.ClusterName))

	VerifyUserRelatedResources(t, awaitilities, signupToShareWith2, murToShareWith2.Spec.TierName, ExpectUserAccountIn(member2Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith2.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))

	// no SBRs are present
	sbrsFound, err := awaitilities.Member2().ListSpaceBindingRequests(GetDefaultNamespace(spaceToMove.Status.ProvisionedNamespaces))
	require.NoError(t, err)
	require.Equal(t, 0, len(sbrsFound))
}
