package parallel

import (
	"context"
	"sort"
	"testing"
	"time"

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
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNSTemplateSetDeleted(t, space.Name)
			require.NoError(t, err)
			err = memberAwait.WaitUntilNamespaceDeleted(t, space.Name, "appstudio")
			require.NoError(t, err)
		})
	})

	t.Run("failed to create space - unknown target member cluster", func(t *testing.T) {
		// given & when
		s, _, _ := CreateSpace(t, awaitilities,
			WithCreationTimestamp(time.Now().Add(time.Second*120)), // we set a time in the future, so that spacecleanup_controller doesn't delete the space before we actually create the spacebinding
			WithTierName("appstudio"), func(space *toolchainv1alpha1.Space) {
				space.Spec.TargetCluster = "unknown"
			})

		// then
		s, err := hostAwait.WaitForSpace(t, s.Name, UntilSpaceHasConditions(ProvisioningFailed("unknown target member cluster 'unknown'")))
		require.NoError(t, err)

		t.Run("unable to delete space", func(t *testing.T) {
			// when
			err = hostAwait.Client.Delete(context.TODO(), s)

			// then it should fail while the member cluster is unknown (ie, unreachable)
			require.NoError(t, err)
			s, err = hostAwait.WaitForSpace(t, s.Name, UntilSpaceHasConditions(TerminatingFailed("cannot delete NSTemplateSet: unknown target member cluster: 'unknown'")))
			require.NoError(t, err)

			t.Run("update target cluster to unblock deletion", func(t *testing.T) {
				// when
				s, err = hostAwait.UpdateSpace(t, s.Name, func(s *toolchainv1alpha1.Space) {
					s.Spec.TargetCluster = memberAwait.ClusterName
				})

				// then it should fail while the member cluster is unknown (ie, unreachable)
				require.NoError(t, err)

				t.Run("space should be finally deleted", func(t *testing.T) {
					// when
					err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, s.Name)
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
	appstudioTier, err := hostAwait.WaitForNSTemplateTier(t, "appstudio")
	require.NoError(t, err)

	// given a user (with her own space, but we'll ignore it in this test)
	_, ownerMUR := NewSignupRequest(awaitilities).
		Username("spaceowner").
		Email("spaceowner@redhat.com").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		NoSpace().
		Execute(t).
		Resources()

	// when a space owned by the user above is created (and user is an admin of this space)
	s, ownerBinding := CreateSpaceWithBinding(t, awaitilities, ownerMUR,
		WithTargetCluster(awaitilities.Member1().ClusterName),
		WithTierName("appstudio"),
	)

	// then
	_, nsTmplSet := VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name,
		UntilSpaceHasStatusTargetCluster(awaitilities.Member1().ClusterName),
		UntilSpaceHasTier("appstudio"),
	)
	require.NoError(t, err)
	nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
		UntilNSTemplateSetHasSpaceRoles(
			SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, ownerMUR.Name)),
	)
	require.NoError(t, err)

	// fetch the namespace check the `last-applied-space-roles` annotation
	_, err = memberAwait.WaitForNamespace(t, s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
		UntilNamespaceIsActive(),
		UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
	require.NoError(t, err)

	t.Run("and with guest admin binding", func(t *testing.T) {
		// given a `spaceguest` user (with her own space, but we'll ignore it in this test)
		_, guestMUR := NewSignupRequest(awaitilities).
			Username("spaceguest").
			Email("spaceguest@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			WaitForMUR().
			NoSpace().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).
			Resources()

		// when the `spaceguest` user is bound to the space as an admin
		guestBinding := CreateSpaceBinding(t, hostAwait, guestMUR, s, "admin")

		// then
		require.NoError(t, err)
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
			UntilNSTemplateSetHasConditions(Provisioned()),
			UntilNSTemplateSetHasSpaceRoles(
				SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceguest", "spaceowner"), // sorted usernames
			),
		)
		require.NoError(t, err)
		// fetch the namespace check the `last-applied-space-roles` annotation
		_, err = memberAwait.WaitForNamespace(t, s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
			UntilNamespaceIsActive(),
			UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name)

		t.Run("remove admin binding", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), guestBinding)

			// then
			require.NoError(t, err)
			nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
				UntilNSTemplateSetHasConditions(Provisioned()),
				UntilNSTemplateSetHasSpaceRoles(
					SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceowner"), // "spaceguest" was removed
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name)
		})
	})

	t.Run("set owner user as viewer instead", func(t *testing.T) {
		// given an appstudio space with `owner` user as an admin of it
		ownerBinding.Spec.SpaceRole = "viewer"

		// when
		err := hostAwait.Client.Update(context.TODO(), ownerBinding)

		// then
		require.NoError(t, err)
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
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
	space, _, _ := CreateSpace(t, awaitilities, WithTierName("base"), WithTargetCluster(memberAwait.ClusterName))
	// then
	VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))

	t.Run("to advanced tier", func(t *testing.T) {
		// when
		tiers.MoveSpaceToTier(t, hostAwait, space.Name, "advanced")

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
	})
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
	space, err := hostAwait.UpdateSpace(t, space.Name, func(s *toolchainv1alpha1.Space) {
		s.Spec.TargetCluster = member2Await.ClusterName
	})
	require.NoError(t, err)

	// then
	// wait until Space has been provisioned on member-1
	space, _ = VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)
	err = member1Await.WaitUntilNSTemplateSetDeleted(t, space.Name) // expect NSTemplateSet to be delete on member-1 cluster
	require.NoError(t, err)
}

