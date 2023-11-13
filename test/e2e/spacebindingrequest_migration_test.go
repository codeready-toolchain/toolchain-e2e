package e2e

import (
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	spacebindingrequesttestcommon "github.com/codeready-toolchain/toolchain-common/pkg/test/spacebindingrequest"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	testsupportspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	testsupportspacebinding "github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestMigrateSpaceBindingToSBR(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	// member cluster configured to skip user creation to mimic stonesoup configuration where user & identity resources are not created
	memberConfigurationWithSkipUserCreation := testconfig.ModifyMemberOperatorConfigObj(awaitilities.Member1().GetMemberOperatorConfig(t), testconfig.SkipUserCreation(true))
	// configure default space tier to appstudio
	awaitilities.Host().UpdateToolchainConfig(t, testconfig.Tiers().DefaultUserTier("deactivate30").DefaultSpaceTier("appstudio"), testconfig.Members().Default(memberConfigurationWithSkipUserCreation.Spec))
	// setup some users and spaces on m1
	primaryMUR1, primarySpace1 := createUser(t, awaitilities, "space-owner1", awaitilities.Member1()) // we provision one space on m1 that will be shared with other users in the same cluster
	guestMUR1, _ := createUser(t, awaitilities, "space-guest1", awaitilities.Member1())
	spaceGuestMURwithSBR1, _ := createUser(t, awaitilities, "space-guest-sbr1", awaitilities.Member1())
	// setup some users and spaces on m2
	primaryMUR2, primarySpace2 := createUser(t, awaitilities, "space-owner2", awaitilities.Member2()) // we provision one space on m2 that will be shared with other users in the same cluster
	guestMUR2a, _ := createUser(t, awaitilities, "space-guest2a", awaitilities.Member2())
	guestMUR2b, _ := createUser(t, awaitilities, "space-guest2b", awaitilities.Member2())

	// when
	// we add the spaceGuests to the primarySpace using a spacebinding
	guestSpaceBinding1 := testsupportspacebinding.CreateSpaceBinding(t, awaitilities.Host(), guestMUR1, primarySpace1, "admin")
	// we add the spaceGuestWithSBR to the spaceSpace using spacebindingrequests
	guestSBR1 := shareSpaceWith(t, awaitilities, primarySpace1, spaceGuestMURwithSBR1)
	// we have a second space where we add a bunch of users with different roles
	guestSpaceBinding2a := testsupportspacebinding.CreateSpaceBinding(t, awaitilities.Host(), guestMUR2a, primarySpace2, "contributor")
	guestSpaceBinding2b := testsupportspacebinding.CreateSpaceBinding(t, awaitilities.Host(), guestMUR2b, primarySpace2, "maintainer")

	// then
	//
	// we check everything matches in m1
	tier, err := awaitilities.Host().WaitForNSTemplateTier(t, primarySpace1.Spec.TierName)
	require.NoError(t, err)
	_, err = awaitilities.Member1().WaitForNSTmplSet(t, primarySpace1.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, primaryMUR1.Name, guestMUR1.Name)))
	require.NoError(t, err)
	// the spacebinding for the primary user is still there
	testsupportspacebinding.VerifySpaceBinding(t, awaitilities.Host(), primaryMUR1.Name, primarySpace1.Name, "admin")
	// there should be a spacebinding request for guestMUR1
	_, err = awaitilities.Member1().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace1.Status.ProvisionedNamespaces), Name: guestMUR1.GetName() + "-admin"},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("admin"), // has admin role
		wait.UntilSpaceBindingRequestHasSpecMUR(guestMUR1.Name),
	)
	require.NoError(t, err)
	// the spacebinding is gone, since was converted into a spacebindingrequest
	err = awaitilities.Host().WaitUntilSpaceBindingDeleted(guestSpaceBinding1.GetName())
	require.NoError(t, err)
	// there should be a spacebindingrequest for the guestSBR1, and should be the one that was created initially
	guestSBRfound, err := awaitilities.Member1().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace1.Status.ProvisionedNamespaces), Name: guestSBR1.GetName()},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("admin"), // has admin role
		wait.UntilSpaceBindingRequestHasSpecMUR(spaceGuestMURwithSBR1.Name),
	)
	require.NoError(t, err)
	require.Equal(t, guestSBR1.UID, guestSBRfound.UID)

	// we check everything matches in m2
	tier1, err := awaitilities.Host().WaitForNSTemplateTier(t, primarySpace2.Spec.TierName)
	require.NoError(t, err)
	_, err = awaitilities.Member2().WaitForNSTmplSet(t, primarySpace2.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier1.Spec.SpaceRoles["admin"].TemplateRef, primaryMUR2.Name),
			wait.SpaceRole(tier1.Spec.SpaceRoles["contributor"].TemplateRef, guestMUR2a.Name),
			wait.SpaceRole(tier1.Spec.SpaceRoles["maintainer"].TemplateRef, guestMUR2b.Name)))
	require.NoError(t, err)
	// the spacebinding for the primary user is still there
	testsupportspacebinding.VerifySpaceBinding(t, awaitilities.Host(), primaryMUR2.Name, primarySpace2.Name, "admin")
	// there should be a spacebinding request for guestMUR2a
	_, err = awaitilities.Member2().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace2.Status.ProvisionedNamespaces), Name: guestMUR2a.GetName() + "-contributor"},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("contributor"), // has contributor role
		wait.UntilSpaceBindingRequestHasSpecMUR(guestMUR2a.Name),
	)
	require.NoError(t, err)
	// the spacebinding is gone, since was converted into a spacebindingrequest
	err = awaitilities.Host().WaitUntilSpaceBindingDeleted(guestSpaceBinding2a.GetName())
	require.NoError(t, err)
	// there should be a spacebinding request for guestMUR2b
	_, err = awaitilities.Member2().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace2.Status.ProvisionedNamespaces), Name: guestMUR2b.GetName() + "-maintainer"},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("maintainer"), // has maintainer role
		wait.UntilSpaceBindingRequestHasSpecMUR(guestMUR2b.Name),
	)
	require.NoError(t, err)
	// the spacebinding is gone, since was converted into a spacebindingrequest
	err = awaitilities.Host().WaitUntilSpaceBindingDeleted(guestSpaceBinding2b.GetName())
	require.NoError(t, err)

	// check that the expected number of SBRs matches
	sbrsFound1, err := awaitilities.Member1().ListSpaceBindingRequests(testsupportspace.GetDefaultNamespace(primarySpace1.Status.ProvisionedNamespaces))
	require.NoError(t, err)
	require.Equal(t, 2, len(sbrsFound1))
	sbrsFound2, err := awaitilities.Member2().ListSpaceBindingRequests(testsupportspace.GetDefaultNamespace(primarySpace2.Status.ProvisionedNamespaces))
	require.NoError(t, err)
	require.Equal(t, 2, len(sbrsFound2))
}

