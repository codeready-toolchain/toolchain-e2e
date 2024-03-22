package community_test

import (
	"sort"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/types"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"

	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	spacebindingrequesttestcommon "github.com/codeready-toolchain/toolchain-common/pkg/test/spacebindingrequest"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	testsupportspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
)

func NewSpaceBindingRequest(
	t *testing.T,
	awaitilities wait.Awaitilities,
	memberAwait *wait.MemberAwaitility,
	hostAwait *wait.HostAwaitility,
	spaceRole string,
) (
	*toolchainv1alpha1.Space,
	*toolchainv1alpha1.SpaceBindingRequest,
	*toolchainv1alpha1.SpaceBinding,
) {
	space, firstUserSignup, _ := testsupportspace.CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
	// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
	space, err := hostAwait.WaitForSpace(t, space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)
	// let's create a new MUR that will have access to the space
	username := uuid.Must(uuid.NewV4()).String()
	_, secondUserMUR := NewSignupRequest(awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		NoSpace().
		WaitForMUR().Execute(t).Resources()
	// create the spacebinding request
	spaceBindingRequest := spacebinding.CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
		spacebinding.WithSpecSpaceRole(spaceRole),

		spacebinding.WithSpecMasterUserRecord(secondUserMUR.GetName()),
		spacebinding.WithNamespace(testsupportspace.GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
	)

	// then
	// check for the spaceBinding creation
	spaceBinding, err := hostAwait.WaitForSpaceBinding(t, spaceBindingRequest.Spec.MasterUserRecord, space.Name,
		wait.UntilSpaceBindingHasMurName(spaceBindingRequest.Spec.MasterUserRecord),
		wait.UntilSpaceBindingHasSpaceName(space.Name),
		wait.UntilSpaceBindingHasSpaceRole(spaceBindingRequest.Spec.SpaceRole),
		wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestLabelKey, spaceBindingRequest.GetName()),
		wait.UntilSpaceBindingHasLabel(toolchainv1alpha1.SpaceBindingRequestNamespaceLabelKey, spaceBindingRequest.GetNamespace()),
	)
	require.NoError(t, err)
	// wait for spacebinding request status
	spaceBindingRequest, err = memberAwait.WaitForSpaceBindingRequest(t, types.NamespacedName{Namespace: spaceBindingRequest.GetNamespace(), Name: spaceBindingRequest.GetName()},
		wait.UntilSpaceBindingRequestHasConditions(spacebindingrequesttestcommon.Ready()),
	)
	require.NoError(t, err)
	tier, err := awaitilities.Host().WaitForNSTemplateTier(t, space.Spec.TierName)
	require.NoError(t, err)
	if spaceRole == "admin" {
		usernamesSorted := []string{firstUserSignup.Status.CompliantUsername, secondUserMUR.Name}
		sort.Strings(usernamesSorted)
		_, err = memberAwait.WaitForNSTmplSet(t, space.Name,
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(tier.Spec.SpaceRoles[spaceRole].TemplateRef, usernamesSorted[0], usernamesSorted[1])))
		require.NoError(t, err)
	} else {
		_, err = memberAwait.WaitForNSTmplSet(t, space.Name,
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, firstUserSignup.Status.CompliantUsername),
				wait.SpaceRole(tier.Spec.SpaceRoles[spaceRole].TemplateRef, secondUserMUR.Name)))
		require.NoError(t, err)
	}
	testsupportspace.VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
	return space, spaceBindingRequest, spaceBinding
}
