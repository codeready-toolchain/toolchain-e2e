package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
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
	ctx, awaitility := WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	defer ctx.Cleanup()

	// host and member cluster statuses should be available at this point
	t.Run("verify cluster statuses are valid", func(t *testing.T) {
		t.Run("verify member cluster status", func(t *testing.T) {
			VerifyMemberStatus(t, awaitility.Member())
		})

		t.Run("verify overall toolchain status", func(t *testing.T) {
			VerifyToolchainStatus(t, awaitility.Host())
		})
	})

	// host metrics should be available at this point
	t.Run("verify metrics servers", func(t *testing.T) {

		t.Run("verify host metrics server", func(t *testing.T) {
			VerifyHostMetricsService(t, awaitility.Host())
		})

		t.Run("verify member metrics server", func(t *testing.T) {
			VerifyMemberMetricsService(t, awaitility.Member())
		})
	})

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := CreateMultipleSignups(t, ctx, awaitility, 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup := CreateAndApproveSignup(t, awaitility, johnsmithName)
	extrajohnName := "extrajohn"
	johnExtraSignup := CreateAndApproveSignup(t, awaitility, extrajohnName)

	VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
	VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")

	t.Run("try to break UserAccount", func(t *testing.T) {

		t.Run("delete user and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), user)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete identity and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(johnSignup.Name)}, identity)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete user mapping and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			user.Identities = []string{}
			err = awaitility.Client.Update(context.TODO(), user)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete identity mapping and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(johnSignup.Name)}, identity)
			require.NoError(t, err)
			identity.User = corev1.ObjectReference{Name: "", UID: ""}

			// when
			err = awaitility.Client.Update(context.TODO(), identity)

			// then
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 3)
			templateRefs := tiers.GetTemplateRefs(awaitility.Host(), "basic")
			for _, ref := range templateRefs.Namespaces {
				ns, err := awaitility.Member().WaitForNamespace(johnSignup.Spec.Username, ref)
				require.NoError(t, err)
				namespaces = append(namespaces, ns)
			}
			// when
			for _, ns := range namespaces {
				err := awaitility.Client.Delete(context.TODO(), ns)
				require.NoError(t, err)
			}

			// then
			// wait for the namespaces to be re-created before validating all other resources to avoid race condition
			for _, ref := range templateRefs.Namespaces {
				_, err := awaitility.Member().WaitForNamespace(johnSignup.Spec.Username, ref)
				require.NoError(t, err)
			}
			VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete useraccount and expect recreation", func(t *testing.T) {
			// given
			memberAwait := wait.NewMemberAwaitility(awaitility)

			// when deleting the user account
			err := memberAwait.DeleteUserAccount(johnSignup.Status.CompliantUsername)

			// then the user account should be recreated
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
		})
	})

	t.Run("delete usersignup and expect all resources to be deleted", func(t *testing.T) {
		// given
		hostAwait := wait.NewHostAwaitility(awaitility)
		memberAwait := wait.NewMemberAwaitility(awaitility)
		johnSignup, err := hostAwait.WaitForUserSignup(johnSignup.Name)
		require.NoError(t, err)

		// when
		err = awaitility.Client.Delete(context.TODO(), johnSignup)

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
		VerifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		VerifyMultipleSignups(t, awaitility, signups)
	})

}