func TestSubSpace(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	appstudioTier, err := hostAwait.WaitForNSTemplateTier(t, "appstudio")
	require.NoError(t, err)

	t.Run("we create a subSpace for the parentSpace and expect same roles and usernames to be inherited in NSTemplateSet", func(t *testing.T) {
		// when
		// we have a parentSpace
		parentSpace, _, parentSpaceBindings := CreateSpace(t, awaitilities, WithTargetCluster(memberAwait.ClusterName), WithTierName("appstudio"))
		// then
		// wait until MUR and Space have been provisioned
		_, parentNSTemplateSet := VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name, UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))
		parentMUR, err := hostAwait.WaitForMasterUserRecord(t, parentSpaceBindings.Spec.MasterUserRecord)
		require.NoError(t, err)

		// when
		// we also have a subSpace with same tier
		subSpace := CreateSubSpace(t, awaitilities, WithParentSpace(parentSpace.Name), WithTierName("appstudio"), WithTargetCluster(memberAwait.ClusterName))

		// then
		// wait until subSpace has been provisioned as well
		_, subSpaceNSTemplateSet := VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
		// check that username and role from parentSpace was inherited in the subSpace NSTemplateSet
		_, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
			UntilNSTemplateSetHasConditions(Provisioned()),
			UntilNSTemplateSetHasSpaceRoles(
				SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // MUR from parentSpaceBinding is here
			),
		)
		require.NoError(t, err)

		// check that user was added to the parentSpace
		_, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
			UntilNSTemplateSetHasConditions(Provisioned()),
			UntilNSTemplateSetHasSpaceRoles(
				SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord),
			),
		)
		require.NoError(t, err)

		t.Run("we update role in parentSpaceBinding and expect change to be reflected in subSpace", func(t *testing.T) {
			// given
			// the parentSpace role was "downgraded" to viewer
			parentSpaceBindings.Spec.SpaceRole = "viewer"

			// when
			// we update the parentSpace bindings
			err := hostAwait.Client.Update(context.TODO(), parentSpaceBindings)

			// then
			// downgrade of the usernames is done in parentSpace
			require.NoError(t, err)
			parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
				UntilNSTemplateSetHasConditions(Provisioned()),
				UntilNSTemplateSetHasSpaceRoles(
					SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // user was downgraded to viewer
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)
			// ....and the downgrade of to viewer of the user should be reflected also to the subSpace
			require.NoError(t, err)
			subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
				UntilNSTemplateSetHasConditions(Provisioned()),
				UntilNSTemplateSetHasSpaceRoles(
					SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // user was downgraded to viewer
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)

			t.Run("we add a specific SpaceBinding bound to the subSpace only", func(t *testing.T) {
				// when
				// we create spaceBinding for subSpace
				_, _, subSpaceBinding := CreateMurWithAdminSpaceBindingForSpace(t, awaitilities, subSpace, true)

				// then
				// subSpace should have usernames and roles from parentSpaceBindings+subSpaceBindings
				subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
					UntilNSTemplateSetHasConditions(Provisioned()),
					UntilNSTemplateSetHasSpaceRoles(
						SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, subSpaceBinding.Spec.MasterUserRecord),      // new MUR is added as admin
						SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // remains unchanged
					),
				)
				require.NoError(t, err)
				VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
				// parentSpace should not have this username role added
				parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
					UntilNSTemplateSetHasConditions(Provisioned()),
					UntilNSTemplateSetHasSpaceRoles(
						SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // unchanged
					),
				)
				require.NoError(t, err)
				VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)

				t.Run("we override the SpaceBinding from parentSpace", func(t *testing.T) {
					// when
					// we create spaceBinding for subSpace
					// override the parentMUR and give him admin role (was viewer previously)
					CreateSpaceBinding(t, awaitilities.Host(), parentMUR, subSpace, "admin")

					// then
					// subSpace should have usernames and roles from parentSpaceBindings+subSpaceBindings
					sortedUsernames := []string{parentSpaceBindings.Spec.MasterUserRecord, subSpaceBinding.Spec.MasterUserRecord}
					sort.Strings(sortedUsernames) // the NSTemplateSet has usernames sorted, this is required so that assertion doesn't fail when order changes
					subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
						UntilNSTemplateSetHasConditions(Provisioned()),
						UntilNSTemplateSetHasSpaceRoles(
							SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, sortedUsernames...), // parent MUR is added as admin
						),
					)
					require.NoError(t, err)
					VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
					// parentSpace should not be affected by the change in sub-space
					parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
						UntilNSTemplateSetHasConditions(Provisioned()),
						UntilNSTemplateSetHasSpaceRoles(
							SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // unchanged in parentNSTemplateSet
						),
					)
					require.NoError(t, err)
					VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)

					t.Run("we remove a user from subSpace only", func(t *testing.T) {
						// when
						err = hostAwait.Client.Delete(context.TODO(), subSpaceBinding)

						// then
						// subSpace should have one user less
						require.NoError(t, err)
						subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
							UntilNSTemplateSetHasConditions(Provisioned()),
							UntilNSTemplateSetHasSpaceRoles(
								SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // removed admin role user anymore
							),
						)
						require.NoError(t, err)
						VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
						// parentSpace should not be affected by the change in sub-space
						parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
							UntilNSTemplateSetHasConditions(Provisioned()),
							UntilNSTemplateSetHasSpaceRoles(
								SpaceRole(appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // unchanged
							),
						)
						require.NoError(t, err)
						VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)
					})
				})
			})
		})
	})
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
