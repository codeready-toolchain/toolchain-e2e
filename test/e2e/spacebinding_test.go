package e2e

import (
	"context"
	"fmt"
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
		space, _, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, hostAwait, memberAwait, "joe", "redhat")

		// when
		err := hostAwait.Client.Delete(context.TODO(), space)
		require.NoError(t, err)

		// then
		err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
		require.NoError(t, err)
	})

	t.Run("when mur is deleted", func(t *testing.T) {
		// given
		_, mur, spaceBinding := setupForSpaceBindingCleanupTest(t, awaitilities, hostAwait, memberAwait, "lara", "ibm")

		// when
		err := hostAwait.Client.Delete(context.TODO(), mur)
		require.NoError(t, err)

		// then
		err = hostAwait.WaitUntilSpaceBindingDeleted(spaceBinding.Name)
		require.NoError(t, err)
	})
}

func setupForSpaceBindingCleanupTest(t *testing.T, awaitilities wait.Awaitilities, hostAwait *wait.HostAwaitility, targetMember *wait.MemberAwaitility, murName, spaceName string) (*v1alpha1.Space, *v1alpha1.MasterUserRecord, *v1alpha1.SpaceBinding) {
	space := NewSpace(hostAwait.Namespace, spaceName, "appstudio", WithTargetCluster(targetMember.ClusterName))
	err := hostAwait.CreateWithCleanup(context.TODO(), space)
	require.NoError(t, err)
	space = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, targetMember, space.Name, "appstudio")

	_, mur := NewSignupRequest(t, awaitilities).
		Username(murName).
		ManuallyApprove().
		TargetCluster(targetMember).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	spaceBinding := newSpaceBinding(space.Namespace, mur.Name, space.Name, "admin")
	err = hostAwait.CreateWithCleanup(context.TODO(), spaceBinding)
	require.NoError(t, err)

	return space, mur, spaceBinding
}

func newSpaceBinding(namespace, mur, space, spaceRole string) *v1alpha1.SpaceBinding {
	return &v1alpha1.SpaceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", mur, space),
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
