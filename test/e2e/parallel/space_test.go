package parallel

import (
	"context"
	"sort"
	"sync"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	testsupportsb "github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
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
		space, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))

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
		s, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster("unknown"))

		// then
		s, err := hostAwait.WaitForSpace(t, s.Name, wait.UntilSpaceHasConditions(ProvisioningFailed("unknown target member cluster 'unknown'")))
		require.NoError(t, err)

		t.Run("space should be deleted", func(t *testing.T) {
			// when
			err = hostAwait.Client.Delete(context.TODO(), s)

			// then
			// it should still delete the space since it wasn't provisioned
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, s.Name)
			require.NoError(t, err)
		})
	})

	t.Run("failed to delete provisioned space - unknown target member cluster", func(t *testing.T) {
		// given & when
		space, _, _ := CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, space.Name)

		// ensure that the space is reset back to the original (valid) target cluster
		// so the cleanup logic can delete the Space
		var resetOnce sync.Once
		reset := func() {
			resetOnce.Do(func() {
				_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
					Update(space.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
						s.Spec.TargetCluster = memberAwait.ClusterName
					})
				require.NoError(t, err)
			})
		}
		defer reset()

		t.Run("unable to delete space that was already provisioned", func(t *testing.T) {
			// given
			s, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
				Update(space.Name, hostAwait.Namespace, func(ss *toolchainv1alpha1.Space) {
					ss.Spec.TargetCluster = "unknown"
				})
			require.NoError(t, err)

			// when
			err = hostAwait.Client.Delete(context.TODO(), s)
			require.NoError(t, err)

			// then
			// it should fail,  while the member cluster is unknown (ie, unreachable),
			// since this space was already provisioned.
			s, err = hostAwait.WaitForSpace(t, s.Name, wait.UntilSpaceHasConditions(TerminatingFailed("cannot delete NSTemplateSet: unknown target member cluster: 'unknown'")))
			require.NoError(t, err)

			t.Run("update target cluster to unblock deletion", func(t *testing.T) {
				// when
				reset()

				// then
				// space should be finally deleted
				err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, s.Name)
				require.NoError(t, err)
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

	// given a user (with her own space)
	ownerUser := NewSignupRequest(awaitilities).
		Username("spaceowner").
		Email("spaceowner@redhat.com").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		SpaceTier("appstudio").
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	ownerMUR := ownerUser.MUR

	// then
	s, nsTmplSet := VerifyResourcesProvisionedForSpace(t, awaitilities, ownerMUR.Name,
		wait.UntilSpaceHasStatusTargetCluster(awaitilities.Member1().ClusterName),
		wait.UntilSpaceHasTier("appstudio"),
	)
	require.NoError(t, err)
	nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
		wait.UntilNSTemplateSetHasSpaceRoles(
			wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, ownerMUR.Name)),
	)
	require.NoError(t, err)

	// fetch the namespace check the `last-applied-space-roles` annotation
	_, err = memberAwait.WaitForNamespace(t, s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
		wait.UntilNamespaceIsActive(),
		wait.UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
	require.NoError(t, err)

	ownerBinding, err := awaitilities.Host().WaitForSpaceBinding(t, ownerMUR.Name, ownerUser.Space.Name)
	require.NoError(t, err)

	t.Run("and with guest admin binding", func(t *testing.T) {
		// given a `spaceguest` user (with her own space, but we'll ignore it in this test)
		guestUser := NewSignupRequest(awaitilities).
			Username("spaceguest").
			Email("spaceguest@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			WaitForMUR().
			NoSpace().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)

		// when the `spaceguest` user is bound to the space as an admin
		guestBinding := testsupportsb.CreateSpaceBinding(t, hostAwait, guestUser.MUR, s, "admin")

		// then
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
			wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceguest", "spaceowner"), // sorted usernames
			),
		)
		require.NoError(t, err)
		// fetch the namespace check the `last-applied-space-roles` annotation
		_, err = memberAwait.WaitForNamespace(t, s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
			wait.UntilNamespaceIsActive(),
			wait.UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name)

		t.Run("remove admin binding", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), guestBinding)

			// then
			require.NoError(t, err)
			nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
				wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
				wait.UntilNSTemplateSetHasSpaceRoles(
					wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, "spaceowner"), // "spaceguest" was removed
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name)
		})
	})

	t.Run("set owner user as maintainer instead", func(t *testing.T) {
		// when
		ownerBinding, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SpaceBinding{}).
			Update(ownerBinding.Name, hostAwait.Namespace, func(sb *toolchainv1alpha1.SpaceBinding) {
				// given an appstudio space with `owner` user as an admin of it
				sb.Spec.SpaceRole = "maintainer"
			})
		require.NoError(t, err)

		// then
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
			wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, ownerMUR.Name),
			),
		)
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpace(t, awaitilities, s.Name)
	})

	t.Run("set owner user as contributor instead", func(t *testing.T) {
		// when
		ownerBinding, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SpaceBinding{}).
			Update(ownerBinding.Name, hostAwait.Namespace, func(sb *toolchainv1alpha1.SpaceBinding) {
				// given an appstudio space with `owner` user as an admin of it
				sb.Spec.SpaceRole = "contributor"
			})

		// then
		require.NoError(t, err)
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
			wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["contributor"].TemplateRef, ownerMUR.Name),
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

	user := NewSignupRequest(awaitilities).
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		EnsureMUR().
		Execute(t)
	spaceName := user.Space.Name

	t.Run("to base1nsnoidling tier", func(t *testing.T) {
		// when
		tiers.MoveSpaceToTier(t, hostAwait, spaceName, "base1nsnoidling")

		// then
		VerifyResourcesProvisionedForSpace(t, awaitilities, spaceName)
	})
}

