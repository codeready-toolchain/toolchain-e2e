package e2e

import (
	"context"
	"math/rand"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateSpace(t *testing.T) {
	// given

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	rand.Seed(time.Now().UnixNano())

	t.Run("create space", func(t *testing.T) {
		// given
		space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "appstudio", memberAwait)

		// when
		err := hostAwait.Client.Create(context.TODO(), space)

		// then
		// then
		require.NoError(t, err)
		space = VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, memberAwait, space.Name, "appstudio")

		t.Run("delete space", func(t *testing.T) {
			// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
			// along with its associated namespace

			// when
			err = hostAwait.Client.Delete(context.TODO(), space)

			// then
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceDeleted(space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(space.Name, "appstudio")
			require.NoError(t, err)
		})
	})

	t.Run("failed to create space", func(t *testing.T) {

		t.Run("missing target member cluster", func(t *testing.T) {
			// given
			space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", nil)

			// when
			err := hostAwait.Client.Create(context.TODO(), space)

			// then
			require.NoError(t, err)
			space, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(ProvisioningPending("unspecified target member cluster")))
			require.NoError(t, err)

			t.Run("delete space", func(t *testing.T) {
				// when
				err = hostAwait.Client.Delete(context.TODO(), space)

				// then
				require.NoError(t, err)
				err = hostAwait.WaitUntilSpaceDeleted(space.Name)
				require.NoError(t, err)
			})
		})

		t.Run("unknown target member cluster", func(t *testing.T) {
			// given
			s := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", nil)
			s.Spec.TargetCluster = "unknown"

			// when
			err := hostAwait.Client.Create(context.TODO(), s)

			// then
			require.NoError(t, err)
			s, err = hostAwait.WaitForSpace(s.Name, wait.UntilSpaceHasConditions(ProvisioningFailed("unknown target member cluster 'unknown'")))
			require.NoError(t, err)

			t.Run("unable to delete space", func(t *testing.T) {
				// when
				err = hostAwait.Client.Delete(context.TODO(), s)

				// then it should fail while the member cluster is unknown (ie, unreachable)
				require.NoError(t, err)
				s, err = hostAwait.WaitForSpace(s.Name, wait.UntilSpaceHasConditions(TerminatingFailed("cannot delete NSTemplateSet: unknown target member cluster: 'unknown'")))
				require.NoError(t, err)

				t.Run("update target cluster to unblock deletion", func(t *testing.T) {
					// given
					s.Spec.TargetCluster = memberAwait.ClusterName
					// when
					err = hostAwait.Client.Update(context.TODO(), s)

					// then it should fail while the member cluster is unknown (ie, unreachable)
					require.NoError(t, err)

					t.Run("space should be finally deleted", func(t *testing.T) {
						// when
						err = hostAwait.WaitUntilSpaceDeleted(s.Name)
						// then
						require.NoError(t, err)
					})
				})
			})
		})
	})
}

func TestUpdateSpace(t *testing.T) {

	// given

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	rand.Seed(time.Now().UnixNano())

	space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", memberAwait)

	// when
	err := hostAwait.CreateWithCleanup(context.TODO(), space)

	// then
	require.NoError(t, err)

	space = VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, memberAwait, space.Name, "base")

	t.Run("update tier", func(t *testing.T) {
		// given
		ctr := NewChangeTierRequest(hostAwait.Namespace, space.Name, "advanced")

		// when
		err = hostAwait.Client.Create(context.TODO(), ctr)

		// then
		require.NoError(t, err)
		_, err := hostAwait.WaitForChangeTierRequest(ctr.Name, toBeComplete)
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, memberAwait, space.Name, "advanced")
	})
}

func TestRetargetSpace(t *testing.T) {
	// given
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	t.Run("to no other cluster", func(t *testing.T) {
		// given
		space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", member1Await)
		err := hostAwait.CreateWithCleanup(context.TODO(), space)
		require.NoError(t, err)
		// wait until Space has been provisioned on member-1
		VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, member1Await, space.Name, "base")

		// when
		space, err = hostAwait.UpdateSpace(space.Name, func(s *toolchainv1alpha1.Space) {
			s.Spec.TargetCluster = ""
		})
		require.NoError(t, err)

		// then
		_, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(ProvisioningPending("unspecified target member cluster")))
		require.NoError(t, err)
		err = member1Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet to be delete on member-1 cluster
		require.NoError(t, err)
		err = member2Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet is not created in member-2 cluster
		require.NoError(t, err)

	})

	t.Run("to another cluster", func(t *testing.T) {
		// given
		space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", member1Await)
		err := hostAwait.CreateWithCleanup(context.TODO(), space)
		require.NoError(t, err)
		// wait until Space has been provisioned on member-1
		space = VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, member1Await, space.Name, "base")

		// when
		space.Spec.TargetCluster = member2Await.ClusterName
		err = hostAwait.Client.Update(context.TODO(), space)
		require.NoError(t, err)

		// then
		// wait until Space has been provisioned on member-1
		space = VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, member2Await, space.Name, "base")
		err = member1Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet to be delete on member-1 cluster
		require.NoError(t, err)
	})
}

func ProvisioningPending(msg string) toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.SpaceProvisioningPendingReason,
		Message: msg,
	}
}

func ProvisioningFailed(msg string) toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.SpaceProvisioningFailedReason,
		Message: msg,
	}
}

func TerminatingFailed(msg string) toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.SpaceTerminatingFailedReason,
		Message: msg,
	}
}
