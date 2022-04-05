package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateSpace(t *testing.T) {
	// given

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("create space", func(t *testing.T) {
		// given & when & then
		space := CreateAndVerifySpace(t, awaitilities, WithTierName("appstudio"), WithTargetCluster(memberAwait))

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

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// given
	appstudioTier, err := hostAwait.WaitForNSTemplateTier("appstudio", UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
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
	s, ownerBinding := CreateSpaceWithBinding(t, awaitilities, ownerMUR, WithTargetCluster(awaitilities.Member1()), WithTierName("appstudio"))

	// then
	nsTmplSet, err := memberAwait.WaitForNSTmplSet(s.Name,
		UntilNSTemplateSetHasConditions(Provisioned()),
		UntilNSTemplateSetHasSpaceRoles([]toolchainv1alpha1.NSTemplateSetSpaceRole{
			{
				TemplateRef: appstudioTier.Spec.SpaceRoles["admin"].TemplateRef,
				Usernames:   []string{"spaceowner"},
			},
		}),
	)
	require.NoError(t, err)
	nsTmplSet, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name,
		UntilNSTemplateSetHasConditions(Provisioned()),
		UntilNSTemplateSetHasSpaceRoles([]toolchainv1alpha1.NSTemplateSetSpaceRole{
			{
				TemplateRef: appstudioTier.Spec.SpaceRoles["admin"].TemplateRef,
				Usernames:   []string{ownerMUR.Name},
			},
		}),
	)
	require.NoError(t, err)
	// fetch the namespace check the `last-applied-space-roles` annotation
	ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
		UntilNamespaceIsActive(),
		UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
	require.NoError(t, err)
	// check that the `appstudio-user-actions` role and `appstudio-${USERNAME}-user-actions` rolebinding for `spaceowner` were created
	_, err = memberAwait.WaitForRole(ns, "appstudio-user-actions")
	assert.NoError(t, err)
	_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-user-actions", ownerMUR.Name))
	assert.NoError(t, err)
	// also, check that the `appstudio-${USERNAME}-view` rolebinding (associated with the buit-in `view` clusterrole) for `spaceowner` was created
	_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-view", ownerMUR.Name))
	assert.NoError(t, err)

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
		guestBinding := NewSpaceBinding(guestMUR, s, "admin")
		err := hostAwait.CreateWithCleanup(context.TODO(), guestBinding)

		// then
		require.NoError(t, err)
		nsTmplSet, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name,
			UntilNSTemplateSetHasConditions(Provisioned()),
			UntilNSTemplateSetHasSpaceRoles([]toolchainv1alpha1.NSTemplateSetSpaceRole{
				{
					TemplateRef: appstudioTier.Spec.SpaceRoles["admin"].TemplateRef,
					Usernames:   []string{"spaceowner", "spaceguest"},
				},
			}),
		)
		require.NoError(t, err)
		// fetch the namespace check the `last-applied-space-roles` annotation
		ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
			UntilNamespaceIsActive(),
			UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
		require.NoError(t, err)
		// check that the `appstudio-user-actions` role and `appstudio-${USERNAME}-user-actions` rolebinding for `spaceowner` still exist
		_, err = memberAwait.WaitForRole(ns, "appstudio-user-actions")
		assert.NoError(t, err)
		_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-user-actions", ownerMUR.Name))
		assert.NoError(t, err)
		// also, check that the `appstudio-${USERNAME}-view` rolebinding (associated with the buit-in `view` clusterrole) for `spaceowner` still exist
		_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-view", ownerMUR.Name))
		assert.NoError(t, err)
		// also, check that the `appstudio-${USERNAME}-user-actions` rolebinding for `spaceguest` was created
		_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-user-actions", guestMUR.Name))
		assert.NoError(t, err)
		// also, check that the `appstudio-${USERNAME}-view` rolebinding (associated with the buit-in `view` clusterrole) for `spaceguest` was created
		_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-view", guestMUR.Name))
		assert.NoError(t, err)

		t.Run("remove admin binding", func(t *testing.T) {
			// when
			err := hostAwait.Client.Delete(context.TODO(), guestBinding)

			// then
			assert.NoError(t, err)
			// check that the `appstudio-user-actions` role and `appstudio-${USERNAME}-user-actions` rolebinding for `spaceowner` still exist
			_, err = memberAwait.WaitForRole(ns, "appstudio-user-actions")
			assert.NoError(t, err)
			_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-user-actions", ownerMUR.Name))
			assert.NoError(t, err)
			// also, check that the `appstudio-${USERNAME}-view` rolebinding (associated with the buit-in `view` clusterrole) for `spaceowner` still exist
			_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-view", ownerMUR.Name))
			assert.NoError(t, err)
			// also, check that the `appstudio-${USERNAME}-user-actions` rolebinding for `spaceguest` was deleted
			err = memberAwait.WaitUntilRoleBindingDeleted(ns, fmt.Sprintf("appstudio-%s-user-actions", guestMUR.Name))
			assert.NoError(t, err)
			// also, check that the `appstudio-${USERNAME}-view` rolebinding (associated with the buit-in `view` clusterrole) for `spaceguest` was deleted
			err = memberAwait.WaitUntilRoleBindingDeleted(ns, fmt.Sprintf("appstudio-%s-view", guestMUR.Name))
			assert.NoError(t, err)

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
			UntilNSTemplateSetHasSpaceRoles([]toolchainv1alpha1.NSTemplateSetSpaceRole{
				{
					TemplateRef: appstudioTier.Spec.SpaceRoles["viewer"].TemplateRef,
					Usernames:   []string{ownerMUR.Name},
				},
			}),
		)
		require.NoError(t, err)
		// check that the `appstudio-user-actions` role and `appstudio-${USERNAME}-user-actions` rolebinding for `spaceowner` were deleted
		// (role should be removed since no user is associated with it)
		err = memberAwait.WaitUntilRoleDeleted(ns, "appstudio-user-actions")
		assert.NoError(t, err)
		err = memberAwait.WaitUntilRoleBindingDeleted(ns, fmt.Sprintf("appstudio-%s-user-actions", ownerMUR.Name))
		assert.NoError(t, err)
		// also, check that the `appstudio-${USERNAME}-view` rolebinding (associated with the buit-in `view` clusterrole) for `spaceowner` still exist
		_, err = memberAwait.WaitForRoleBinding(ns, fmt.Sprintf("appstudio-%s-view", ownerMUR.Name))
		assert.NoError(t, err)

	})
}

