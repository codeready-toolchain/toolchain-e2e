package e2e

import (
	"context"
	"fmt"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	userv1 "github.com/openshift/api/user/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
	"time"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	hostAwait, memberAwait, member2Await := WaitForDeployments(t)
	memberConfiguration1 := testconfig.NewMemberOperatorConfig(testconfig.MemberStatus().RefreshPeriod("5s"))
	memberConfiguration2 := testconfig.NewMemberOperatorConfig(testconfig.MemberStatus().RefreshPeriod("10s"))
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled(), testconfig.Members().Default(memberConfiguration1.Spec), testconfig.Members().SpecificPerMemberCluster(member2Await.ClusterName, memberConfiguration2.Spec))

	t.Run("verify MemberOperatorConfigs synced from ToolchainConfig to member clusters", func(t *testing.T) {
		t.Run("verify ToolchainConfig has synced status", func(t *testing.T) {
			VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasSyncedStatus(ToolchainConfigSyncComplete()))
		})
		t.Run("verify MemberOperatorConfig was synced to both member clusters", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, memberAwait, wait.UntilMemberConfigMatches(memberConfiguration1))
		})
		t.Run("verify MemberOperatorConfig was synced to member2", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, member2Await, wait.UntilMemberConfigMatches(memberConfiguration2))
		})
		t.Run("verify updated toolchainconfig is synced", func(t *testing.T) {
			// when switch member1 and member2 configs
			hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled(), testconfig.Members().Default(memberConfiguration2.Spec), testconfig.Members().SpecificPerMemberCluster(member2Await.ClusterName, memberConfiguration1.Spec))

			// then
			VerifyMemberOperatorConfig(t, memberAwait, wait.UntilMemberConfigMatches(memberConfiguration2))
			VerifyMemberOperatorConfig(t, member2Await, wait.UntilMemberConfigMatches(memberConfiguration1))
		})
	})

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

	// host metrics should be available at this point
	t.Run("verify metrics servers", func(t *testing.T) {

		t.Run("verify host metrics server", func(t *testing.T) {
			VerifyHostMetricsService(t, hostAwait)
		})

		t.Run("verify member metrics server", func(t *testing.T) {
			VerifyMemberMetricsService(t, memberAwait)
		})
	})

	memberAwait.WaitForUsersPodsWebhook()
	memberAwait.WaitForAutoscalingBufferApp()

	originalToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
		ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
	originalMursPerDomainCount := originalToolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
	t.Logf("the original MasterUserRecord count: %v", originalMursPerDomainCount)

	// Get metrics assertion helper for testing metrics before creating any users
	metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, hostAwait, memberAwait, 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup := CreateAndApproveSignup(t, hostAwait, johnsmithName, memberAwait.ClusterName)
	extrajohnName := "extrajohn"
	johnExtraSignup := CreateAndApproveSignup(t, hostAwait, extrajohnName, memberAwait.ClusterName)
	targetedJohnName := "targetedjohn"
	targetedJohnSignup := CreateAndApproveSignup(t, hostAwait, targetedJohnName, member2Await.ClusterName)

	VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
	VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
	VerifyResourcesProvisionedForSignup(t, hostAwait, targetedJohnSignup, "base", member2Await)

	johnsmithMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(johnsmithName))
	require.NoError(t, err)

	targetedJohnMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(targetedJohnName))
	require.NoError(t, err)

	t.Run("verify metrics are correct at the beginning", func(t *testing.T) {
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 8)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 8)
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 8, "domain", "external")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 7, "cluster_name", memberAwait.ClusterName)  // 7 users on member1
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", member2Await.ClusterName) // 1 user on member2
	})

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
			// when deleting the user account
			err := memberAwait.DeleteUserAccount(johnSignup.Status.CompliantUsername)

			// then the user account should be recreated
			require.NoError(t, err)
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

	t.Run("provision new user for manual testing", func(t *testing.T) {
		fmt.Printf("-------------------------------------------------------------")
		kanikaSignUp := CreateAndApproveSignup(t, hostAwait, "kanikarana", memberAwait.ClusterName)
		VerifyResourcesProvisionedForSignup(t, hostAwait, kanikaSignUp, "base", memberAwait)
		fmt.Printf("\n" + kanikaSignUp.Namespace)
		kanikaMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName("kanikarana"))
		require.NoError(t, err)
		fmt.Printf(string(kanikaMur.UID))
		fmt.Printf("\n")
		fmt.Printf(kanikaMur.Namespace)
		time.Sleep(time.Minute * 5)
		fmt.Printf("-------------------------------------------------------------")
	})

	t.Run("verify userAccount is not deleted if namespace is not deleted", func(t *testing.T) {
		laraUserName := "laracroft"
		userNamespace := "laracroft-dev"
		podName := "test-useraccount-delete-1"
		laraSignUp := CreateAndApproveSignup(t, hostAwait, laraUserName, memberAwait.ClusterName)
		VerifyResourcesProvisionedForSignup(t, hostAwait, laraSignUp, "base", memberAwait)

		// when
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
					"kubernetes",
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
		fmt.Printf("\n Pod created >>>>>>>>>>>>>>>>>>>>>>>>>>>>>")

		// confirm pod created
		pod := &corev1.Pod{}
		if err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: userNamespace, Name: podName}, pod); err != nil {
			fmt.Errorf("\n could not find the created pod")
		}
		require.NoError(t, err)
		type testDeleteOption struct{}

		deletePolicy := metav1.DeletePropagationForeground
		deleteOpts := &client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		// now delete userSignup but nothing should be deleted yet
		err = hostAwait.Client.Delete(context.TODO(), laraSignUp, deleteOpts)
		require.NoError(t, err)

		//laraSignUp, err = hostAwait.UpdateUserSignupSpec(laraSignUp.Name, func(us *toolchainv1alpha1.UserSignup) {
		//	states.SetDeactivated(us, true)
		//})
		//require.NoError(t, err)
		//fmt.Printf(string(laraSignUp.Spec.States[0]))

		fmt.Printf(">>>>>>>>>> was delete triggered")
		err = memberAwait.WaitUntilNamespaceDeleted(laraUserName, "dev")
		assert.Error(t, err, "laracroft-dev namespace got deleted")

		err = memberAwait.WaitUntilNSTemplateSetDeleted(laraUserName)
		assert.Error(t, err, "NSTemplateSet got deleted")

		err = memberAwait.WaitUntilUserAccountDeleted(laraUserName)
		assert.Error(t, err, "userAccount got deleted")

		err = hostAwait.WaitUntilMasterUserRecordDeleted(laraUserName)
		require.Error(t, err)

		err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: laraSignUp.Namespace, Name: laraSignUp.Name}, laraSignUp)
		require.NoError(t, err)

		// now remove finalizer from pod and check all are deleted
		if err := memberAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: userNamespace, Name: podName}, pod); err != nil {
			fmt.Errorf("\n could not find the pod")
		}
		require.Equal(t, pod.Finalizers[0], "kubernetes")
		pod.Finalizers = nil
		err = memberAwait.Client.Update(context.TODO(), pod)
		require.NoError(t, err)
		require.Equal(t, len(pod.Finalizers), 0)

		err = memberAwait.WaitUntilNamespaceDeleted(laraUserName, "dev")
		assert.NoError(t, err, "laracroft-dev namespace is not deleted")

		err = memberAwait.WaitUntilNSTemplateSetDeleted(laraUserName)
		assert.NoError(t, err, "NSTemplateSet is not deleted")

		err = memberAwait.WaitUntilUserAccountDeleted(laraUserName)
		assert.NoError(t, err, "userAccount is not deleted")

		err = hostAwait.WaitUntilMasterUserRecordDeleted(laraUserName)
		assert.NoError(t, err, "MUR is not deleted")

		err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: laraSignUp.Namespace, Name: laraSignUp.Name}, laraSignUp)
		require.Error(t, err)
		require.True(t, errors.IsNotFound(err))

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
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 6)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, targetedJohnMur.Spec.UserAccounts[0].TargetCluster, 1)

		t.Run("verify metrics are correct at the end", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 8)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 8)
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 8, "activations", "1", "domain", "external") // 'johnsignup' was deleted but we keep track of his activation
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 6, "cluster_name", memberAwait.ClusterName)                  // 6 users left on member1 ('johnsignup' was deleted)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", member2Await.ClusterName)                 // 1 user on member2
		})
	})

}
