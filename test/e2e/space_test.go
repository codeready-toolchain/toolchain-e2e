package e2e

import (
	"context"
	"math/rand"
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

func TestCreateSpace(t *testing.T) {
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
				Namespace:    hostAwait.Namespace,
				GenerateName: "oddity-",
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
					Namespace:    hostAwait.Namespace,
					GenerateName: "oddity-",
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
			s := &toolchainv1alpha1.Space{
				ObjectMeta: v1.ObjectMeta{
					Namespace:    hostAwait.Namespace,
					GenerateName: "oddity-",
				},
				Spec: toolchainv1alpha1.SpaceSpec{
					TargetCluster: "unknown",
					TierName:      "base",
				},
			}

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

	space := &toolchainv1alpha1.Space{
		ObjectMeta: v1.ObjectMeta{
			Namespace:    hostAwait.Namespace,
			GenerateName: "oddity-",
		},
		Spec: toolchainv1alpha1.SpaceSpec{
			TargetCluster: memberAwait.ClusterName,
			TierName:      "base",
		},
	}

	// when
	err := hostAwait.CreateWithCleanup(context.TODO(), space)

	// then
	require.NoError(t, err)
	// wait until NSTemplateSet has been created and Space is in `Ready` status
	space, err = hostAwait.WaitForSpace(space.Name,
		wait.UntilSpaceHasConditions(Provisioned()),
		wait.UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName),
	)
	require.NoError(t, err)
	// wait until NSTemplateSet has been created and Space is in `Ready` status
	nsTmplSet, err := memberAwait.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned()))
	require.NoError(t, err)
	tierChecks, err := tiers.NewChecks("base")
	require.NoError(t, err)
	tiers.VerifyGivenNsTemplateSet(t, memberAwait, nsTmplSet, tierChecks, tierChecks, tierChecks.GetExpectedTemplateRefs(hostAwait))

	t.Run("update tier", func(t *testing.T) {
		// given
		ctr := NewChangeTierRequest(hostAwait.Namespace, space.Name, "advanced")

		// when
		err = hostAwait.Client.Create(context.TODO(), ctr)

		// then
		require.NoError(t, err)
		_, err := hostAwait.WaitForChangeTierRequest(ctr.Name, toBeComplete)
		require.NoError(t, err)
		advancedTier, err := hostAwait.WaitForNSTemplateTier("advanced")
		require.NoError(t, err)
		hash, err := computeTemplateRefsHash(advancedTier) // we can assume the JSON marshalling will always work
		require.NoError(t, err)

		space, err = hostAwait.WaitForSpace(space.Name,
			wait.UntilSpaceHasTier("advanced"),
			wait.UntilSpaceHasLabelWithValue("toolchain.dev.openshift.com/advanced-tier-hash", hash),
			wait.UntilSpaceHasNoLabel("toolchain.dev.openshift.com/base-tier-hash"),
			wait.UntilSpaceHasConditions(Provisioned()))
		require.NoError(t, err)
		nsTmplSet, err := memberAwait.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		tierChecks, err := tiers.NewChecks("advanced")
		require.NoError(t, err)
		tiers.VerifyGivenNsTemplateSet(t, memberAwait, nsTmplSet, tierChecks, tierChecks, tierChecks.GetExpectedTemplateRefs(hostAwait))
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
		space := &toolchainv1alpha1.Space{
			ObjectMeta: v1.ObjectMeta{
				Namespace:    hostAwait.Namespace,
				GenerateName: "oddity-",
			},
			Spec: toolchainv1alpha1.SpaceSpec{
				TargetCluster: member1Await.ClusterName,
				TierName:      "base",
			},
		}
		err := hostAwait.CreateWithCleanup(context.TODO(), space)
		require.NoError(t, err)
		// wait until NSTemplateSet has been created on member-1
		_, err = member1Await.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		// wait until Space is in `Ready` status
		space, err = hostAwait.WaitForSpace(space.Name,
			wait.UntilSpaceHasConditions(Provisioned()),
			wait.UntilSpaceHasStatusTargetCluster(member1Await.ClusterName),
		)
		require.NoError(t, err)

		// when
		space.Spec.TargetCluster = ""
		err = hostAwait.Client.Update(context.TODO(), space)
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
		space := &toolchainv1alpha1.Space{
			ObjectMeta: v1.ObjectMeta{
				Namespace:    hostAwait.Namespace,
				GenerateName: "oddity-",
			},
			Spec: toolchainv1alpha1.SpaceSpec{
				TargetCluster: member1Await.ClusterName,
				TierName:      "base",
			},
		}
		err := hostAwait.CreateWithCleanup(context.TODO(), space)
		require.NoError(t, err)
		// wait until NSTemplateSet has been created
		_, err = member1Await.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		// wait until Space is in `Ready` status
		space, err = hostAwait.WaitForSpace(space.Name,
			wait.UntilSpaceHasConditions(Provisioned()),
			wait.UntilSpaceHasStatusTargetCluster(member1Await.ClusterName),
		)
		require.NoError(t, err)

		// when
		space.Spec.TargetCluster = member2Await.ClusterName
		err = hostAwait.Client.Update(context.TODO(), space)
		require.NoError(t, err)

		// then
		_, err = hostAwait.WaitForSpace(space.Name, wait.UntilSpaceHasConditions(Provisioned()))
		require.NoError(t, err)
		_, err = member2Await.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasConditions(Provisioned())) // expect NSTemplateSet to be created on member-2 cluster
		require.NoError(t, err)
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