func TestUpdateSpaceRoleTemplates(t *testing.T) {

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// given
	appstudioTier, err := hostAwait.WaitForNSTemplateTier("appstudio", UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
	require.NoError(t, err)
	adminRole, found := appstudioTier.Spec.SpaceRoles["admin"]
	require.True(t, found)
	_, ownerAdmin := NewSignupRequest(t, awaitilities).
		Username("guestadmin").
		Email("owneradmin@redhat.com").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().
		Resources()
	s, _ := CreateSpaceWithBinding(t, awaitilities, ownerAdmin, WithTierName("appstudio"))
	nsTmplSet, err := memberAwait.WaitForNSTmplSet(s.Name,
		UntilNSTemplateSetHasConditions(Provisioned()),
		UntilNSTemplateSetHasSpaceRoles([]toolchainv1alpha1.NSTemplateSetSpaceRole{
			{
				TemplateRef: appstudioTier.Spec.SpaceRoles["admin"].TemplateRef,
				Usernames:   []string{"user1"},
			},
		}),
	)

	t.Run("with other content", func(t *testing.T) {

		// when
		nsTmplSet, err := memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
			nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
				{
					TemplateRef: adminRole.TemplateRef,
					Usernames:   []string{"user1", "user2"},
				},
			}
		})

		// then
		require.NoError(t, err)
		_, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		// fetch the namespace check the `last-applied-space-roles` annotation
		ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
			UntilNamespaceIsActive(),
			UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
		require.NoError(t, err)
		// check that the `admin` role and role bindings were created
		_, err = memberAwait.WaitForRole(ns, "space-admin")
		assert.NoError(t, err)
		_, err = memberAwait.WaitForRoleBinding(ns, "user1-space-admin")
		assert.NoError(t, err)
		_, err = memberAwait.WaitForRoleBinding(ns, "user2-space-admin")
		assert.NoError(t, err)

		t.Run("update templateref", func(t *testing.T) {
			// given
			adminTierTmpl2 := NewTierTemplate(t, hostAwait.Namespace, "space-role-admin-2-123456", "space-role-admin-2", "appstudio", "123456", []byte(spaceAdminTmpl2))
			err := hostAwait.CreateWithCleanup(context.TODO(), adminTierTmpl2)
			require.NoError(t, err)
			// when
			nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
				nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
					{
						TemplateRef: adminTierTmpl2.Name,
						Usernames:   []string{"user1", "user2"},
					},
				}
			})
			// then
			require.NoError(t, err)
			_, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasConditions(Provisioned()))
			require.NoError(t, err)
			// fetch the namespace check the `last-applied-space-roles` annotation
			ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
				UntilNamespaceIsActive(),
				UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
			require.NoError(t, err)
			// check that the new `admin 2` role and role bindings were created
			_, err = memberAwait.WaitForRole(ns, "space-admin-2")
			assert.NoError(t, err)
			_, err = memberAwait.WaitForRoleBinding(ns, "user1-space-admin-2")
			assert.NoError(t, err)
			_, err = memberAwait.WaitForRoleBinding(ns, "user2-space-admin-2")
			assert.NoError(t, err)
			// also, check that the old `admin` role and role bindings were deleted
			err = memberAwait.WaitUntilRoleDeleted(ns, "space-admin")
			assert.NoError(t, err)
			err = memberAwait.WaitUntilRoleBindingDeleted(ns, "user1-space-admin")
			assert.NoError(t, err)
			err = memberAwait.WaitUntilRoleBindingDeleted(ns, "user2-space-admin")
			assert.NoError(t, err)

		})
	})

	t.Run("with same content", func(t *testing.T) {

		// when
		nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
			nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
				{
					TemplateRef: adminRole.TemplateRef,
					Usernames:   []string{"user1", "user2"},
				},
			}
		})

		// then
		require.NoError(t, err)
		_, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasConditions(Provisioned()))
		require.NoError(t, err)
		// fetch the namespace check the `last-applied-space-roles` annotation
		ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
			UntilNamespaceIsActive(),
			UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
		require.NoError(t, err)
		// check that the `admin` role and role bindings were created
		_, err = memberAwait.WaitForRole(ns, "space-admin")
		assert.NoError(t, err)
		_, err = memberAwait.WaitForRoleBinding(ns, "user1-space-admin")
		assert.NoError(t, err)
		_, err = memberAwait.WaitForRoleBinding(ns, "user2-space-admin")
		assert.NoError(t, err)

		t.Run("update templateref", func(t *testing.T) {
			// given `adminTierTmpl3` has the same content (role & rolebindings) as `adminTierTmpl`, but the templateref is different
			adminTierTmpl3 := NewTierTemplate(t, hostAwait.Namespace, "space-role-admin-3-123456", "space-role-admin-3", "appstudio", "123456", []byte(spaceAdminTmpl))
			err := hostAwait.CreateWithCleanup(context.TODO(), adminTierTmpl3)
			require.NoError(t, err)
			// when
			nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
				nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
					{
						TemplateRef: adminTierTmpl3.Name,
						Usernames:   []string{"user1", "user2"},
					},
				}
			})
			// then
			require.NoError(t, err)
			_, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasConditions(Provisioned()))
			require.NoError(t, err)
			// fetch the namespace check the `last-applied-space-roles` annotation
			ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
				UntilNamespaceIsActive(),
				UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
			require.NoError(t, err)
			// check that the existing `admin` role and role bindings still exist
			_, err = memberAwait.WaitForRole(ns, "space-admin")
			assert.NoError(t, err)
			_, err = memberAwait.WaitForRoleBinding(ns, "user1-space-admin")
			assert.NoError(t, err)
			_, err = memberAwait.WaitForRoleBinding(ns, "user2-space-admin")
			assert.NoError(t, err)

		})
	})
}

