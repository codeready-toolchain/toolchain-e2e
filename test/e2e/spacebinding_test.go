package e2e

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO: needs to be changed as soon as we start creating objects in namespaces for SpaceRoles
func TestSpaceBindingCleanup(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("when space is deleted", func(t *testing.T) {
		// given
		space, _, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, hostAwait, memberAwait)

		// when
		err := hostAwait.Client.Delete(context.TODO(), space)
		require.NoError(t, err)

		// then
		err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
		require.NoError(t, err)
	})

	t.Run("when mur is deleted", func(t *testing.T) {
		// given
		_, mur, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, hostAwait, memberAwait)

		// when
		err := hostAwait.Client.Delete(context.TODO(), mur)
		require.NoError(t, err)

		// then
		err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
		require.NoError(t, err)
	})
}

func setupForSpaceBindingCleanupTest(t *testing.T, awaitilities wait.Awaitilities, hostAwait *wait.HostAwaitility, targetMember *wait.MemberAwaitility) (*v1alpha1.Space, *v1alpha1.MasterUserRecord, *v1alpha1.SpaceBinding) {
	space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "appstudio", targetMember)
	err := hostAwait.CreateWithCleanup(context.TODO(), space)
	require.NoError(t, err)
	space = VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, targetMember, space.Name, "appstudio")

	_, mur := NewSignupRequest(t, awaitilities).
		Username(GenerateName("john-sb")).
		ManuallyApprove().
		TargetCluster(targetMember).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	spaceBinding := newSpaceBinding(space.Namespace, GenerateName("john-admin-sb"), mur.Name, space.Name, "admin")
	err = hostAwait.CreateWithCleanup(context.TODO(), spaceBinding)
	require.NoError(t, err)

	return space, mur, spaceBinding
}

func newSpaceBinding(namespace, name, mur, space, spaceRole string) *v1alpha1.SpaceBinding {
	return &v1alpha1.SpaceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha1.SpaceBindingMasterUserRecordLabelKey: mur,
				v1alpha1.SpaceBindingSpaceLabelKey:            space,
			},
		},
		Spec: v1alpha1.SpaceBindingSpec{
			MasterUserRecord: mur,
			Space:            space,
			SpaceRole:        spaceRole,
		},
	}
}
