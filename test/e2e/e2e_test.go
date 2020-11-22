package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	ctx, hostAwait, memberAwait := WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	defer ctx.Cleanup()
	hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())
	consoleURL := memberAwait.GetConsoleURL()
	// host and member cluster statuses should be available at this point
	t.Run("verify cluster statuses are valid", func(t *testing.T) {
		t.Run("verify member cluster status", func(t *testing.T) {
			VerifyMemberStatus(t, memberAwait, consoleURL)
		})

		t.Run("verify overall toolchain status", func(t *testing.T) {
			VerifyToolchainStatus(t, hostAwait)
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

	originalToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReady()))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
	originalMurCount := originalToolchainStatus.Status.HostOperator.MasterUserRecordCount
	t.Logf("the original MasterUserRecord count: %d", originalMurCount)

	// Get metrics assertion helper for testing metrics before creating any users
	metricsAssertion := InitMetricsAssertion(hostAwait)

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, ctx, hostAwait, memberAwait, 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup := CreateAndApproveSignup(t, hostAwait, johnsmithName)
	extrajohnName := "extrajohn"
	johnExtraSignup := CreateAndApproveSignup(t, hostAwait, extrajohnName)

	VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
	VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")

	johnsmithMur, err := hostAwait.GetMasterUserRecord(wait.WithMurName(johnsmithName))
	require.NoError(t, err)

	t.Run("verify metrics are correct at the beginning", func(t *testing.T) {
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 7)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 7)
		metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 7)
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 3)
			templateRefs := tiers.GetTemplateRefs(hostAwait, "basic")
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
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")
		})

		t.Run("delete useraccount and expect recreation", func(t *testing.T) {
			// when deleting the user account
			err := memberAwait.DeleteUserAccount(johnSignup.Status.CompliantUsername)

			// then the user account should be recreated
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnSignup, "basic")
		})
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		VerifyMultipleSignups(t, hostAwait, memberAwait, signups)

		// check if the MUR and UA counts match
		currentToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReady()), wait.UntilHasMurCount(originalMurCount+7))
		require.NoError(t, err)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 7)
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

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "code")
		assert.NoError(t, err, "johnsmith-code namespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "dev")
		assert.NoError(t, err, "johnsmith-dev namespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "stage")
		assert.NoError(t, err, "johnsmith-stage namespace is not deleted")

		// also, verify that other user's resource are left intact
		VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, johnExtraSignup, "basic")

		// check if the MUR and UA counts match
		currentToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReady()), wait.UntilHasMurCount(originalMurCount+6))
		require.NoError(t, err)
		VerifyIncreaseOfUserAccountCount(t, originalToolchainStatus, currentToolchainStatus, johnsmithMur.Spec.UserAccounts[0].TargetCluster, 6)

	})

	t.Run("verify metrics are correct at the end", func(t *testing.T) {
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 7)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 7)
		metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 6)
	})
}