func TestPromoteSpace(t *testing.T) {

	// given
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	//  when & then
	space := CreateAndVerifySpace(t, awaitilities, WithTierName("base"), WithTargetCluster(memberAwait))

	t.Run("to advanced tier", func(t *testing.T) {
		// given
		ctr := tiers.NewChangeTierRequest(hostAwait.Namespace, space.Name, "advanced")

		// when
		err := hostAwait.Client.Create(context.TODO(), ctr)

		// then
		require.NoError(t, err)
		_, err = hostAwait.WaitForChangeTierRequest(ctr.Name, toBeComplete)
		require.NoError(t, err)
		VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, memberAwait, space.Name, "advanced")
	})
}

func TestRetargetSpace(t *testing.T) {
	// given
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	member1Await := awaitilities.Member1()
	member2Await := awaitilities.Member2()

	// wait until Space has been provisioned on member-1
	space := CreateAndVerifySpace(t, awaitilities, WithTierName("base"), WithTargetCluster(member1Await))

	// when
	space, err := hostAwait.UpdateSpace(space.Name, func(s *toolchainv1alpha1.Space) {
		s.Spec.TargetCluster = member2Await.ClusterName
	})
	require.NoError(t, err)

	// then
	// wait until Space has been provisioned on member-1
	space = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, member2Await, space.Name, "base")
	err = member1Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet to be delete on member-1 cluster
	require.NoError(t, err)
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

