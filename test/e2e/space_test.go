package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
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
		// given
		space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "appstudio", WithTargetCluster(memberAwait.ClusterName))

		// when
		err := hostAwait.Client.Create(context.TODO(), space)

		// then
		// then
		require.NoError(t, err)
		space = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, memberAwait, space.Name, "appstudio")

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
			space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "appstudio")

			// when
			err := hostAwait.Client.Create(context.TODO(), space)

			// then
			require.NoError(t, err)
			space, err = hostAwait.WaitForSpace(space.Name,
				UntilSpaceHasConditions(ProvisioningPending("unspecified target member cluster")),
				UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValuePending))
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
			s := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "appstudio", WithTargetCluster("unknown"))
			s.Spec.TargetCluster = "unknown"

			// when
			err := hostAwait.Client.Create(context.TODO(), s)

			// then
			require.NoError(t, err)
			s, err = hostAwait.WaitForSpace(s.Name, UntilSpaceHasConditions(ProvisioningFailed("unknown target member cluster 'unknown'")))
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
						err = hostAwait.WaitUntilSpaceDeleted(s.Name)
						// then
						require.NoError(t, err)
					})
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
	s := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "appstudio", WithTargetCluster(memberAwait.ClusterName))

	// when
	err := hostAwait.CreateWithCleanup(context.TODO(), s)

	// then
	require.NoError(t, err)
	nsTmplSet, err := memberAwait.WaitForNSTmplSet(s.Name, UntilNSTemplateSetHasConditions(Provisioned()))
	require.NoError(t, err)
	// wait until NSTemplateSet has been created and Space is in `Ready` status
	s = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, memberAwait, s.Name, "appstudio")

	// given
	adminTierTmpl := NewTierTemplate(t, hostAwait.Namespace, "space-role-admin-123456", "space-role-admin", "appstudio", "123456", []byte(spaceAdminTmpl))
	err = hostAwait.CreateWithCleanup(context.TODO(), adminTierTmpl)
	require.NoError(t, err)

	t.Run("add admin role and bindings", func(t *testing.T) {

		// when
		nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
			nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
				{
					TemplateRef: adminTierTmpl.Name,
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

		t.Run("remove admin binding", func(t *testing.T) {
			// when
			nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
				nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
					{
						TemplateRef: adminTierTmpl.Name,
						Usernames:   []string{"user1"}, // removed `user2`
					},
				}
			})

			// then
			require.NoError(t, err)
			_, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasConditions(Provisioned()))
			require.NoError(t, err)

			// check that role and role bindings were created
			ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
				UntilNamespaceIsActive(),
				UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
			require.NoError(t, err)
			_, err = memberAwait.WaitForRole(ns, "space-admin") // unchanged
			assert.NoError(t, err)
			_, err = memberAwait.WaitForRoleBinding(ns, "user1-space-admin") // unchanged
			assert.NoError(t, err)
			err = memberAwait.WaitUntilRoleBindingDeleted(ns, "user2-space-admin") // deleted
			assert.NoError(t, err)

			t.Run("replace with viewer role and binding", func(t *testing.T) {
				// given
				viewerTierTmpl := NewTierTemplate(t, hostAwait.Namespace, "space-role-viewer-123456", "space-role-viewer", "appstudio", "123456", []byte(spaceViewerTmpl))
				err := hostAwait.CreateWithCleanup(context.TODO(), viewerTierTmpl)
				require.NoError(t, err)

				// when
				nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
					nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
						{
							TemplateRef: viewerTierTmpl.Name,
							Usernames:   []string{"user3"},
						},
					}
				})

				// then
				require.NoError(t, err)
				_, err = memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasConditions(Provisioned()))
				require.NoError(t, err)
				ns, err := memberAwait.WaitForNamespace(s.Name, nsTmplSet.Spec.Namespaces[0].TemplateRef, "appstudio",
					UntilNamespaceIsActive(),
					UntilHasLastAppliedSpaceRoles(nsTmplSet.Spec.SpaceRoles))
				require.NoError(t, err)

				// 'admin' role and role binding should have been removed
				err = memberAwait.WaitUntilRoleDeleted(ns, "space-admin") // deleted
				assert.NoError(t, err)
				err = memberAwait.WaitUntilRoleBindingDeleted(ns, "user2-space-admin") // deleted
				assert.NoError(t, err)
				// check that role and role bindings were created
				// check that the `admin` role and role bindings were created
				_, err = memberAwait.WaitForRole(ns, "space-viewer")
				assert.NoError(t, err)
				_, err = memberAwait.WaitForRoleBinding(ns, "user3-space-viewer")
				assert.NoError(t, err)

			})
		})
	})

	t.Run("update space roles templates", func(t *testing.T) {

		t.Run("with other content", func(t *testing.T) {

			// when
			nsTmplSet, err = memberAwait.UpdateNSTemplateSet(nsTmplSet.Name, func(nsTmplSet *toolchainv1alpha1.NSTemplateSet) {
				nsTmplSet.Spec.SpaceRoles = []toolchainv1alpha1.NSTemplateSetSpaceRole{
					{
						TemplateRef: adminTierTmpl.Name,
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
						TemplateRef: adminTierTmpl.Name,
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
	})
}

func TestPromoteSpace(t *testing.T) {

	// given
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", WithTargetCluster(memberAwait.ClusterName))

	// when
	err := hostAwait.CreateWithCleanup(context.TODO(), space)

	// then
	require.NoError(t, err)

	space = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, memberAwait, space.Name, "base")

	t.Run("to advanced tier", func(t *testing.T) {
		// given
		ctr := NewChangeTierRequest(hostAwait.Namespace, space.Name, "advanced")

		// when
		err = hostAwait.Client.Create(context.TODO(), ctr)

		// then
		require.NoError(t, err)
		_, err := hostAwait.WaitForChangeTierRequest(ctr.Name, toBeComplete)
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

	t.Run("to no other cluster", func(t *testing.T) {
		// given
		space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", WithTargetCluster(member1Await.ClusterName))
		err := hostAwait.CreateWithCleanup(context.TODO(), space)
		require.NoError(t, err)
		// wait until Space has been provisioned on member-1
		VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, member1Await, space.Name, "base")

		// when
		space, err = hostAwait.UpdateSpace(space.Name, func(s *toolchainv1alpha1.Space) {
			s.Spec.TargetCluster = ""
		})
		require.NoError(t, err)

		// then
		_, err = hostAwait.WaitForSpace(space.Name, UntilSpaceHasConditions(ProvisioningPending("unspecified target member cluster")))
		require.NoError(t, err)
		err = member1Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet to be delete on member-1 cluster
		require.NoError(t, err)
		err = member2Await.WaitUntilNSTemplateSetDeleted(space.Name) // expect NSTemplateSet is not created in member-2 cluster
		require.NoError(t, err)

	})

	t.Run("to another cluster", func(t *testing.T) {
		// given
		space := NewSpace(hostAwait.Namespace, GenerateName("oddity"), "base", WithTargetCluster(member1Await.ClusterName))
		err := hostAwait.CreateWithCleanup(context.TODO(), space)
		require.NoError(t, err)
		// wait until Space has been provisioned on member-1
		space = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, member1Await, space.Name, "base")

		// when
		space, err = hostAwait.UpdateSpace(space.Name, func(s *toolchainv1alpha1.Space) {
			s.Spec.TargetCluster = member2Await.ClusterName
		})
		require.NoError(t, err)

		// then
		// wait until Space has been provisioned on member-1
		space = VerifyResourcesProvisionedForSpaceWithTier(t, hostAwait, member2Await, space.Name, "base")
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

const spaceViewerTmpl = `apiVersion: template.openshift.io/v1
kind: Template
metadata:
  name: space-viewer-template
objects:
- apiVersion: rbac.authorization.k8s.io/v1
  kind: Role
  metadata:
    name: space-viewer
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
    name: ${USERNAME}-space-viewer
    namespace: ${NAMESPACE}
  roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: Role
    name: space-viewer
  subjects:
    - kind: User
      name: ${USERNAME}
parameters:
- name: NAMESPACE
  required: true
- name: USERNAME
  value: johnsmith
`
