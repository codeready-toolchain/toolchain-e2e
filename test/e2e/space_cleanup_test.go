package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

// TODO: needs to be changed as soon as we start creating objects in namespaces for SpaceRoles
func TestSpaceBindingCleanup(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	// Create Spaces, MURs & SpaceBindings to verify automatic deletion.
	// The verification is done in the following test, but we need to create it here so the Spaces has older creation timestamp
	createSpacesToVerifyDeletion(t, awaitilities)

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
		_, mur, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, memberAwait, "lara", "ibm")

		// when
		err := hostAwait.Client.Delete(context.TODO(), mur)
		require.NoError(t, err)

		// then
		err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
		require.NoError(t, err)
	})
}

func setupForSpaceBindingCleanupTest(t *testing.T, awaitilities wait.Awaitilities, targetMember *wait.MemberAwaitility, murName, spaceName string) (*v1alpha1.Space, *v1alpha1.MasterUserRecord, *v1alpha1.SpaceBinding) {
	space := CreateAndVerifySpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(targetMember), WithName(spaceName))

	_, mur := NewSignupRequest(t, awaitilities).
		Username(murName).
		ManuallyApprove().
		TargetCluster(targetMember).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")

	return space, mur, spaceBinding
}

var (
	initOnce           = sync.Once{}
	space1, space2     *v1alpha1.Space
	signup2            *v1alpha1.UserSignup
	binding1, binding2 *v1alpha1.SpaceBinding
)

// TODO: move this to separate test as soon as we support test execution in parallel
func TestSpaceCleanup(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	createSpacesToVerifyDeletion(t, awaitilities)

	deletionThreshold := time.Now().Add(-30 * time.Second)

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
		space = VerifyResourcesProvisionedForSpace(t, awaitilities, space)

		t.Run("when SpaceBinding is deleted, then Space should be automatically deleted as well", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), binding1)
			require.NoError(t, err)

			// then
			err = hostAwait.WaitUntilSpaceDeleted(space.Name)
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
		err = hostAwait.WaitUntilSpaceDeleted(space2.Name)
		require.NoError(t, err)
	})
}

func createSpacesToVerifyDeletion(t *testing.T, awaitilities wait.Awaitilities) {
	initOnce.Do(func() {
		space1, _, binding1 = createSpaceWithoutCleanup(t, awaitilities)
		space2, signup2, binding2 = createSpaceWithoutCleanup(t, awaitilities)
	})
}

func createSpaceWithoutCleanup(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceOption) (*v1alpha1.Space, *v1alpha1.UserSignup, *v1alpha1.SpaceBinding) {
	space := NewSpace(awaitilities, opts...)

	err := awaitilities.Host().Client.Create(context.TODO(), space)
	require.NoError(t, err)

	// we need to  create the MUR & SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	signup, spaceBinding := CreateMurWithAdminSpaceBindingForSpace(t, awaitilities, space, false)

	return space, signup, spaceBinding
}
