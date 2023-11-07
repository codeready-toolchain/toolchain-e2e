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
	primaryMUR, primarySpace := createUser(t, awaitilities, "space-owner", awaitilities.Member1()) // we provision one space on m1 that will be shared with other users in the same cluster
	guestMUR, _ := createUser(t, awaitilities, "space-guest", awaitilities.Member1())
	spaceGuestMURwithSBR, _ := createUser(t, awaitilities, "space-guest-sbr", awaitilities.Member1())
	// setup some users and spaces on m2
	primaryMUR1, primarySpace1 := createUser(t, awaitilities, "space-owner1", awaitilities.Member2()) // we provision one space on m2 that will be shared with other users in the same cluster
	guestMUR1, _ := createUser(t, awaitilities, "space-guest1", awaitilities.Member2())
	guestMUR2, _ := createUser(t, awaitilities, "space-guest2", awaitilities.Member2())

	// when
	// we add the spaceGuests to the primarySpace using a spacebinding
	guestSpaceBinding := testsupportspacebinding.CreateSpaceBinding(t, awaitilities.Host(), guestMUR, primarySpace, "admin")
	tier, err := awaitilities.Host().WaitForNSTemplateTier(t, primarySpace.Spec.TierName)
	require.NoError(t, err)
	_, err = awaitilities.Member1().WaitForNSTmplSet(t, primarySpace.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, primaryMUR.Name, guestMUR.Name)))
	require.NoError(t, err)
	// we add the spaceGuestWithSBR to the spaceSpace using spacebindingrequests
	guestSBR := shareSpaceWith(t, awaitilities, primarySpace, spaceGuestMURwithSBR)
	// we have a second space where we add a bunch of users with different roles
	guestSpaceBinding1 := testsupportspacebinding.CreateSpaceBinding(t, awaitilities.Host(), guestMUR1, primarySpace1, "contributor")
	guestSpaceBinding2 := testsupportspacebinding.CreateSpaceBinding(t, awaitilities.Host(), guestMUR2, primarySpace1, "maintainer")
	tier1, err := awaitilities.Host().WaitForNSTemplateTier(t, primarySpace1.Spec.TierName)
	require.NoError(t, err)
	_, err = awaitilities.Member2().WaitForNSTmplSet(t, primarySpace1.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(tier1.Spec.SpaceRoles["admin"].TemplateRef, primaryMUR1.Name),
			wait.SpaceRole(tier1.Spec.SpaceRoles["contributor"].TemplateRef, guestMUR1.Name),
			wait.SpaceRole(tier1.Spec.SpaceRoles["maintainer"].TemplateRef, guestMUR2.Name)))
	require.NoError(t, err)

	// then
	//
	// we check everything matches on the first space
	// the spacebinding for the primary user is still there
	testsupportspacebinding.VerifySpaceBinding(t, awaitilities.Host(), primaryMUR.Name, primarySpace.Name, "admin")
	// there should be a spacebinding request for guestMUR
	_, err = awaitilities.Member1().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace.Status.ProvisionedNamespaces), Name: guestMUR.GetName() + "-admin"},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("admin"), // has admin role
		wait.UntilSpaceBindingRequestHasSpecMUR(guestMUR.Name),
	)
	require.NoError(t, err)
	// the spacebinding is gone, since was converted into a spacebindingrequest
	err = awaitilities.Host().WaitUntilSpaceBindingDeleted(guestSpaceBinding.GetName())
	require.NoError(t, err)
	// there should be a spacebindingrequest for the guestSBR , and should be the one that was created initially
	guestSBRfound, err := awaitilities.Member1().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace.Status.ProvisionedNamespaces), Name: guestSBR.GetName()},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("admin"), // has admin role
		wait.UntilSpaceBindingRequestHasSpecMUR(spaceGuestMURwithSBR.Name),
	)
	require.Equal(t, guestSBR.UID, guestSBRfound.UID)

	// we check everything matches on the second space
	// the spacebinding for the primary user is still there
	testsupportspacebinding.VerifySpaceBinding(t, awaitilities.Host(), primaryMUR1.Name, primarySpace1.Name, "admin")
	// there should be a spacebinding request for guestMUR1
	_, err = awaitilities.Member2().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace1.Status.ProvisionedNamespaces), Name: guestMUR1.GetName() + "-contributor"},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("contributor"), // has contributor role
		wait.UntilSpaceBindingRequestHasSpecMUR(guestMUR1.Name),
	)
	require.NoError(t, err)
	// the spacebinding is gone, since was converted into a spacebindingrequest
	err = awaitilities.Host().WaitUntilSpaceBindingDeleted(guestSpaceBinding1.GetName())
	require.NoError(t, err)
	// there should be a spacebinding request for guestMUR2
	_, err = awaitilities.Member2().WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: testsupportspace.GetDefaultNamespace(primarySpace1.Status.ProvisionedNamespaces), Name: guestMUR2.GetName() + "-maintainer"},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
		wait.UntilSpaceBindingRequestHasSpecSpaceRole("maintainer"), // has maintainer role
		wait.UntilSpaceBindingRequestHasSpecMUR(guestMUR2.Name),
	)
	require.NoError(t, err)
	// the spacebinding is gone, since was converted into a spacebindingrequest
	err = awaitilities.Host().WaitUntilSpaceBindingDeleted(guestSpaceBinding2.GetName())
	require.NoError(t, err)

	// check that the expected number of SBRs matches
	sbrsFound, err := awaitilities.Member1().ListSpaceBindingRequests(testsupportspace.GetDefaultNamespace(primarySpace.Status.ProvisionedNamespaces))
	require.NoError(t, err)
	require.Equal(t, len(sbrsFound), 2)
	sbrsFound1, err := awaitilities.Member2().ListSpaceBindingRequests(testsupportspace.GetDefaultNamespace(primarySpace1.Status.ProvisionedNamespaces))
	require.NoError(t, err)
	require.Equal(t, len(sbrsFound1), 2)
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
