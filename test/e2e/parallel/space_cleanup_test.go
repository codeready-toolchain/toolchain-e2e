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
			err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
			require.NoError(t, err)
		})

		t.Run("when mur is deleted", func(t *testing.T) {
			// given
			_, userSignup, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, memberAwait, "lara", "ibm")

			// when
			// deactivate the UserSignup so that the MUR will be deleted
			userSignup, err := hostAwait.UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
			require.NoError(t, err)
			t.Logf("user signup '%s' set to deactivated", userSignup.Name)

			// then
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
		space1, err := hostAwait.WaitForSpace(space1.Name, wait.UntilSpaceHasCreationTimestampOlderThan(deletionThreshold))
		require.NoError(t, err)
		space2, err := hostAwait.WaitForSpace(space2.Name, wait.UntilSpaceHasCreationTimestampOlderThan(deletionThreshold))
		require.NoError(t, err)

		t.Run("when space is provisioned for more than 30 seconds, then it should not delete the Space when SpaceBinding still exists", func(t *testing.T) {

			// when
			space, err := hostAwait.WaitForSpace(space1.Name)
			require.NoError(t, err)

			// then
			space = VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)

			t.Run("when SpaceBinding is deleted, then Space should be automatically deleted as well", func(t *testing.T) {
				// when
				err := hostAwait.Client.Delete(context.TODO(), binding1)
				require.NoError(t, err)

				// then
				err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(space.Name)
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
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(space2.Name)
			require.NoError(t, err)
		})
	})
}

func setupForSpaceBindingCleanupTest(t *testing.T, awaitilities wait.Awaitilities, targetMember *wait.MemberAwaitility, murName, spaceName string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBinding) {
	space, _, _ := CreateSpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(targetMember.ClusterName), WithName(spaceName))
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, wait.UntilSpaceHasStatusTargetCluster(targetMember.ClusterName))

	userSignup, mur := NewSignupRequest(t, awaitilities).
		Username(murName).
		ManuallyApprove().
		TargetCluster(targetMember).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")

	return space, userSignup, spaceBinding
}
