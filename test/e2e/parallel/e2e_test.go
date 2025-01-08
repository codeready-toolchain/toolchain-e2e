package parallel

import (
	"context"
	"sync"
	"testing"
	"time"

	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestE2EFlow(t *testing.T) {
	t.Parallel()
	// given
	// full flow from usersignup with approval down to namespaces creation
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	consoleURL := memberAwait.GetConsoleURL(t)
	// host and member cluster statuses should be available at this point
	t.Run("verify cluster statuses are valid", func(t *testing.T) {
		t.Run("verify member cluster status", func(t *testing.T) {
			VerifyMemberStatus(t, memberAwait, consoleURL)
		})

		t.Run("verify overall toolchain status", func(t *testing.T) {
			VerifyToolchainStatus(t, hostAwait, memberAwait)
		})
	})

	t.Run("verify MemberOperatorConfigs synced from ToolchainConfig to member clusters", func(t *testing.T) {
		currentConfig := hostAwait.GetToolchainConfig(t)
		expectedMemberConfiguration := currentConfig.Spec.Members.Default

		t.Run("verify ToolchainConfig has synced status", func(t *testing.T) {
			VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasSyncedStatus(wait.ToolchainConfigSyncComplete()))
		})
		t.Run("verify MemberOperatorConfig was synced to member 1", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, hostAwait, memberAwait, wait.UntilMemberConfigMatches(expectedMemberConfiguration))
		})
		t.Run("verify MemberOperatorConfig was synced to member 2", func(t *testing.T) {
			// Expected to be the same as for member-1 but with Webhook deployment disabled.
			// We can't deploy the same cluster-scoped webhook twice (member-1 and member-2) in the same physical cluster.
			member2ExpectedConfig := expectedMemberConfiguration.DeepCopy()
			f := false
			member2ExpectedConfig.Webhook.Deploy = &f
			member2ExpectedConfig.Webhook.Secret = nil
			VerifyMemberOperatorConfig(t, hostAwait, memberAwait2, wait.UntilMemberConfigMatches(*member2ExpectedConfig))
		})
	})

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, awaitilities, awaitilities.Member1(), 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnUser := NewSignupRequest(awaitilities).
		Username(johnsmithName).
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		SpaceTier("base").
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	johnSignup := johnUser.UserSignup

	extrajohnName := "extrajohn"
	johnExtraUser := NewSignupRequest(awaitilities).
		Username(extrajohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	johnExtraSignup := johnExtraUser.UserSignup

	targetedJohnName := "targetedjohn"
	targetedJohnUser := NewSignupRequest(awaitilities).
		Username(targetedJohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait2).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	targetedJohnSignup := targetedJohnUser.UserSignup

	originalSubJohnName := "originalsubjohn"
	originalSubJohnClaim := "originalsub:john"
	originalSubJohnUser := NewSignupRequest(awaitilities).
		Username(originalSubJohnName).
		OriginalSub(originalSubJohnClaim).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	originalSubJohnSignup := originalSubJohnUser.UserSignup
	originalSubJohnSpace := originalSubJohnUser.Space

	// Confirm the originalSub property has been set during signup
	require.Equal(t, originalSubJohnClaim, originalSubJohnSignup.Spec.IdentityClaims.OriginalSub)

	VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, targetedJohnSignup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, originalSubJohnSignup)

	t.Run("try to break UserAccount", func(t *testing.T) {
		t.Run("delete user and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			err = memberAwait.Client.Delete(context.TODO(), user)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
		})

		t.Run("delete identity and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: identitypkg.NewIdentityNamingStandard(
				johnSignup.Spec.IdentityClaims.Sub, "rhd").IdentityName()}, identity)
			require.NoError(t, err)

			// when
			err = memberAwait.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
		})

		t.Run("delete user mapping and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			_, err = wait.For(t, memberAwait.Awaitility, &userv1.User{}).
				Update(user.Name, memberAwait.Namespace, func(u *userv1.User) {
					u.Identities = []string{}
				})

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
		})

		t.Run("delete identity mapping and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: identitypkg.NewIdentityNamingStandard(
				johnSignup.Spec.IdentityClaims.Sub, "rhd").IdentityName()}, identity)
			require.NoError(t, err)

			// when
			_, err = wait.For(t, memberAwait.Awaitility, &userv1.Identity{}).
				Update(identity.Name, memberAwait.Namespace, func(i *userv1.Identity) {
					i.User = corev1.ObjectReference{Name: "", UID: ""}
				})

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 2)
			templateRefs := tiers.GetTemplateRefs(t, hostAwait, "base")
			for _, ref := range templateRefs.Namespaces {
				ns, err := memberAwait.WaitForNamespace(t, johnSignup.Spec.IdentityClaims.PreferredUsername, ref, "base", wait.UntilNamespaceIsActive())
				require.NoError(t, err)
				namespaces = append(namespaces, ns)
			}
			// when
			for _, ns := range namespaces {
				err := memberAwait.Client.Delete(context.TODO(), ns)
				require.NoError(t, err)
			}

			// then
			// wait for the namespaces to be re-created before validating all other resources to avoid race condition
			for _, ref := range templateRefs.Namespaces {
				_, err := memberAwait.WaitForNamespace(t, johnSignup.Spec.IdentityClaims.PreferredUsername, ref, "base", wait.UntilNamespaceIsActive())
				require.NoError(t, err)
			}
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
		})

		t.Run("delete useraccount and expect recreation", func(t *testing.T) {
			// given
			ua, err := memberAwait.WaitForUserAccount(t, johnSignup.Status.CompliantUsername)
			require.NoError(t, err)
			originalCreationTimestamp := ua.CreationTimestamp

			// when deleting the user account
			deletePolicy := metav1.DeletePropagationForeground
			deleteOpts := &client.DeleteOptions{
				PropagationPolicy: &deletePolicy,
			}
			err = memberAwait.Client.Delete(context.TODO(), ua, deleteOpts)
			require.NoError(t, err)
			// useraccount deletion happens very quickly so instead of waiting for the useraccount to
			// have a deletion timestamp, ensure the creation timestamp is updated to a newer timestamp
			_, err = memberAwait.WaitForUserAccount(t, ua.Name, wait.UntilUserAccountIsCreatedAfter(originalCreationTimestamp))
			require.NoError(t, err)

			// then verify the recreated user account
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, johnSignup, "deactivate30", "base")
		})
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		VerifyMultipleSignups(t, awaitilities, signups)
	})

	t.Run("verify Space is not deleted if namespace is not deleted", func(t *testing.T) {
		// given
		laraUserName := "laracroft"
		userNamespace := "laracroft-dev"
		cmName := "test-useraccount-delete-1"

		laraUser := NewSignupRequest(awaitilities).
			Username(laraUserName).
			Email("laracroft@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)
		laraSignup := laraUser.UserSignup

		require.Equal(t, "laracroft", laraSignup.Status.CompliantUsername)

		VerifyResourcesProvisionedForSignup(t, awaitilities, laraSignup)

		VerifySpaceBinding(t, hostAwait, laraUserName, laraUserName, "admin")

		// Create a configmap with finalizer in user's namespace, which will block the deletion of namespace.
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: userNamespace,
				Finalizers: []string{
					"test/finalizer.toolchain.e2e.tests",
				},
			},
			Data: map[string]string{
				"video_game": "Tomb Raider",
			},
		}
		err := memberAwait.Client.Create(context.TODO(), cm)
		require.NoError(t, err)
		cm, err = memberAwait.WaitForConfigMap(t, userNamespace, cmName)
		require.NoError(t, err)
		require.NotEmpty(t, cm)

		// ensure that the finalizer is removed in all cases, so the cleanup logic
		// is not blocked and can delete the Space
		var removeOnce sync.Once
		removeFinalizer := func() {
			removeOnce.Do(func() {
				_, err = wait.For(t, memberAwait.Awaitility, &corev1.ConfigMap{}).
					Update(cmName, cm.Namespace, func(cm *corev1.ConfigMap) {
						cm.Finalizers = nil
					})
				require.NoError(t, err)
			})
		}
		defer removeFinalizer()

		deletePolicy := metav1.DeletePropagationForeground
		deleteOpts := &client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		// when deleting the userSignup
		err = hostAwait.Client.Delete(context.TODO(), laraSignup, deleteOpts)
		require.NoError(t, err)

		// then nothing should be deleted yet (because of the CM with its own finalizer)
		nsTmplSet, err := memberAwait.WaitForNSTmplSet(t, laraUserName, wait.UntilNSTemplateSetIsBeingDeleted(), wait.UntilNSTemplateSetHasConditions(wait.TerminatingNSTemplateSet()))
		require.NoError(t, err)
		require.NotEmpty(t, nsTmplSet)

		// Check that namespace is not deleted and is in Terminating state after 10sec
		_, err = memberAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForNamespaceInTerminating(t, userNamespace)
		require.NoError(t, err)

		nsTmplSet, err = memberAwait.WaitForNSTmplSet(t, laraUserName, wait.UntilNSTemplateSetIsBeingDeleted(), wait.UntilNSTemplateSetHasConditions(wait.TerminatingNSTemplateSet()))
		require.NoError(t, err)
		require.NotEmpty(t, nsTmplSet)

		// UserAccount should be deleted when MUR is deleted
		err = memberAwait.WaitUntilUserAccountDeleted(t, laraUserName)
		require.NoError(t, err)

		// MUR should be deleted when UserSignup is deleted
		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, laraUserName)
		require.NoError(t, err)

		// UserSignup should be deleted even though Space and NSTemplateSet are stuck deleting so that
		// the behaviour is consistent for both AppStudio & DevSandbox
		err = hostAwait.WaitUntilUserSignupDeleted(t, laraSignup.Name)
		require.NoError(t, err)

		// space should be stuck terminating
		space, err := hostAwait.WaitForSpace(t, laraUserName, wait.UntilSpaceIsBeingDeleted(), wait.UntilSpaceHasConditions(wait.TerminatingSpace()))
		require.NoError(t, err)
		require.NotEmpty(t, space)

		t.Run("remove finalizer", func(t *testing.T) {
			// when removing the finalizer from the CM
			removeFinalizer()

			// then check remaining resources are deleted
			err = memberAwait.WaitUntilNamespaceDeleted(t, laraUserName, "dev")
			require.NoError(t, err, "laracroft-dev namespace is not deleted")

			err = memberAwait.WaitUntilNSTemplateSetDeleted(t, laraUserName)
			require.NoError(t, err, "NSTemplateSet is not deleted")

			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, laraUserName)
			require.NoError(t, err)
		})
	})

	t.Run("delete namespaced scoped resources of users and expect recreation", func(t *testing.T) {
		user := NewSignupRequest(awaitilities).
			Username("wonderwoman").
			Email("wonderwoman@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			SpaceTier("base").
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)
		userSignup := user.UserSignup
		devNs := corev1.Namespace{}
		err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "wonderwoman-dev"}, &devNs)
		require.NoError(t, err)

		stageNs := corev1.Namespace{}
		err = memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "wonderwoman-stage"}, &stageNs)
		require.NoError(t, err)

		// roles and role bindings defined in the namespace templates
		t.Run("namespace role accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, devNs, "exec-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("namespace rolebinding accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, devNs, "crtadmin-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("namespace role accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, stageNs, "exec-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("namespace rolebinding accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, stageNs, "crtadmin-pods")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		// roles and role bindings defined in the spacerole templates
		t.Run("space role accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, devNs, "rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("space rolebinding accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, devNs, "wonderwoman-rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("space role accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, stageNs, "rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("space rolebinding accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, stageNs, "wonderwoman-rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, userSignup, "deactivate30", "base")
		})
	})

	t.Run("delete usersignup and expect all resources to be deleted", func(t *testing.T) {
		// given
		johnSignup, err := hostAwait.WaitForUserSignup(t, johnSignup.Name)
		require.NoError(t, err)

		// when
		err = hostAwait.Client.Delete(context.TODO(), johnSignup)

		// then
		require.NoError(t, err)
		t.Logf("usersignup '%s' deleted (resource name='%s')", johnsmithName, johnSignup.Name)

		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, johnsmithName)
		require.NoError(t, err, "MasterUserRecord is not deleted")

		err = memberAwait.WaitUntilUserAccountDeleted(t, johnsmithName)
		require.NoError(t, err, "UserAccount is not deleted")

		err = memberAwait.WaitUntilUserDeleted(t, johnsmithName)
		require.NoError(t, err, "User is not deleted")

		err = memberAwait.WaitUntilIdentityDeleted(t, johnsmithName)
		require.NoError(t, err, "Identity is not deleted")

		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, johnsmithName)
		require.NoError(t, err)

		err = memberAwait.WaitUntilNSTemplateSetDeleted(t, johnsmithName)
		require.NoError(t, err, "NSTemplateSet is not deleted")

		err = memberAwait.WaitUntilClusterResourceQuotasDeleted(t, johnsmithName)
		require.NoError(t, err, "ClusterResourceQuotas were not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(t, johnsmithName, "dev")
		require.NoError(t, err, "johnsmith-dev namespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(t, johnsmithName, "stage")
		require.NoError(t, err, "johnsmith-stage namespace is not deleted")

		// also, verify that other user's resource are left intact
		VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup)
	})

	t.Run("deactivate UserSignup and ensure that all user and identity resources are deleted", func(t *testing.T) {
		// First confirm that there are actually multiple identities for the UserSignup in question, using the
		// owner label to locate them

		// First, find the MUR
		mur, err := hostAwait.WaitForMasterUserRecord(t, originalSubJohnSpace.Name)
		require.NoError(t, err)

		memberAwait := GetMurTargetMember(t, awaitilities, mur)

		// Then locate the UserAccount
		userAccount, err := memberAwait.WaitForUserAccount(t, mur.Name)
		require.NoError(t, err)
		require.NotNil(t, userAccount)

		// Once we have the UserAccount we can lookup the identities using the owner label
		identityList := &userv1.IdentityList{}
		err = memberAwait.Client.List(context.TODO(), identityList, listByOwnerLabel(userAccount.Name))
		require.NoError(t, err)

		// We should have exactly two identities
		require.Len(t, identityList.Items, 2)

		// Also lookup the user
		userList := &userv1.UserList{}
		err = memberAwait.Client.List(context.TODO(), userList, listByOwnerLabel(userAccount.Name))
		require.NoError(t, err)

		// We should have exactly one user
		require.Len(t, userList.Items, 1)

		// Now deactivate the UserSignup
		userSignup, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(originalSubJohnSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(us, true)
				})
		require.NoError(t, err)

		// Wait until the UserSignup is deactivated
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ManuallyDeactivated())...))
		require.NoError(t, err)

		// Ensure the first identity is deleted
		err = memberAwait.WaitUntilIdentityDeleted(t, identityList.Items[0].Name)
		require.NoError(t, err)

		// Ensure the second identity is deleted
		err = memberAwait.WaitUntilIdentityDeleted(t, identityList.Items[1].Name)
		require.NoError(t, err)

		// Ensure the user is deleted
		err = memberAwait.WaitUntilUserDeleted(t, userList.Items[0].Name)
		require.NoError(t, err)
	})
}

func listByOwnerLabel(owner string) client.ListOption {
	labels := map[string]string{
		toolchainv1alpha1.OwnerLabelKey: owner,
	}
	return client.MatchingLabels(labels)
}