const spaceAdminTmpl = `apiVersion: template.openshift.io/v1
kind: Template
metadata:
  labels:
  name: space-admin-template
objects:
- apiVersion: rbac.authorization.k8s.io/v1
  kind: Role
  metadata:
    name: space-admin
    namespace: ${NAMESPACE}
  rules:
    # examples
    - apiGroups:
        - ""
      resources:
        - "secrets"
        - "serviceaccounts"
      verbs:
        - get
        - list
- apiVersion: rbac.authorization.k8s.io/v1
  kind: RoleBinding
  metadata:
    name: ${USERNAME}-space-admin
    namespace: ${NAMESPACE}
  roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: Role
    name: space-admin
  subjects:
    - kind: User
      name: ${USERNAME}
parameters:
- name: NAMESPACE
  required: true
- name: USERNAME
  value: johnsmith
`

const spaceAdminTmpl2 = `apiVersion: template.openshift.io/v1
kind: Template
metadata:
  labels:
  name: space-admin-template-2
objects:
- apiVersion: rbac.authorization.k8s.io/v1
  kind: Role
  metadata:
    name: space-admin-2
    namespace: ${NAMESPACE}
  rules:
    # examples
    - apiGroups:
        - ""
      resources:
        - "secrets"
        - "serviceaccounts"
      verbs:
        - get
        - list
- apiVersion: rbac.authorization.k8s.io/v1
  kind: RoleBinding
  metadata:
    name: ${USERNAME}-space-admin-2
    namespace: ${NAMESPACE}
  roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: Role
    name: space-admin-2
  subjects:
    - kind: User
      name: ${USERNAME}
parameters:
- name: NAMESPACE
  required: true
- name: USERNAME
  value: johnsmith
`
