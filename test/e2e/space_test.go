package e2e

import (
	"context"
	"math/rand"
	"strconv"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestSpace(t *testing.T) {
	// given

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	rand.Seed(time.Now().UnixNano())

	t.Run("create space", func(t *testing.T) {
		// given
		space := &toolchainv1alpha1.Space{
			ObjectMeta: v1.ObjectMeta{
				Namespace: hostAwait.Namespace,
				Name:      "oddity-" + strconv.Itoa(rand.Int()),
			},
			Spec: toolchainv1alpha1.SpaceSpec{
				TargetCluster: memberAwait.ClusterName,
				TierName:      "base",
			},
		}

		// when
		err := hostAwait.Client.Create(context.TODO(), space)

		// then
		// then
		require.NoError(t, err)
		// wait until NSTemplateSet has been created and Space is in `Ready` status
		nsTmplSet, err := memberAwait.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		tierChecks, err := tiers.NewChecks("base")
		require.NoError(t, err)
		tiers.VerifyGivenNsTemplateSet(t, memberAwait, nsTmplSet, tierChecks, tierChecks, tierChecks.GetExpectedTemplateRefs(hostAwait))
		space, err = hostAwait.WaitForSpace(space.Name,
			wait.UntilSpaceHasConditions(Provisioned()),
			wait.UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))
		require.NoError(t, err)

		t.Run("delete space", func(t *testing.T) {
			// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
			// along with its associated namespace

			// when
			err = hostAwait.Client.Delete(context.TODO(), space)

			// then
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceDeleted(space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(nsTmplSet.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(nsTmplSet.Name, "dev")
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(nsTmplSet.Name, "stage")
			require.NoError(t, err)
		})
	})

	t.Run("failed to create space", func(t *testing.T) {

		t.Run("missing target member cluster", func(t *testing.T) {
			// given
			space := &toolchainv1alpha1.Space{
				ObjectMeta: v1.ObjectMeta{
					Namespace: hostAwait.Namespace,
					Name:      "oddity-" + strconv.Itoa(rand.Int()),
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					//TargetCluster missing
					TierName: "base",
				},
			}

			// when
			err := hostAwait.Client.Create(context.TODO(), space)

			// then
			require.NoError(t, err)
			space, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(ProvisioningPending("unspecified target member cluster")))
			require.NoError(t, err)

			t.Run("delete space", func(t *testing.T) {
				// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
				// along with its associated namespace

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
			space := &toolchainv1alpha1.Space{
				ObjectMeta: v1.ObjectMeta{
					Namespace: hostAwait.Namespace,
					Name:      "oddity-" + strconv.Itoa(rand.Int()),
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					TargetCluster: "unknown",
					TierName:      "base",
				},
			}

			// when
			err := hostAwait.Client.Create(context.TODO(), space)

			// then
			require.NoError(t, err)
			space, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(ProvisioningFailed("unknown target member cluster 'unknown'")))
			require.NoError(t, err)

			t.Run("delete space", func(t *testing.T) {
				// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
				// along with its associated namespace

				// when
				err = hostAwait.Client.Delete(context.TODO(), space)

				// then
				require.NoError(t, err)
				_, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(TerminatingFailed("Cannot delete NSTemplateSet: unknown target member cluster: 'unknown'")))
				require.NoError(t, err)
			})
		})
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
