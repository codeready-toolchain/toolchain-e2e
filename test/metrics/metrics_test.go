package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestMetricsWhenUsersDeactivated verifies that `UserSignupsDeactivatedMetric` counter is increased when users are deactivated
// (`UsersPerActivationsAndDomainMetric` gauge and `UserSignupsApprovedMetric` counter remain unchanged)
func TestMetricsWhenUsersDeactivated(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	usersignups := map[string]*toolchainv1alpha1.UserSignup{}
	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("user-%04d", i)

		// Create UserSignup
		usersignups[username], _ = NewSignupRequest(t, awaitilities).
			Username(username).
			Email(username + "@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait2).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()
	}
	// checking the metrics after creation/before deactivation, so we can better understand the changes after deactivations occurred.
	metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)                                                            // all signups
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all activated
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)                                                    // all activated
	metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // none deactivated
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)                  // none activated on member-1
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 2, "cluster_name", memberAwait2.ClusterName)                 // all activated on member-2
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 2, "cluster_name", memberAwait2.ClusterName)

	// when deactivating the users
	for username, usersignup := range usersignups {
		_, err := hostAwait.UpdateUserSignup(usersignup.Name, func(usersignup *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(usersignup, true)
		})
		require.NoError(t, err)

		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(username)
		require.NoError(t, err)

		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(username)
		require.NoError(t, err)
	}

	// then verify the value of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)                                                            // all signups (even if deactivated)
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all deactivated (but this metric is never decremented)
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)                                                    // all deactivated (but counters are never decremented)
	metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 2)                                                 // all deactivated
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)                  // all deactivated on member-1
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait2.ClusterName)                 // all deactivated on member-2
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

}

// TestMetricsWhenUsersReactivated activates and deactivates a few users, and check the metrics.
// user-0001 will be activated 1 time
// user-0002 will be activated 2 times
// user-0003 will be activated 3 times
func TestMetricsWhenUsersDeactivatedAndReactivated(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	// when
	for i := 1; i <= 3; i++ {
		username := fmt.Sprintf("user-%04d", i)

		usersignups[username], _ = NewSignupRequest(t, awaitilities).
			Username(username).
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()

		for j := 1; j < i; j++ { // deactivate and reactivate as many times as necessary (based on its "number")
			// deactivate the user
			_, err := hostAwait.UpdateUserSignup(usersignups[username].Name, func(usersignup *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(usersignup, true)
			})
			require.NoError(t, err)

			err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(username)
			require.NoError(t, err)

			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(username)
			require.NoError(t, err)

			// reactivate the user
			usersignups[username], _ = NewSignupRequest(t, awaitilities).
				IdentityID(uuid.Must(uuid.FromString(usersignups[username].Spec.Userid))).
				Username(username).
				ManuallyApprove().
				TargetCluster(memberAwait).
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().
				Resources()
		}
	}

	// then verify the value of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "internal") // no activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "internal") // no activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "internal") // no activation
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)

	t.Run("restart host-operator pod and verify that metrics are still available", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, awaitilities)

		// when deleting the host-operator pod to emulate an operator restart during redeployment.
		err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

		// then check how much time it takes to restart and process all existing resources
		require.NoError(t, err)
		// host metrics should become available again at this point
		_, err = hostAwait.WaitForRouteToBeAvailable(hostAwait.Namespace, "host-operator-metrics-service", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics-service' service to be available")
		// also verify that the metric values "survived" the restart
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 was 1 time (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "internal") // no activation
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 was 2 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "internal") // no activation
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // user-0003 was 3 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "internal") // no activation
		metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	})
}

// TestMetricsWhenUsersDeleted verifies that the `UsersPerActivationsAndDomainMetric` metric is NOT decreased when users are deleted
func TestMetricsWhenUsersDeleted(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("user-%04d", i)
		usersignups[username], _ = NewSignupRequest(t, awaitilities).
			Username(username).
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()
	}

	// when deleting user "user-0001"
	err := hostAwait.Client.Delete(context.TODO(), usersignups["user-0001"])

	// then
	require.NoError(t, err)
	// and verify that the values of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // user-0001 and user-0002 have been provisioned

	// and verify that the values of the `sandbox_spaces_current` metric
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 2, "cluster_name", memberAwait.ClusterName)

	// when deleting user "user-0002"
	err = hostAwait.Client.Delete(context.TODO(), usersignups["user-0002"])

	// then
	require.NoError(t, err)
	// and verify that the values of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // same offset as above: users has been deleted but metric remains unchanged

	// and verify that the values of the `sandbox_spaces_current` metric drops after user deletion
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)
}

// TestMetricsWhenUsersBanned verifies that the relevant gauges are decreased when a user is banned, and increased again when unbanned
func TestMetricsWhenUsersBanned(t *testing.T) {

	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)

	// given
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))
	// Create a new UserSignup and approve it manually
	userSignup, _ := NewSignupRequest(t, awaitilities).
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
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait.ClusterName)  // no user on member1
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait2.ClusterName) // no user on member2
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

	t.Run("unban the banned user", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, awaitilities)

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
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // user provisioned on member1
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait2.ClusterName) // no user on member2
		metricsAssertion.WaitForMetricDelta(SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)        // space provisioned on member1
		metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)       // no spaces on member2
	})
}

// TestMetricsWhenUserDisabled verifies that there is no impact on metrics when a user is re-enabled after being disabled
func TestMetricsWhenUserDisabled(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)

	// Create UserSignup
	_, mur := NewSignupRequest(t, awaitilities).
		Username("janedoe").
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().
		Resources()

	metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
	metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // approved
	metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // user is on member1
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait2.ClusterName) // no user on member2
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)        // space present on member1
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)       // no space on member2

	// when disabling MUR
	_, err := hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
		mur.Spec.Disabled = true
	})
	require.NoError(t, err)

	// then
	// verify the metrics
	metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
	metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // still approved even though (temporarily) disabled
	metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 0)
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", memberAwait.ClusterName)  // user is on member1
	metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait2.ClusterName) // no user on member2
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)        // space is on member1
	metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)       // no space on member2

	t.Run("re-enabled mur", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, awaitilities)

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
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", memberAwait2.ClusterName)
		metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
		metricsAssertion.WaitForMetricDelta(SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

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
