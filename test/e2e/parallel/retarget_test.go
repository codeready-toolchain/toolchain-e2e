package parallel

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func TestRetargetUserByChangingSpaceTargetCluster(t *testing.T) {
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
