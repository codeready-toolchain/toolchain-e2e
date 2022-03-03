package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	consoleURL := memberAwait.GetConsoleURL()
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
		currentConfig := hostAwait.GetToolchainConfig()
		expectedMemberConfiguration := currentConfig.Spec.Members.Default

		t.Run("verify ToolchainConfig has synced status", func(t *testing.T) {
			VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasSyncedStatus(ToolchainConfigSyncComplete()))
		})
		t.Run("verify MemberOperatorConfig was synced to member 1", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, hostAwait, memberAwait, wait.UntilMemberConfigMatches(expectedMemberConfiguration))
		})
		t.Run("verify MemberOperatorConfig was synced to member 2", func(t *testing.T) {
			member2ExpectedConfig := testconfig.NewMemberOperatorConfigObj(testconfig.Webhook().Deploy(false))
			VerifyMemberOperatorConfig(t, hostAwait, memberAwait2, wait.UntilMemberConfigMatches(member2ExpectedConfig.Spec))
		})
		t.Run("verify updated toolchainconfig is synced - go to unready", func(t *testing.T) {
			// set the che required flag to true to force an error on the memberstatus (che is not installed in e2e test environments)
			memberConfigurationWithCheRequired := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(), testconfig.Che().Required(true))
			hostAwait.UpdateToolchainConfig(testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

			err := memberAwait.WaitForMemberStatus(
				wait.UntilMemberStatusHasConditions(ToolchainStatusComponentsNotReady("[routes]")))
			require.NoError(t, err, "failed while waiting for MemberStatus to contain error due to che being required")

			_, err = hostAwait.WaitForToolchainStatus(
				wait.UntilToolchainStatusHasConditions(ToolchainStatusComponentsNotReady("[members]"), ToolchainStatusUnreadyNotificationNotCreated()))
			require.NoError(t, err, "failed while waiting for ToolchainStatus to contain error due to che being required")

			t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
				// change che required flag back to true to resolve the error on the memberstatus
				memberConfigurationWithCheRequired = testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(), testconfig.Che().Required(false))
				hostAwait.UpdateToolchainConfig(testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

				VerifyMemberStatus(t, memberAwait, consoleURL)
				VerifyToolchainStatus(t, hostAwait, memberAwait)
			})
		})
	})

	memberAwait.WaitForUsersPodsWebhook()
	memberAwait.WaitForAutoscalingBufferApp()

	originalToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
		ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
	originalMursPerDomainCount := originalToolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
	t.Logf("the original MasterUserRecord count: %v", originalMursPerDomainCount)

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, awaitilities, awaitilities.Member1(), 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup, _ := NewSignupRequest(t, awaitilities).
		Username(johnsmithName).
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	extrajohnName := "extrajohn"
	johnExtraSignup, _ := NewSignupRequest(t, awaitilities).
		Username(extrajohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	targetedJohnName := "targetedjohn"
	targetedJohnSignup, _ := NewSignupRequest(t, awaitilities).
		Username(targetedJohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait2).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	originalSubJohnName := "originalsubjohn"
	originalSubJohnClaim := "originalsub:john"
	originalSubJohnSignup, _ := NewSignupRequest(t, awaitilities).
		Username(originalSubJohnName).
		OriginalSub(originalSubJohnClaim).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	// Confirm the originalSub property has been set during signup
	require.Equal(t, originalSubJohnClaim, originalSubJohnSignup.Spec.OriginalSub)

	VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, targetedJohnSignup, "base")
	VerifyResourcesProvisionedForSignup(t, awaitilities, originalSubJohnSignup, "base")

	johnsmithMur, err := hostAwait.GetMasterUserRecord(johnsmithName)
	require.NoError(t, err)

	targetedJohnMur, err := hostAwait.GetMasterUserRecord(targetedJohnName)
	require.NoError(t, err)

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
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")
		})

		t.Run("delete identity and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(johnSignup.Name)}, identity)
			require.NoError(t, err)

			// when
			err = memberAwait.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")
		})

		t.Run("delete user mapping and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			user.Identities = []string{}
			err = memberAwait.Client.Update(context.TODO(), user)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")
		})

		t.Run("delete identity mapping and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(johnSignup.Name)}, identity)
			require.NoError(t, err)
			identity.User = corev1.ObjectReference{Name: "", UID: ""}

			// when
			err = memberAwait.Client.Update(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 2)
			templateRefs := tiers.GetTemplateRefs(hostAwait, "base")
			for _, ref := range templateRefs.Namespaces {
				ns, err := memberAwait.WaitForNamespace(johnSignup.Spec.Username, ref, "base", wait.UntilNamespaceIsActive())
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
				_, err := memberAwait.WaitForNamespace(johnSignup.Spec.Username, ref, "base", wait.UntilNamespaceIsActive())
				require.NoError(t, err)
			}
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")
		})

		t.Run("delete useraccount and expect recreation", func(t *testing.T) {
			// given
			ua, err := memberAwait.WaitForUserAccount(johnSignup.Status.CompliantUsername)
			require.NoError(t, err)

			// when deleting the user account
			deletePolicy := metav1.DeletePropagationForeground
			deleteOpts := &client.DeleteOptions{
				PropagationPolicy: &deletePolicy,
			}
			err = memberAwait.Client.Delete(context.TODO(), ua, deleteOpts)
			require.NoError(t, err)
			_, err = memberAwait.WaitForUserAccount(ua.Name, wait.UntilUserAccountIsBeingDeleted())
			require.NoError(t, err)

			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, johnSignup, "base")
		})
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		VerifyMultipleSignups(t, awaitilities, signups)

		// check if the MUR and UA counts match
		currentToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
			ToolchainStatusReadyAndUnreadyNotificationNotCreated()...), wait.UntilHasMurCount("external", originalMursPerDomainCount["external"]+9))
		require.NoError(t, err)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 8)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, targetedJohnMur.Spec.UserAccounts[0].TargetCluster, 1)
	})

	t.Run("verify userAccount is not deleted if namespace is not deleted", func(t *testing.T) {
		// given
		laraSignUp, _ := NewSignupRequest(t, awaitilities).
			Username("laracroft").
			Email("laracroft@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(t, "laracroft", laraSignUp.Status.CompliantUsername)

		laraUserName := "laracroft"
		userNamespace := "laracroft-dev"
		cmName := "test-useraccount-delete-1"

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
		err = memberAwait.Client.Create(context.TODO(), cm)
		require.NoError(t, err)
		cm, err := memberAwait.WaitForConfigMap(userNamespace, cmName)
		require.NoError(t, err)
		require.NotEmpty(t, cm)

		deletePolicy := metav1.DeletePropagationForeground
		deleteOpts := &client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		// when deleting the userSignup
		err = hostAwait.Client.Delete(context.TODO(), laraSignUp, deleteOpts)
		require.NoError(t, err)

		// then nothing should be deleted yet (because of the CM with its own finalizer)
		nsTmplSet, err := memberAwait.WaitForNSTmplSet(laraUserName, wait.UntilNSTemplateSetIsBeingDeleted(), wait.UntilNSTemplateSetHasConditions(TerminatingNSTemplateSet()))
		require.NoError(t, err)
		require.NotEmpty(t, nsTmplSet)

		// Check that namespace is not deleted and is in Terminating state after 10sec
		_, err = memberAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForNamespaceInTerminating(userNamespace)
		require.NoError(t, err)

		nsTmplSet, err = memberAwait.WaitForNSTmplSet(laraUserName, wait.UntilNSTemplateSetIsBeingDeleted(), wait.UntilNSTemplateSetHasConditions(TerminatingNSTemplateSet()))
		require.NoError(t, err)
		require.NotEmpty(t, nsTmplSet)

		userAcc, err := memberAwait.WaitForUserAccount(laraUserName, wait.UntilUserAccountIsBeingDeleted(), wait.UntilUserAccountContainsCondition(TerminatingUserAccount()))
		require.NoError(t, err)
		require.NotEmpty(t, userAcc)

		mur, err := hostAwait.WaitForMasterUserRecord(laraUserName, wait.UntilMasterUserRecordIsBeingDeleted(), wait.UntilMasterUserRecordHasCondition(UnableToDeleteUserAccount()))
		require.NoError(t, err)
		require.NotEmpty(t, mur)

		userSignup, err := hostAwait.WaitForUserSignup(laraSignUp.Name, wait.UntilUserSignupIsBeingDeleted())
		require.NoError(t, err)
		require.NotEmpty(t, userSignup)

		t.Run("remove finalizer", func(t *testing.T) {
			// when removing the finalizer from the CM
			_, err = memberAwait.UpdateConfigMap(cm.Namespace, cmName, func(cm *corev1.ConfigMap) {
				cm.Finalizers = nil
			})
			require.NoError(t, err)

			// then check all are deleted
			err = memberAwait.WaitUntilNamespaceDeleted(laraUserName, "dev")
			assert.NoError(t, err, "laracroft-dev namespace is not deleted")

			err = memberAwait.WaitUntilNSTemplateSetDeleted(laraUserName)
			assert.NoError(t, err, "NSTemplateSet is not deleted")

			err = memberAwait.WaitUntilUserAccountDeleted(laraUserName)
			require.NoError(t, err)

			err = hostAwait.WaitUntilMasterUserRecordDeleted(laraUserName)
			require.NoError(t, err)

			err = hostAwait.WaitUntilUserSignupDeleted(laraSignUp.Name)
			require.NoError(t, err)
		})

	})

	t.Run("Try to delete namespaced scoped resources of users, and expect recreation", func(t *testing.T) {
		userSignup, _ := NewSignupRequest(t, awaitilities).
			Username("wonderwoman").
			Email("wonderwoman@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()
		devNs := corev1.Namespace{}
		err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "wonderwoman-dev"}, &devNs)
		require.NoError(t, err)

		stageNs := corev1.Namespace{}
		err = memberAwait.Client.Get(context.TODO(), types.NamespacedName{Name: "wonderwoman-stage"}, &stageNs)
		require.NoError(t, err)

		t.Run("role accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, devNs, "rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "base")
		})

		t.Run("role accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {
			DeletedRoleAndAwaitRecreation(t, memberAwait, stageNs, "rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "base")
		})

		t.Run("rolebinding accidentally deleted by user in dev namespace is recreated", func(t *testing.T) {

			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, devNs, "user-rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "base")
		})

		t.Run("rolebinding accidentally deleted by user in stage namespace is recreated", func(t *testing.T) {

			DeleteRoleBindingAndAwaitRecreation(t, memberAwait, stageNs, "user-rbac-edit")
			// then the user account should be recreated
			VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup, "base")
		})
	})

	t.Run("delete usersignup and expect all resources to be deleted", func(t *testing.T) {
		// given
		johnSignup, err := hostAwait.WaitForUserSignup(johnSignup.Name)
		require.NoError(t, err)

		// when
		err = hostAwait.Client.Delete(context.TODO(), johnSignup)

		// then
		require.NoError(t, err)
		t.Logf("usersignup '%s' deleted (resource name='%s')", johnsmithName, johnSignup.Name)

		err = hostAwait.WaitUntilMasterUserRecordDeleted(johnsmithName)
		assert.NoError(t, err, "MasterUserRecord is not deleted")

		err = memberAwait.WaitUntilUserAccountDeleted(johnsmithName)
		assert.NoError(t, err, "UserAccount is not deleted")

		err = memberAwait.WaitUntilUserDeleted(johnsmithName)
		assert.NoError(t, err, "User is not deleted")

		err = memberAwait.WaitUntilIdentityDeleted(johnsmithName)
		assert.NoError(t, err, "Identity is not deleted")

		err = memberAwait.WaitUntilNSTemplateSetDeleted(johnsmithName)
		assert.NoError(t, err, "NSTemplateSet id not deleted")

		err = memberAwait.WaitUntilClusterResourceQuotasDeleted(johnsmithName)
		assert.NoError(t, err, "ClusterResourceQuotas were not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "dev")
		assert.NoError(t, err, "johnsmith-dev namespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "stage")
		assert.NoError(t, err, "johnsmith-stage namespace is not deleted")

		// also, verify that other user's resource are left intact
		VerifyResourcesProvisionedForSignup(t, awaitilities, johnExtraSignup, "base")

		// check if the MUR and UA counts match
		currentToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
			ToolchainStatusReadyAndUnreadyNotificationNotCreated()...), wait.UntilHasMurCount("external", originalMursPerDomainCount["external"]+8))
		require.NoError(t, err)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 8)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, targetedJohnMur.Spec.UserAccounts[0].TargetCluster, 1)
	})

	t.Run("deactivate UserSignup and ensure that all user and identity resources are deleted", func(t *testing.T) {

		// First confirm that there are actually multiple identities for the UserSignup in question, using the
		// owner label to locate them

		// First, find the MUR
		mur, err := hostAwait.WaitForMasterUserRecord(originalSubJohnSignup.Status.CompliantUsername)
		require.NoError(t, err)

		memberAwait := GetMurTargetMember(t, awaitilities, mur)

		// Then locate the UserAccount
		userAccount, err := memberAwait.WaitForUserAccount(mur.Name)
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
		userSignup, err := hostAwait.UpdateUserSignup(originalSubJohnSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})
		require.NoError(t, err)

		// Wait until the UserSignup is deactivated
		_, err = hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), ManuallyDeactivated())...))
		require.NoError(t, err)

		// Ensure the first identity is deleted
		err = memberAwait.WaitUntilIdentityDeleted(identityList.Items[0].Name)
		require.NoError(t, err)

		// Ensure the second identity is deleted
		err = memberAwait.WaitUntilIdentityDeleted(identityList.Items[1].Name)
		require.NoError(t, err)

		// Ensure the user is deleted
		err = memberAwait.WaitUntilUserDeleted(userList.Items[0].Name)
		require.NoError(t, err)
	})

}

func listByOwnerLabel(owner string) client.ListOption {
	labels := map[string]string{toolchainv1alpha1.OwnerLabelKey: owner}
	return client.MatchingLabels(labels)
}