func shareSpaceWith(t *testing.T, awaitilities wait.Awaitilities, spaceToShare *toolchainv1alpha1.Space, guestUserMur *toolchainv1alpha1.MasterUserRecord) *toolchainv1alpha1.SpaceBindingRequest {
	// share primaryUser space with guestUser
	spaceBindingRequest := testsupportspacebinding.CreateSpaceBindingRequest(t, awaitilities, spaceToShare.Spec.TargetCluster,
		testsupportspacebinding.WithSpecSpaceRole("admin"),
		testsupportspacebinding.WithSpecMasterUserRecord(guestUserMur.GetName()),
		testsupportspacebinding.WithNamespace(testsupportspace.GetDefaultNamespace(spaceToShare.Status.ProvisionedNamespaces)),
	)
	_, err := awaitilities.Host().WaitForSpaceBinding(t, guestUserMur.GetName(), spaceToShare.GetName())
	require.NoError(t, err)
	return spaceBindingRequest
}

func createUser(t *testing.T, awaitilities wait.Awaitilities, username string, targetCluster *wait.MemberAwaitility) (*toolchainv1alpha1.MasterUserRecord, *toolchainv1alpha1.Space) {
	// Create and approve signup
	userSignup, mur := NewSignupRequest(awaitilities).
		Username(username).
		Email(fmt.Sprintf("for-%s@redhat.com", username)).
		TargetCluster(targetCluster).
		ManuallyApprove().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		EnsureMUR().
		Execute(t).
		Resources()
	VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "deactivate30", "appstudio")
	mur, err := awaitilities.Host().WaitForMasterUserRecord(t, mur.GetName(), wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
	require.NoError(t, err)
	space, err := awaitilities.Host().WaitForSpace(t, userSignup.Status.CompliantUsername)
	require.NoError(t, err)
	return mur, space
}
