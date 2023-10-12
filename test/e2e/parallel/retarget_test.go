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

func TestRetargetUserByChangingSpaceTargetClusterWhenSpaceIsShared(t *testing.T) {
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

	spacebinding.CreateSpaceBinding(t, hostAwait, murToShareWith1, spaceToMove, "admin")
	spacebinding.CreateSpaceBinding(t, hostAwait, murToShareWith2, spaceToMove, "admin")

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

	// and provisioned with the set of usernames for admin role as before on member-2
	_, err = member2Await.WaitForNSTmplSet(t, spaceToMove.Name,
		UntilNSTemplateSetHasSpaceRoles(
			SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, ownerMur.Name, murToShareWith1.Name, murToShareWith2.Name)))
	require.NoError(t, err)
	VerifyResourcesProvisionedForSpace(t, awaitilities, spaceToMove.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
	VerifyUserRelatedResources(t, awaitilities, userSignup, ownerMur.Spec.TierName, ExpectUserAccountIn(member2Await))

	// the move doesn't have any effect on the other signups and MURs
	VerifyUserRelatedResources(t, awaitilities, signupToShareWith1, murToShareWith1.Spec.TierName, ExpectUserAccountIn(member1Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith1.Name, UntilSpaceHasStatusTargetCluster(member1Await.ClusterName))

	VerifyUserRelatedResources(t, awaitilities, signupToShareWith2, murToShareWith2.Spec.TierName, ExpectUserAccountIn(member2Await))
	VerifyResourcesProvisionedForSpace(t, awaitilities, murToShareWith2.Name, UntilSpaceHasStatusTargetCluster(member2Await.ClusterName))
}
