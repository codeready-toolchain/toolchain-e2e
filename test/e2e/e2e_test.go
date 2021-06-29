package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	hostAwait, memberAwait, memberAwait2 := WaitForDeployments(t)

	consoleURL := memberAwait.GetConsoleURL()
	// host and member cluster statuses should be available at this point
	t.Run("verify cluster statuses", func(t *testing.T) {
		t.Run("verify member cluster status is ready", func(t *testing.T) {
			VerifyMemberStatusReady(t, memberAwait, consoleURL)
		})

		t.Run("verify overall toolchain status is ready", func(t *testing.T) {
			VerifyToolchainStatusReady(t, hostAwait, memberAwait)
		})
	})

	t.Run("verify MemberOperatorConfigs synced from ToolchainConfig to member clusters", func(t *testing.T) {
		currentConfig := hostAwait.GetToolchainConfig()
		expectedMemberConfiguration := currentConfig.Spec.Members.Default

		t.Run("verify ToolchainConfig has synced status", func(t *testing.T) {
			VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasSyncedStatus(ToolchainConfigSyncComplete()))
		})
		t.Run("verify MemberOperatorConfig was synced to both member clusters", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, memberAwait, wait.UntilMemberConfigMatches(expectedMemberConfiguration))
		})
		t.Run("verify MemberOperatorConfig was synced to member2", func(t *testing.T) {
			VerifyMemberOperatorConfig(t, memberAwait2, wait.UntilMemberConfigMatches(expectedMemberConfiguration))
		})
		t.Run("verify updated toolchainconfig is synced - go to unready", func(t *testing.T) {
			// set the che required flag to true to force an error on the memberstatus (che is not installed in e2e test environments)
			memberConfigurationWithCheRequired := testconfig.ModifyMemberOperatorConfig(memberAwait.GetMemberOperatorConfig(), testconfig.Che().Required(true))
			hostAwait.UpdateToolchainConfig(testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

			err := memberAwait.WaitForMemberStatus(
				wait.UntilMemberStatusHasConditions(ToolchainStatusComponentsNotReady("[routes]")))
			require.NoError(t, err, "failed while waiting for MemberStatus to contain error due to che being required")

			_, err = hostAwait.WaitForToolchainStatus(
				wait.UntilToolchainStatusHasConditions(ToolchainStatusComponentsNotReady("[members]"), ToolchainStatusUnreadyNotificationNotCreated()))
			require.NoError(t, err, "failed while waiting for ToolchainStatus to contain error due to che being required")

			t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
				// change che required flag back to true to resolve the error on the memberstatus
				memberConfigurationWithCheRequired = testconfig.ModifyMemberOperatorConfig(memberAwait.GetMemberOperatorConfig(), testconfig.Che().Required(false))
				hostAwait.UpdateToolchainConfig(testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

				VerifyMemberStatusReady(t, memberAwait, consoleURL)
				VerifyToolchainStatusReady(t, hostAwait, memberAwait)
			})
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

	t.Run("verify users pods webhook", func(t *testing.T) {
		memberAwait.WaitForUsersPodsWebhook()
	})
	t.Run("verify autoscaling buffer", func(t *testing.T) {
		memberAwait.WaitForAutoscalingBufferApp()
	})

	originalToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
		ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
	originalMursPerDomainCount := originalToolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
	t.Logf("the original MasterUserRecord count: %v", originalMursPerDomainCount)

	// Get metrics assertion helper for testing metrics before creating any users
	metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, memberAwait2.ClusterName})

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, hostAwait, memberAwait, 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup := CreateAndApproveSignup(t, hostAwait, johnsmithName, memberAwait.ClusterName)
	extrajohnName := "extrajohn"
	johnExtraSignup := CreateAndApproveSignup(t, hostAwait, extrajohnName, memberAwait.ClusterName)
	targetedJohnName := "targetedjohn"
	targetedJohnSignup := CreateAndApproveSignup(t, hostAwait, targetedJohnName, memberAwait2.ClusterName)

	VerifyResourcesProvisionedForSignup(t, hostAwait, johnSignup, "base", memberAwait)
	VerifyResourcesProvisionedForSignup(t, hostAwait, johnExtraSignup, "base", memberAwait)
	VerifyResourcesProvisionedForSignup(t, hostAwait, targetedJohnSignup, "base", memberAwait2)

	johnsmithMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(johnsmithName))
	require.NoError(t, err)

	targetedJohnMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(targetedJohnName))
	require.NoError(t, err)

	t.Run("verify metrics are correct at the beginning", func(t *testing.T) {
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 8)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 8)
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 8, "domain", "external")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 7, "cluster_name", memberAwait.ClusterName)  // 7 users on member1
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait2.ClusterName) // 1 user on member2
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
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait2.ClusterName)                 // 1 user on member2
		})
	})

}
