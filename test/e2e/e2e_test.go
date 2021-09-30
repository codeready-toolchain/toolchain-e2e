package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

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
	hostAwait, memberAwait, memberAwait2 := WaitForDeployments(t)

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
	signups := CreateMultipleSignups(t, hostAwait, memberAwait, 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, memberAwait2).
		Username(johnsmithName).
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	extrajohnName := "extrajohn"
	johnExtraSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, memberAwait2).
		Username(extrajohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	targetedJohnName := "targetedjohn"
	targetedJohnSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, memberAwait2).
		Username(targetedJohnName).
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait2).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
	VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
	VerifyResourcesProvisionedForSignup(t, hostAwait, targetedJohnSignup, "base", memberAwait2)

	johnsmithMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(johnsmithName))
	require.NoError(t, err)

	targetedJohnMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(targetedJohnName))
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 2)
			templateRefs := tiers.GetTemplateRefs(hostAwait, "base")
			for _, ref := range templateRefs.Namespaces {
				ns, err := memberAwait.WaitForNamespace(johnSignup.Spec.Username, ref)
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
				_, err := memberAwait.WaitForNamespace(johnSignup.Spec.Username, ref)
				require.NoError(t, err)
			}
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
		})
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		VerifyMultipleSignups(t, hostAwait, signups, memberAwait)

		// check if the MUR and UA counts match
		currentToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
			ToolchainStatusReadyAndUnreadyNotificationNotCreated()...), wait.UntilHasMurCount("external", originalMursPerDomainCount["external"]+8))
		require.NoError(t, err)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 7)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, targetedJohnMur.Spec.UserAccounts[0].TargetCluster, 1)
	})

	t.Run("verify userAccount is not deleted if namespace is not deleted", func(t *testing.T) {
		laraSignUp, _ := NewSignupRequest(t, hostAwait, memberAwait, memberAwait2).
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
		podName := "test-useraccount-delete-1"

		//Create a pod with finalizer in user's namespace, which will block the deletion of namespace.
		memberPod := corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: userNamespace,
				Finalizers: []string{
					"test/finalizer.toolchain.e2e.tests",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "test",
					Image: "busybox",
				}},
			},
		}
		err = memberAwait.Client.Create(context.TODO(), &memberPod)
		require.NoError(t, err)
		pod, err := memberAwait.WaitForPod(userNamespace, podName)
		require.NoError(t, err)
		require.NotEmpty(t, pod)

		deletePolicy := metav1.DeletePropagationForeground
		deleteOpts := &client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		// now delete userSignup but nothing should be deleted yet
		err = hostAwait.Client.Delete(context.TODO(), laraSignUp, deleteOpts)
		require.NoError(t, err)

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

		// now remove finalizer from pod and check all are deleted
		_, err = memberAwait.UpdatePod(pod.Namespace, podName, func(pod *corev1.Pod) {
			pod.Finalizers = nil
		})
		require.NoError(t, err)

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

	t.Run("role accidentally deleted by user is recreated", func(t *testing.T) {

		userSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, memberAwait2).
			Username("wonderwoman").
			Email("wonderwoman@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()
		devNs := corev1.Namespace{}
		err := memberAwait.Client.Get(context.TODO(),types.NamespacedName{Name: "wonderwoman-dev"}, &devNs)
		require.NoError(t, err)

		stageNs := corev1.Namespace{}
		err = memberAwait.Client.Get(context.TODO(),types.NamespacedName{Name: "wonderwoman-stage"}, &stageNs)
		require.NoError(t, err)

		userRole, err := memberAwait.WaitForRole(&devNs, "rbac-edit")
		require.NoError(t, err)
		require.NotEmpty(t, userRole)
		fmt.Println(userRole.Labels)
		require.Contains(t, userRole.Labels, "toolchain.dev.openshift.com/owner")

		//when role deleted
		err = memberAwait.Client.Delete(context.TODO(),userRole)
		require.NoError(t, err)

		// then verify role is recreated
		userRole, err = memberAwait.WaitForRole(&devNs, "rbac-edit")
		require.NoError(t, err)
		require.NotEmpty(t, userRole)

		// then the user account should be recreated
		VerifyResourcesProvisionedForSignup(t, hostAwait, userSignup, "base", memberAwait)
	})

	t.Run("rolebinding accidentally deleted by user is recreated", func(t *testing.T) {

		userSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, memberAwait2).
			Username("wonderwoman2").
			Email("wonderwoman2@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()
		devNs := corev1.Namespace{}
		err := memberAwait.Client.Get(context.TODO(),types.NamespacedName{Name: "wonderwoman2-dev"}, &devNs)
		require.NoError(t, err)

		stageNs := corev1.Namespace{}
		err = memberAwait.Client.Get(context.TODO(),types.NamespacedName{Name: "wonderwoman2-stage"}, &stageNs)
		require.NoError(t, err)

		userRoleBinding, err := memberAwait.WaitForRoleBinding(&devNs, "user-rbac-edit")
		require.NoError(t, err)
		require.NotEmpty(t, userRoleBinding)
		fmt.Println(userRoleBinding.Labels)
		require.Contains(t, userRoleBinding.Labels, "toolchain.dev.openshift.com/owner")

		//when rolebinding deleted
		err = memberAwait.Client.Delete(context.TODO(),userRoleBinding)
		require.NoError(t, err)

		// then verify role is recreated
		userRoleBinding, err = memberAwait.WaitForRoleBinding(&devNs, "user-rbac-edit")
		require.NoError(t, err)
		require.NotEmpty(t, userRoleBinding)

		// then the user account should be recreated
		VerifyResourcesProvisionedForSignup(t, hostAwait, userSignup, "base", memberAwait)

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
		VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)

		// check if the MUR and UA counts match
		currentToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
			ToolchainStatusReadyAndUnreadyNotificationNotCreated()...), wait.UntilHasMurCount("external", originalMursPerDomainCount["external"]+7))
		require.NoError(t, err)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 7)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, targetedJohnMur.Spec.UserAccounts[0].TargetCluster, 1)
	})

}
