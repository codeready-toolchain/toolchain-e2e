package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type appstudioUsers struct {
	expectedMemberCluster *wait.MemberAwaitility
	username              string
	token                 string
	signup                *toolchainv1alpha1.UserSignup
}

func TestAppStudioFlow(t *testing.T) {
	// full flow from usersignup with approval down to namespaces creation and cleanup
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	// check that the tier exists, and all its namespace other cluster-scoped resource revisions
	// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

	originalToolchainStatus, err := hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(
		ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
	require.NoError(t, err, "failed while waiting for ToolchainStatus")
	originalMursPerDomainCount := originalToolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
	t.Logf("the original MasterUserRecord count: %v", originalMursPerDomainCount)

	users := []appstudioUsers{
		{
			expectedMemberCluster: memberAwait,
			username:              "asm1",
		},
		{
			expectedMemberCluster: memberAwait2,
			username:              "asm2",
		},
	}

	for _, user := range users {
		// Create and approve signup
		req := NewSignupRequest(t, awaitilities).
			Username(user.username).
			ManuallyApprove().
			TargetCluster(user.expectedMemberCluster).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute()

		user.signup, _ = req.Resources()
		user.token = req.GetToken()

		VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, "base")
		_, err = hostAwait.GetMasterUserRecord(wait.WithMurName(user.username))
		require.NoError(t, err)

		// since the registration service always provisions users to the default tier users need to be
		// promoted to the appstudio tier in order to test appstudio scenarios
		t.Run("promote to appstudio tier", func(t *testing.T) {
			// given
			promotionTier := "appstudio"

			changeTierRequest := NewChangeTierRequest(hostAwait.Namespace, user.signup.Status.CompliantUsername, promotionTier)

			// when
			err = hostAwait.CreateWithCleanup(context.TODO(), changeTierRequest)

			// then
			require.NoError(t, err)
			_, err := hostAwait.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, promotionTier)

			// then - wait until ChangeTierRequest is deleted by our automatic GC
			err = hostAwait.WaitUntilChangeTierRequestDeleted(changeTierRequest.Name)
			assert.NoError(t, err)
		})

		t.Run("use proxy to create a configmap in each user appstudio namespace via proxy API", func(t *testing.T) {
			cmName := "appstudio-test-cm"
			expectedCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: user.username,
				},
				Data: map[string]string{
					"planet": "venus",
				},
			}

			proxyCl := hostAwait.CreateAPIProxyClient(user.token)
			err := proxyCl.Create(context.TODO(), expectedCM)
			require.NoError(t, err)

			createdCM := &corev1.ConfigMap{}
			err = proxyCl.Get(context.TODO(), types.NamespacedName{Namespace: user.username, Name: cmName}, createdCM)
			require.NoError(t, err)

			require.NoError(t, err)
			require.NotEmpty(t, createdCM)
			require.Equal(t, "venus", createdCM.Data["planet"])
		})

		t.Run("try to create a resource in an unauthorized namespace", func(t *testing.T) {
			cmName := fmt.Sprintf("%s-appstudio-test-cm", user.username)
			expectedCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: hostAwait.Namespace, // user should not be allowed to create a resource in the host operator namespace
				},
				Data: map[string]string{
					"planet": "venus",
				},
			}

			proxyCl := hostAwait.CreateAPIProxyClient(user.token)
			err := proxyCl.Create(context.TODO(), expectedCM)
			require.NoError(t, err)

			t.Log("sleeping for 1 minute...")
			time.Sleep(1 * time.Minute)

			createdCM := &corev1.ConfigMap{}
			err = proxyCl.Get(context.TODO(), types.NamespacedName{Namespace: user.username, Name: cmName}, createdCM)
			require.NoError(t, err)

			require.NoError(t, err)
			require.NotEmpty(t, createdCM)
			require.Equal(t, "venus", createdCM.Data["planet"])
		})

		t.Run("delete usersignup and expect all resources to be deleted", func(t *testing.T) {
			// given
			signup, err := hostAwait.WaitForUserSignup(user.signup.Name)
			require.NoError(t, err)

			// when
			err = hostAwait.Client.Delete(context.TODO(), user.signup)

			// then
			require.NoError(t, err)
			t.Logf("usersignup '%s' deleted (resource name='%s')", user.username, signup.Name)

			err = hostAwait.WaitUntilMasterUserRecordDeleted(user.username)
			assert.NoError(t, err, "MasterUserRecord is not deleted")

			err = memberAwait.WaitUntilUserAccountDeleted(user.username)
			assert.NoError(t, err, "UserAccount is not deleted")

			err = memberAwait.WaitUntilUserDeleted(user.username)
			assert.NoError(t, err, "User is not deleted")

			err = memberAwait.WaitUntilIdentityDeleted(user.username)
			assert.NoError(t, err, "Identity is not deleted")

			err = memberAwait.WaitUntilNSTemplateSetDeleted(user.username)
			assert.NoError(t, err, "NSTemplateSet id not deleted")

			err = memberAwait.WaitUntilClusterResourceQuotasDeleted(user.username)
			assert.NoError(t, err, "ClusterResourceQuotas were not deleted")

			err = memberAwait.WaitUntilNamespaceDeleted(user.username, "dev")
			assert.NoError(t, err, "johnsmith-dev namespace is not deleted")

			err = memberAwait.WaitUntilNamespaceDeleted(user.username, "stage")
			assert.NoError(t, err, "johnsmith-stage namespace is not deleted")
		})
	} // end users loop

}