func TestSubSpaces(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	appstudioTier, err := hostAwait.WaitForNSTemplateTier(t, "appstudio")
	require.NoError(t, err)

	t.Run("we create subSpaces in the parentSpace tree and expect roles and usernames to be inherited in NSTemplateSet", func(t *testing.T) {
		// when
		// we have a parentSpace
		user := NewSignupRequest(awaitilities).
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			SpaceTier("appstudio").
			EnsureMUR().
			Execute(t)
		mur := user.MUR
		parentSpace := user.Space
		parentSpaceBindings, err := hostAwait.WaitForSpaceBinding(t, mur.Name, parentSpace.Name)
		require.NoError(t, err)

		// then
		// wait until MUR and Space have been provisioned
		_, parentNSTemplateSet := VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name, wait.UntilSpaceHasStatusTargetCluster(memberAwait.ClusterName))
		parentMUR, err := hostAwait.WaitForMasterUserRecord(t, parentSpaceBindings.Spec.MasterUserRecord)
		require.NoError(t, err)

		// when
		// we also have a subSpace with same tier
		subSpace := CreateSubSpace(t, awaitilities, testspace.WithSpecParentSpace(parentSpace.Name), testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
		//... and we also have a subSubSpace with same tier
		subSubSpace := CreateSubSpace(t, awaitilities, testspace.WithSpecParentSpace(subSpace.Name), testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))

		// then
		// wait until subSubSpace has been provisioned as well
		_, subSubSpaceNSTemplateSet := VerifyResourcesProvisionedForSpace(t, awaitilities, subSubSpace.Name)
		// check that username and role from parentSpace was inherited in the subSubSpace NSTemplateSet
		_, err = memberAwait.WaitForNSTmplSet(t, subSubSpaceNSTemplateSet.Name,
			wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // MUR from parentSpaceBinding is here
			),
		)
		require.NoError(t, err)
		// wait until subSpace has been provisioned as well
		_, subSpaceNSTemplateSet := VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
		// check that username and role from parentSpace was inherited in the subSpace NSTemplateSet
		_, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
			wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // MUR from parentSpaceBinding is here
			),
		)
		require.NoError(t, err)

		// check that user was added to the parentSpace
		_, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
			wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord),
			),
		)
		require.NoError(t, err)

		t.Run("we update role in parentSpaceBinding and expect change to be reflected in subSpaces", func(t *testing.T) {
			// when
			// we update the parentSpace bindings
			parentSpaceBindings, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SpaceBinding{}).
				Update(parentSpaceBindings.Name, hostAwait.Namespace, func(sb *toolchainv1alpha1.SpaceBinding) {
					// the parentSpace role was "downgraded" to maintainer
					sb.Spec.SpaceRole = "maintainer"
				})

			// then
			// downgrade of the usernames is done in parentSpace
			require.NoError(t, err)
			parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
				wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
				wait.UntilNSTemplateSetHasSpaceRoles(
					wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // user was downgraded to maintainer
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)
			// ....and the downgrade to maintainer of the user should be reflected also to the subSpace
			require.NoError(t, err)
			subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
				wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
				wait.UntilNSTemplateSetHasSpaceRoles(
					wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // user was downgraded to maintainer
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
			// ....user should be maintainer also in the subSubSpace
			require.NoError(t, err)
			subSubSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSubSpaceNSTemplateSet.Name,
				wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
				wait.UntilNSTemplateSetHasSpaceRoles(
					wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // user was downgraded to maintainer
				),
			)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSpace(t, awaitilities, subSubSpace.Name)

			t.Run("we add a specific SpaceBinding bound to the subSpace only", func(t *testing.T) {
				// when
				// we create spaceBinding for subSpace
				_, _, subSpaceBinding := CreateMurWithAdminSpaceBindingForSpace(t, awaitilities, subSpace, true)

				// then
				// subSpace should have usernames and roles from parentSpaceBindings+subSpaceBindings
				subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
					wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
					wait.UntilNSTemplateSetHasSpaceRoles(
						wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, subSpaceBinding.Spec.MasterUserRecord),          // new MUR is added as admin
						wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // remains unchanged
					),
				)
				require.NoError(t, err)
				VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
				// then
				// subSubSpace should have usernames and roles from parentSpaceBindings+subSpaceBindings
				subSubSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSubSpaceNSTemplateSet.Name,
					wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
					wait.UntilNSTemplateSetHasSpaceRoles(
						wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, subSpaceBinding.Spec.MasterUserRecord),          // new MUR is added as admin
						wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // remains unchanged
					),
				)
				require.NoError(t, err)
				VerifyResourcesProvisionedForSpace(t, awaitilities, subSubSpace.Name)
				// parentSpace should not have this username role added
				parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
					wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
					wait.UntilNSTemplateSetHasSpaceRoles(
						wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // unchanged
					),
				)
				require.NoError(t, err)
				VerifyResourcesProvisionedForSpace(t, awaitilities, parentSpace.Name)

				t.Run("we override the SpaceBinding from parentSpace", func(t *testing.T) {
					// when
					// we create spaceBinding for subSpace
					// override the parentMUR and give him admin role (was maintainer previously)
					testsupportsb.CreateSpaceBinding(t, awaitilities.Host(), parentMUR, subSpace, "admin")

					// then
					// subSpace should have usernames and roles from parentSpaceBindings+subSpaceBindings
					sortedUsernames := []string{parentSpaceBindings.Spec.MasterUserRecord, subSpaceBinding.Spec.MasterUserRecord}
					sort.Strings(sortedUsernames) // the NSTemplateSet has usernames sorted, this is required so that assertion doesn't fail when order changes
					subSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSpaceNSTemplateSet.Name,
						wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
						wait.UntilNSTemplateSetHasSpaceRoles(
							wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, sortedUsernames...), // parent MUR is added as admin
						),
					)
					require.NoError(t, err)
					VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
					// subSubSpace should have usernames and roles from parentSpaceBindings+subSpaceBindings
					subSubSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSubSpaceNSTemplateSet.Name,
						wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
						wait.UntilNSTemplateSetHasSpaceRoles(
							wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, sortedUsernames...), // parent MUR is added as admin
						),
					)
					require.NoError(t, err)
					VerifyResourcesProvisionedForSpace(t, awaitilities, subSubSpace.Name)
					// parentSpace should not be affected by the change in sub-space
					parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
						wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
						wait.UntilNSTemplateSetHasSpaceRoles(
							wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // unchanged in parentNSTemplateSet
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
							wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
							wait.UntilNSTemplateSetHasSpaceRoles(
								wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // removed admin role user anymore
							),
						)
						require.NoError(t, err)
						VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name)
						// ... also subSubSpace should have one user less
						require.NoError(t, err)
						subSubSpaceNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, subSubSpaceNSTemplateSet.Name,
							wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
							wait.UntilNSTemplateSetHasSpaceRoles(
								wait.SpaceRole(appstudioTier.Spec.SpaceRoles["admin"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // removed admin role user anymore
							),
						)
						require.NoError(t, err)
						VerifyResourcesProvisionedForSpace(t, awaitilities, subSubSpace.Name)
						// parentSpace should not be affected by the change in sub-space
						parentNSTemplateSet, err = memberAwait.WaitForNSTmplSet(t, parentNSTemplateSet.Name,
							wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
							wait.UntilNSTemplateSetHasSpaceRoles(
								wait.SpaceRole(appstudioTier.Spec.SpaceRoles["maintainer"].TemplateRef, parentSpaceBindings.Spec.MasterUserRecord), // unchanged
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

func TestSubSpaceInheritance(t *testing.T) {
	// given
	t.Parallel()
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("we create a subSpace, with disable inheritance and do not expect roles and usernames from the parent to be inherited in NSTemplateSet", func(t *testing.T) {
		// given
		appstudioTier, err := hostAwait.WaitForNSTemplateTier(t, "appstudio")
		require.NoError(t, err)

		// when
		// we have a parentSpace
		t.Logf("Create parent space")
		parentUser := NewSignupRequest(awaitilities).
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			SpaceTier("appstudio").
			EnsureMUR().
			Execute(t)

		user := NewSignupRequest(awaitilities).
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			SpaceTier("appstudio").
			EnsureMUR().
			Execute(t)
		mur := user.MUR

		// when
		// we also have a subSpace with same tier but with disable inheritance
		t.Logf("Create sub space with role: contributor")
		subSpace := testspace.NewSpaceWithGeneratedName(awaitilities.Host().Namespace, util.NewObjectNamePrefix(t),
			testspace.WithSpecParentSpace(parentUser.Space.Name),
			testspace.WithTierName("appstudio"),
			testspace.WithSpecTargetCluster(memberAwait.ClusterName),
			testspace.WithDisableInheritance(true))
		subSpace, subSpaceBindings, err := awaitilities.Host().CreateSpaceAndSpaceBinding(t, mur, subSpace, "contributor")
		require.NoError(t, err)

		// then
		t.Logf("Verify sub space resources")
		_, nsTmplSet := VerifyResourcesProvisionedForSpace(t, awaitilities, subSpace.Name,
			wait.UntilSpaceHasStatusTargetCluster(awaitilities.Member1().ClusterName),
			wait.UntilSpaceHasTier("appstudio"),
		)

		t.Logf("Wait for space binding")
		_, err = awaitilities.Host().WaitForSpaceBinding(t, mur.Name, subSpace.Name,
			wait.UntilSpaceBindingHasMurName(mur.Name),
			wait.UntilSpaceBindingHasSpaceName(subSpace.Name),
			wait.UntilSpaceBindingHasSpaceRole("contributor"),
		)
		require.NoError(t, err)

		t.Logf("Wait for master user")
		subMur, err := hostAwait.WaitForMasterUserRecord(t, subSpaceBindings.Spec.MasterUserRecord)
		require.NoError(t, err)

		t.Logf("Wait for NS template")
		require.NoError(t, err)
		_, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name,
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(appstudioTier.Spec.SpaceRoles["contributor"].TemplateRef, subMur.Name)),
		)
		require.NoError(t, err)
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
