package parallel

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpaceAndSpaceBindingCleanup(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	// let's create the spaces for the deletion here so they have older creation timestamp and we don't have to wait entire 30 seconds for the deletion
	space1, _, binding1 := CreateSpace(t, awaitilities, testspace.WithTierName("base"))
	space2, signup2, binding2 := CreateSpace(t, awaitilities, testspace.WithTierName("base"))

	// TODO: needs to be changed as soon as we start creating objects in namespaces for SpaceRoles - we need to verify that it also automatically updates NSTemplateSet and the resources in the namespaces
	t.Run("for SpaceBinding", func(t *testing.T) {
		t.Run("when space is deleted", func(t *testing.T) {

			// given
			space, _, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, memberAwait, "joe", "for-redhat")

			// when
			err := hostAwait.Client.Delete(context.TODO(), space)
			require.NoError(t, err)

			// then
			err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
			require.NoError(t, err)
		})

		t.Run("when mur is deleted", func(t *testing.T) {
			// given
			_, userSignup, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, memberAwait, "lara", "for-ibm")

			// when
			// deactivate the UserSignup so that the MUR will be deleted
			userSignup, err := hostAwait.UpdateUserSignup(t, userSignup.Name,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(us, true)
				})
			require.NoError(t, err)
			t.Logf("user signup '%s' set to deactivated", userSignup.Name)

			// then
			err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
			require.NoError(t, err)
		})

		t.Run("when mur is deleted with SpaceBindingRequest", func(t *testing.T) {
			// given
			// we have a space
			space, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName), testspace.WithName("for-john"))
			// wait for the namespace to be provisioned since we will be creating the SpaceBindingRequest into it.
			space, err := hostAwait.WaitForSpace(t, space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
			require.NoError(t, err)

			// and we also have a user that gets admin access to the Space but using SpaceBindingRequest mechanism
			userSignup, spaceBindingRequest, spaceBinding := setupForSpaceBindingCleanupWithSBRTest(t, awaitilities, memberAwait, space, hostAwait, "jack", "admin")

			// when
			// we deactivate the UserSignup so that the MUR will be deleted
			userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(us, true)
				})
			require.NoError(t, err)
			t.Logf("user signup '%s' set to deactivated", userSignup.Name)

			// then
			// spaceBindingRequest and spaceBindings should be deleted
			err = memberAwait.WaitUntilSpaceBindingRequestDeleted(t, spaceBindingRequest)
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
			require.NoError(t, err)
		})
	})

	// TODO: move this to separate test as soon as we support test execution in parallel and we don't care when the test waits for 30 seconds
	t.Run("for Spaces", func(t *testing.T) {
		// given
		awaitilities := WaitForDeployments(t)
		hostAwait := awaitilities.Host()

		deletionThreshold := -30 * time.Second // space will only be deleted if at least 30 seconds has elapsed since it was created

		// check that the spaces were provisioned before 30 seconds
		space1, err := hostAwait.WaitForSpace(t, space1.Name, wait.UntilSpaceHasCreationTimestampOlderThan(deletionThreshold))
		require.NoError(t, err)
		space2, err := hostAwait.WaitForSpace(t, space2.Name, wait.UntilSpaceHasCreationTimestampOlderThan(deletionThreshold))
		require.NoError(t, err)

		t.Run("when space has parent-space, then it should not be deleted even if without spacebinding", func(t *testing.T) {
			// when
			parentSpace, err := hostAwait.WaitForSpace(t, space1.Name)
			require.NoError(t, err)
			subSpace := CreateSubSpace(t, awaitilities, testspace.WithSpecParentSpace(parentSpace.Name))

			// then
			// parent-space should be provisioned
			parentSpace, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)
			// sub-space should be provisioned
			VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name,
				wait.UntilSpaceHasLabelWithValue(toolchainv1alpha1.ParentSpaceLabelKey, parentSpace.Name)) // check that parent-space label was added accordingly

			t.Run("when SpaceBinding is inherited from parent-space, then sub-space should not be deleted", func(t *testing.T) {
				// when
				// there is no spaceBinding with the sub-space name as label
				err := hostAwait.WaitUntilSpaceBindingsWithLabelDeleted(t, toolchainv1alpha1.SpaceBindingSpaceLabelKey, subSpace.Name)
				require.NoError(t, err)

				// then
				// sub-space is provisioned
				actualSubSpace, _ := VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name,
					wait.UntilSpaceHasLabelWithValue(toolchainv1alpha1.ParentSpaceLabelKey, parentSpace.Name)) // check that parent-space label is present
				// check that sub-space was not recreated
				assert.Equal(t, subSpace.UID, actualSubSpace.UID)
			})

		})

		t.Run("when space is provisioned for more than 30 seconds, then it should not delete the Space when SpaceBinding still exists", func(t *testing.T) {

			// when
			space, err := hostAwait.WaitForSpace(t, space1.Name)
			require.NoError(t, err)

			// then
			space, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)

			t.Run("when SpaceBinding is deleted, then Space should be automatically deleted as well", func(t *testing.T) {
				// when
				err := hostAwait.Client.Delete(context.TODO(), binding1)
				require.NoError(t, err)

				// then
				err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, space.Name)
				require.NoError(t, err)
			})
		})

		t.Run("when UserSignup (and thus also MUR) is deleted, then it triggers deletion of both SpaceBinding and Space", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), signup2)
			require.NoError(t, err)

			// then
			err = hostAwait.WaitUntilSpaceBindingDeleted(binding2.Name)
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, space2.Name)
			require.NoError(t, err)
		})
	})
}

