package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestMetricsOnWhenUsersReactivated activates and deactivates a few users, and check the metrics.
// user-0001 will be activated 1 time
// user-0002 will be activated 2 times
// user-0003 will be activated 3 times
func TestMetricsOnWhenUsersDeactivatedAndReactivated(t *testing.T) {
	// given
	hostAwait, memberAwait, _ := WaitForDeployments(t)
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName})
	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	// when
	for i := 1; i <= 3; i++ {
		username := fmt.Sprintf("user-%04d", i)
		usersignups[username] = CreateAndApproveSignup(t, hostAwait, username, memberAwait.ClusterName)
		for j := 1; j < i; j++ { // deactivate and reactivate as many times as necessary (based on its "number")
			// deactivate the user
			_, err := hostAwait.UpdateUserSignupSpec(usersignups[username].Name, func(usersignup *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(usersignup, true)
			})
			require.NoError(t, err)
			err = hostAwait.WaitUntilMasterUserRecordDeleted(username)
			require.NoError(t, err)
			// reactivate the user
			CreateAndApproveSignup(t, hostAwait, username, memberAwait.ClusterName, WithIdentityID(usersignups[username].Spec.Userid))
		}
	}

	// then verify the value of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // 1 activation

	t.Run("restart host-operator pod and verify that metrics are still available", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName})

		// when deleting the host-operator pod to emulate an operator restart during redeployment.
		err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

		// then check how much time it takes to restart and process all existing resources
		require.NoError(t, err)
		// host metrics should become available again at this point
		_, err = hostAwait.WaitForRouteToBeAvailable(hostAwait.Namespace, "host-operator-metrics-service", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics-service' service to be available")
		// also verify that the metric values "survived" the restart
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 was 1 time (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 was 2 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // user-0003 was 3 times (unchanged after pod restarted)
	})
}

func TestMetricsWhenUsersDeleted(t *testing.T) {
	// given
	hostAwait, memberAwait, _ := WaitForDeployments(t)
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName})
	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("user-%04d", i)
		usersignups[username] = CreateAndApproveSignup(t, hostAwait, username, memberAwait.ClusterName)
	}

	// when deleting user "user-0001"
	err := hostAwait.Client.Delete(context.TODO(), usersignups["user-0001"])

	// then
	require.NoError(t, err)
	// and verify that the values of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // user-0001 and user-0002 have been provisioned

	// when deleting user "user-0002"
	err = hostAwait.Client.Delete(context.TODO(), usersignups["user-0002"])

	// then
	require.NoError(t, err)
	// and verify that the values of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // same offset as above: users has been deleted but metric remains unchanged
}

func TestMetricsWhenUsersBanned(t *testing.T) {

	// given
	hostAwait, memberAwait, member2Await := WaitForDeployments(t)
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)

	t.Run("ban provisioned usersignup", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())
		// Create a new UserSignup and approve it manually
		userSignup, _ := NewSignupRequest(t, hostAwait, memberAwait, member2Await).
			Username("metricsbanprovisioned").
			Email("metricsbanprovisioned@test.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// when creating the BannedUser resource
		bannedUser := banUser(t, hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// then
		// confirm the user is banned
		_, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
		require.NoError(t, err)
		// verify the metrics
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)
		metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2

		t.Run("unban the banned user", func(t *testing.T) {
			// given
			metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

			// when unbaning the user
			err = hostAwait.Client.Delete(context.TODO(), bannedUser)
			require.NoError(t, err)

			// then
			// confirm the BannedUser resource is deleted
			err = hostAwait.WaitUntilBannedUserDeleted(bannedUser.Name)
			require.NoError(t, err)
			// verify the metrics
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 0)         // unchanged: user signup already existed
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // user approved
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)   // unchanged: banneduser already existed
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // user provisioned on member1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
		})
	})

	t.Run("manually created usersignup with preexisting banneduser", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())
		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		banUser(t, hostAwait, email)

		// when creating the usersignup directly (not via the registration service)
		userSignup := NewUserSignup(t, hostAwait, "testuser"+id, email)
		userSignup.Spec.TargetCluster = memberAwait.ClusterName
		err := hostAwait.CreateWithCleanup(context.TODO(), userSignup)
		require.NoError(t, err)

		// then
		// check the UserSignup is created
		userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name)
		require.NoError(t, err)
		// confirm that the user is banned
		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		mur, err := hostAwait.GetMasterUserRecord(wait.WithMurName("testuser" + id))
		require.NoError(t, err)
		assert.Nil(t, mur)
		// verify the metrics
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 0) // not provisioned because banned before signup
		metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
	})

	t.Run("register new user with preexisting ban", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())
		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		banUser(t, hostAwait, email)
		// get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity0 := authsupport.NewIdentity()
		emailClaim0 := authsupport.WithEmailClaim(email)
		token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
		require.NoError(t, err)
		route := hostAwait.RegistrationServiceURL

		// when calling the signup endpoint with a valid token to initiate a signup process
		req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token0)
		req.Header.Set("content-type", "application/json")
		resp, err := HTTPClient.Do(req)

		// then
		require.NoError(t, err)
		defer Close(t, resp)
		// verify the metrics
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 0)
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 0) // not provisioned because banned before signup
		metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)   // banneduser resource was created before signup
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2
	})

}

func TestMetricsWhenUserDisabled(t *testing.T) {
	// given
	hostAwait, memberAwait, member2Await := WaitForDeployments(t)
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval())
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

	// Create UserSignup
	userSignup := CreateAndApproveSignup(t, hostAwait, "janedoe", memberAwait.ClusterName)
	// Get MasterUserRecord
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(t, err)

	// when disabling MUR
	_, err = hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
		mur.Spec.Disabled = true
	})
	require.NoError(t, err)

	// then
	// verify the metrics
	metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
	metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // approved even though (temporarily) disabled
	metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // user is on member1
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName) // no user on member2

	t.Run("re-enabled mur", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

		// When re-enabling MUR
		mur, err = hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = false
		})
		require.NoError(t, err)

		// then
		// verify the metrics
		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 0)         // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 0) // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external") // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName) // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", member2Await.ClusterName)

	})
}

func banUser(t *testing.T, hostAwait *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	bannedUser := &toolchainv1alpha1.BannedUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.Must(uuid.NewV4()).String(),
			Namespace: hostAwait.Namespace,
			Labels: map[string]string{
				toolchainv1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: toolchainv1alpha1.BannedUserSpec{
			Email: email,
		},
	}
	err := hostAwait.CreateWithCleanup(context.TODO(), bannedUser)
	require.NoError(t, err)
	return bannedUser
}
