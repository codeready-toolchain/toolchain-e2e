package parallel

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

func TestSpaceAndSpaceBindingCleanup(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	// let's create the spaces for the deletion here so they have older creation timestamp and we don't have to wait entire 30 seconds for the deletion
	space1, _, binding1 := CreateSpace(t, awaitilities)
	space2, signup2, binding2 := CreateSpace(t, awaitilities)

	// TODO: needs to be changed as soon as we start creating objects in namespaces for SpaceRoles - we need to verify that it also automatically updates NSTemplateSet and the resources in the namespaces
	t.Run("for SpaceBinding", func(t *testing.T) {
		t.Run("when space is deleted", func(t *testing.T) {
			// given
			space, _, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, memberAwait, "joe", "redhat")

			// when
			err := hostAwait.Client.Delete(context.TODO(), space)
			require.NoError(t, err)

			// then
			hostAwait.WaitUntilSpaceBindingDeleted(t, spaceBinding.Name)
		})

		t.Run("when mur is deleted", func(t *testing.T) {
			// given
			_, userSignup, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, memberAwait, "lara", "ibm")

			// when
			// deactivate the UserSignup so that the MUR will be deleted
			userSignup = hostAwait.UpdateUserSignup(t, userSignup.Name,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(us, true)
				})
			t.Logf("user signup '%s' set to deactivated", userSignup.Name)

			// then
			hostAwait.WaitUntilSpaceBindingDeleted(t, spaceBinding.Name)
		})
	})

	// TODO: move this to separate test as soon as we support test execution in parallel and we don't care when the test waits for 30 seconds
	t.Run("for Spaces", func(t *testing.T) {
		// given
		awaitilities := WaitForDeployments(t)
		hostAwait := awaitilities.Host()

		deletionThreshold := -30 * time.Second // space will only be deleted if at least 30 seconds has elapsed since it was created

		// check that the spaces were provisioned before 30 seconds
		space1 := hostAwait.WaitForSpace(t, space1.Name, wait.UntilSpaceHasCreationTimestampOlderThan(deletionThreshold))
		space2 := hostAwait.WaitForSpace(t, space2.Name, wait.UntilSpaceHasCreationTimestampOlderThan(deletionThreshold))

		t.Run("when space is provisioned for more than 30 seconds, then it should not delete the Space when SpaceBinding still exists", func(t *testing.T) {

			// when
			space := hostAwait.WaitForSpace(t, space1.Name)

			// then
			space, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)

			t.Run("when SpaceBinding is deleted, then Space should be automatically deleted as well", func(t *testing.T) {
				// when
				err := hostAwait.Client.Delete(context.TODO(), binding1)
				require.NoError(t, err)

				// then
				hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, space.Name)
			})
		})

		t.Run("when UserSignup (and thus also MUR) is deleted, then it triggers deletion of both SpaceBinding and Space", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), signup2)
			require.NoError(t, err)

			// then
			hostAwait.WaitUntilSpaceBindingDeleted(t, binding2.Name)
			hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, space2.Name)
		})
	})
}

func setupForSpaceBindingCleanupTest(t *testing.T, awaitilities wait.Awaitilities, targetMember *wait.MemberAwaitility, murName, spaceName string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBinding) {
	space, owner, _ := CreateSpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(targetMember.ClusterName), WithName(spaceName))
	// at this point, just make sure the space exists so we can bind it to our user
	userSignup, mur := NewSignupRequest(awaitilities).
		Username(murName).
		ManuallyApprove().
		TargetCluster(targetMember.ClusterName).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()
	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")
	appstudioTier := awaitilities.Host().WaitForNSTemplateTier(t, "appstudio")
	// make sure that the NSTemplateSet associated with the Space was updated after the space binding was created (new entry in the `spec.SpaceRoles`)
	// before we can check the resources (roles and rolebindings)
	targetMember.WaitForNSTmplSet(t, spaceName,
		wait.UntilNSTemplateSetHasSpaceRoles(wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, owner.Name, murName)))
	// in particular, verify that there are role and rolebindings for all the users (the "default" one and the one referred as an argument of this func) in the space
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, wait.UntilSpaceHasStatusTargetCluster(targetMember.ClusterName))
	return space, userSignup, spaceBinding
}
