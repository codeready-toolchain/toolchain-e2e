package parallel

import (
	"context"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateSpace(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("create space", func(t *testing.T) {
		// when
		space, _, _ := CreateSpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(memberAwait.ClusterName))
		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))

		t.Run("delete space", func(t *testing.T) {
			// now, delete the Space and expect that the NSTemplateSet will be deleted as well,
			// along with its associated namespace

			// when
			err := hostAwait.Client.Delete(context.TODO(), space)

			// then
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(space.Name, "appstudio")
			require.NoError(t, err)
		})
	})

	t.Run("failed to create space - unknown target member cluster", func(t *testing.T) {
		// given & when
		s, _, _ := CreateSpace(t, awaitilities, WithTierName("appstudio"), func(space *toolchainv1alpha1.Space) {
			space.Spec.TargetCluster = "unknown"
		})

		// then
		s, err := hostAwait.WaitForSpace(s.Name, UntilSpaceHasConditions(ProvisioningFailed("unknown target member cluster 'unknown'")))
		require.NoError(t, err)

		t.Run("unable to delete space", func(t *testing.T) {
			// when
			err = hostAwait.Client.Delete(context.TODO(), s)

			// then it should fail while the member cluster is unknown (ie, unreachable)
			require.NoError(t, err)
			s, err = hostAwait.WaitForSpace(s.Name, UntilSpaceHasConditions(TerminatingFailed("cannot delete NSTemplateSet: unknown target member cluster: 'unknown'")))
			require.NoError(t, err)

			t.Run("update target cluster to unblock deletion", func(t *testing.T) {
				// when
				s, err = hostAwait.UpdateSpace(s.Name, func(s *toolchainv1alpha1.Space) {
					s.Spec.TargetCluster = memberAwait.ClusterName
				})

				// then it should fail while the member cluster is unknown (ie, unreachable)
				require.NoError(t, err)

				t.Run("space should be finally deleted", func(t *testing.T) {
					// when
					err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(s.Name)
					// then
					require.NoError(t, err)
				})
			})
		})
	})
}

func TestSpaceRoles(t *testing.T) {

	t.Parallel()

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// given
	appstudioTier, err := hostAwait.WaitForNSTemplateTier("appstudio")
	require.NoError(t, err)

	// given a user (with her own space, but we'll ignore it in this test)
	_, ownerMUR := NewSignupRequest(t, awaitilities).
		Username("spaceowner").
		Email("spaceowner@redhat.com").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().
		Resources()

	// when a space owned by the user above is created (and user is an admin of this space)
	s, ownerBinding := CreateSpaceWithBinding(t, awaitilities, ownerMUR,
		WithTargetCluster(awaitilities.Member1().ClusterName),
		WithTierName("appstudio"),
	)

	// then
	VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name,
		UntilSpaceHasStatusTargetCluster(awaitilities.Member1().ClusterName),
		UntilSpaceHasTier("appstudio"))
	nsTmplSet, err := memberAwait.WaitForNSTmplSet(s.Name,
		UntilNSTemplateSetHasConditions(Provisioned()),
		UntilNSTemplateSetHasSpaceRoles(
			SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceowner"),
		),
	)
	require.NoError(t, err)
	// fetch the namespace check the `last-applied-space-roles` annotation
	_, err = memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
		UntilNamespaceIsActive(),
		UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
	require.NoError(t, err)

	t.Run("and with guest admin binding", func(t *testing.T) {
		// given a `spaceguest` user (with her own space, but we'll ignore it in this test)
		_, guestMUR := NewSignupRequest(t, awaitilities).
			Username("spaceguest").
			Email("spaceguest@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()

		// when the `spaceguest` user is bound to the space as an admin
		guestBinding := CreateSpaceBinding(t, hostAwait, guestMUR, s, "admin")

		// then
		require.NoError(t, err)
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name,
			UntilNSTemplateSetHasConditions(Provisioned()),
			UntilNSTemplateSetHasSpaceRoles(
				SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceguest", "spaceowner"), // sorted usernames
			),
		)
		require.NoError(t, err)
		// fetch the namespace check the `last-applied-space-roles` annotation
		_, err = memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
			UntilNamespaceIsActive(),
			UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
		require.NoError(t, err)

		t.Run("remove admin binding", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), guestBinding)

			// then
			require.NoError(t, err)
			nsTmplSet, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name,
				UntilNSTemplateSetHasConditions(Provisioned()),
				UntilNSTemplateSetHasSpaceRoles(
					SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceowner"), // "spaceguest" was removed
				),
			)
			require.NoError(t, err)
		})
	})

	t.Run("set owner user as viewer instead", func(t *testing.T) {
		// given an appstudio space with `owner` user as an admin of it
		ownerBinding.Spec.SpaceRole = "viewer"

		// when
		err := hostAwait.Client.Update(context.TODO(), ownerBinding)

		// then
		require.NoError(t, err)
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name,
			UntilNSTemplateSetHasConditions(Provisioned()),
			UntilNSTemplateSetHasSpaceRoles(
				SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, ownerMUR.Name),
			),
		)
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name)
	})
}

func TestPromoteSpace(t *testing.T) {
	t.Parallel()
	// given
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// when
	creatorName := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	space, _, _ := CreateSpace(t, awaitilities, WithTierName("base"), WithTargetCluster(memberAwait.ClusterName))
	// then
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))

	t.Run("to advanced tier", func(t *testing.T) {
		// given
		ctr := tiers.NewChangeTierRequest(hostAwait.Namespace, space.Name, "advanced")

		// when
		err := hostAwait.Client.Create(context.TODO(), ctr)

		// then
		require.NoError(t, err)
		_, err = hostAwait.WaitForChangeTierRequest(ctr.Name, toBeComplete)
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, UntilSpaceHasCreatorLabel(creatorName))
	})
}

var toBeComplete = toolchainv1alpha1.Condition{
	Type:   toolchainv1alpha1.ChangeTierRequestComplete,
	Status: corev1.ConditionTrue,
	Reason: toolchainv1alpha1.ChangeTierRequestChangedReason,
}

func TestRetargetSpace(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	// when
	space, _, _ := CreateSpace(t, awaitilities, WithTierName("base"), WithTargetCluster(member1Await.ClusterName))
	// then
	// wait until Space has been provisioned on member-1
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, UntilSpaceHasStatusTargetCluster(member1Await.ClusterName))

	// when
	space, err := hostAwait.UpdateSpace(space.Name, func(s *toolchainv1alpha1.Space) {
		s.Spec.TargetCluster = member2Await.ClusterName
	})
	require.NoError(t, err)

	// then
	// wait until Space has been provisioned on member-1
	space = VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
	err = member1Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet to be delete on member-1 cluster
	require.NoError(t, err)
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