func setupForSpaceBindingCleanupWithSBRTest(t *testing.T, awaitilities wait.Awaitilities, memberAwait *wait.MemberAwaitility, space *toolchainv1alpha1.Space, hostAwait *wait.HostAwaitility, username, spaceRole string) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBindingRequest, *toolchainv1alpha1.SpaceBinding) {
	userSignup2, mur2 := NewSignupRequest(awaitilities).
		Username(username).
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		NoSpace().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).Resources()
	//... that gets access to the space but using SpaceBindingRequests
	spaceBindingRequest := CreateSpaceBindingRequest(t, awaitilities, memberAwait.ClusterName,
		WithSpecSpaceRole(spaceRole),
		WithSpecMasterUserRecord(mur2.GetName()),
		WithNamespace(GetDefaultNamespace(space.Status.ProvisionedNamespaces)),
	)
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
		wait.UntilSpaceBindingRequestHasConditions(wait.Provisioned()),
	)
	require.NoError(t, err)
	return userSignup2, spaceBindingRequest, spaceBinding
}

func setupForSpaceBindingCleanupTest(t *testing.T, awaitilities wait.Awaitilities, targetMember *wait.MemberAwaitility, murName, spaceName string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBinding) {
	space, owner, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(targetMember.ClusterName), testspace.WithName(spaceName))
	// at this point, just make sure the space exists so we can bind it to our user
	userSignup, mur := NewSignupRequest(awaitilities).
		Username(murName).
		ManuallyApprove().
		TargetCluster(targetMember).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).Resources()
	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")
	appstudioTier, err := awaitilities.Host().WaitForNSTemplateTier(t, "appstudio")
	require.NoError(t, err)
	// make sure that the NSTemplateSet associated with the Space was updated after the space binding was created (new entry in the `spec.SpaceRoles`)
	// before we can check the resources (roles and rolebindings)
	_, err = targetMember.WaitForNSTmplSet(t, spaceName,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, owner.Status.CompliantUsername, murName)))
	require.NoError(t, err)
	// in particular, verify that there are role and rolebindings for all the users (the "default" one and the one referred as an argument of this func) in the space
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, wait.UntilSpaceHasStatusTargetCluster(targetMember.ClusterName), wait.UntilSpaceHasAnyProvisionedNamespaces())
	return space, userSignup, spaceBinding
}
