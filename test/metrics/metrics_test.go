package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestMetricsWhenUsersDeactivated verifies that `UserSignupsDeactivatedMetric` counter is increased when users are deactivated
// (`UsersPerActivationsAndDomainMetric` gauge and `UserSignupsApprovedMetric` counter remain unchanged)
func TestMetricsWhenUsersDeactivated(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	t.Cleanup(func() {
		// wait until metrics are back to their respective baselines
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait.ClusterName)
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait2.ClusterName)
	})

	usersignups := map[string]*toolchainv1alpha1.UserSignup{}
	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("user-%04d", i)

		// Create UserSignup
		usersignups[username], _ = NewSignupRequest(awaitilities).
			Username(username).
			Email(username + "@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait2).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).
			Resources()
	}
	// checking the metrics after creation/before deactivation, so we can better understand the changes after deactivations occurred.
	metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 2)                                                            // all signups
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all activated
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 2)                                                    // all activated
	metricsAssertion.WaitForMetricDelta(t, UserSignupsDeactivatedMetric, 0)                                                 // none deactivated
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 2, "cluster_name", memberAwait2.ClusterName) // 2 spaces created on member-2

	// when deactivating the users
	for username, usersignup := range usersignups {
		_, err := hostAwait.UpdateUserSignup(t, usersignup.Name,
			func(usersignup *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(usersignup, true)
			})
		require.NoError(t, err)

		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, username)
		require.NoError(t, err)

		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, username)
		require.NoError(t, err)
	}

	// then verify the value of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 2)                                                            // all signups (even if deactivated)
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all deactivated (but this metric is never decremented)
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 2)                                                    // all deactivated (but counters are never decremented)
	metricsAssertion.WaitForMetricDelta(t, UserSignupsDeactivatedMetric, 2)                                                 // all deactivated
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // 2 spaces deleted from member-2

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
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	t.Cleanup(func() {
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait.ClusterName) // wait until counter is back to 0
	})

	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	// when
	for i := 1; i <= 3; i++ {
		username := fmt.Sprintf("user-%04d", i)

		usersignups[username], _ = NewSignupRequest(awaitilities).
			Username(username).
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).
			Resources()

		for j := 1; j < i; j++ { // deactivate and reactivate as many times as necessary (based on its "number")
			// deactivate the user
			_, err := hostAwait.UpdateUserSignup(t, usersignups[username].Name,
				func(usersignup *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(usersignup, true)
				})
			require.NoError(t, err)

			err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, username)
			require.NoError(t, err)

			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, username)
			require.NoError(t, err)

			// reactivate the user
			usersignups[username], _ = NewSignupRequest(awaitilities).
				IdentityID(uuid.Must(uuid.FromString(usersignups[username].Spec.Userid))).
				Username(username).
				ManuallyApprove().
				TargetCluster(memberAwait).
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute(t).
				Resources()
		}
	}

	// then verify the value of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "internal") // no activation
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "internal") // no activation
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "internal") // no activation

	t.Run("restart host-operator pod and verify that metrics are still available", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, awaitilities)

		// when deleting the host-operator pod to emulate an operator restart during redeployment.
		err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

		// then check how much time it takes to restart and process all existing resources
		require.NoError(t, err)
		// host metrics should become available again at this point
		_, err = hostAwait.WaitForRouteToBeAvailable(t, hostAwait.Namespace, "host-operator-metrics-service", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics-service' service to be available")
		// also verify that the metric values "survived" the restart
		metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 was 1 time (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "internal") // no activation
		metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 was 2 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "internal") // no activation
		metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // user-0003 was 3 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "internal") // no activation
	})
}

// TestMetricsWhenUsersDeleted verifies that the `UsersPerActivationsAndDomainMetric` metric is NOT decreased when users are deleted
func TestMetricsWhenUsersDeleted(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	t.Cleanup(func() {
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait.ClusterName) // wait until counter is back to 0
	})

	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("user-%04d", i)
		usersignups[username], _ = NewSignupRequest(awaitilities).
			Username(username).
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).
			Resources()
	}

	// when deleting user "user-0001"
	err := hostAwait.Client.Delete(context.TODO(), usersignups["user-0001"])
	// then
	require.NoError(t, err)

	// wait for space to be deleted
	err = hostAwait.WaitUntilUserSignupDeleted(t, usersignups["user-0001"].GetName())
	require.NoError(t, err)
	err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, usersignups["user-0001"].GetName())
	require.NoError(t, err)

	// and verify that the values of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // user-0001 and user-0002 have been provisioned

	// when deleting user "user-0002"
	err = hostAwait.Client.Delete(context.TODO(), usersignups["user-0002"])

	// then
	require.NoError(t, err)

	// wait for space to be deleted
	err = hostAwait.WaitUntilUserSignupDeleted(t, usersignups["user-0002"].GetName())
	require.NoError(t, err)
	err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, usersignups["user-0002"].GetName())
	require.NoError(t, err)

	// and verify that the values of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // same offset as above: users has been deleted but metric remains unchanged
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
	t.Cleanup(func() {
		t.Log("waiting for metrics to get back to their baseline values...")
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait.ClusterName)  // wait until counter is back to 0
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait2.ClusterName) // wait until counter is back to 0
	})

	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	// Create a new UserSignup and approve it manually
	userSignup, _ := NewSignupRequest(awaitilities).
		Username("metricsbanprovisioned").
		Email("metricsbanprovisioned@test.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).Resources()

	// when creating the BannedUser resource
	bannedUser := banUser(t, hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

	// then
	// confirm the user is banned
	_, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
	require.NoError(t, err)
	// verify the metrics
	metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 1)
	metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 1)
	metricsAssertion.WaitForMetricDelta(t, UserSignupsBannedMetric, 1)
	metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "external")
	metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

	t.Run("unban the banned user", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, awaitilities)

		// when unbaning the user
		err = hostAwait.Client.Delete(context.TODO(), bannedUser)
		require.NoError(t, err)

		// then
		// confirm the BannedUser resource is deleted
		err = hostAwait.WaitUntilBannedUserDeleted(t, bannedUser.GetName())
		require.NoError(t, err)
		// wait for space to be deleted
		err = hostAwait.WaitUntilUserSignupDeleted(t, bannedUser.GetName())
		require.NoError(t, err)
		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, bannedUser.GetName())
		require.NoError(t, err)
		// verify the metrics
		metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 0)         // unchanged: user signup already existed
		metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 1) // user approved
		metricsAssertion.WaitForMetricDelta(t, UserSignupsBannedMetric, 0)   // unchanged: banneduser already existed
		metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 1, "domain", "external")
		metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
		metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // space provisioned on member1
		metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // no spaces on member2
	})
}

// TestMetricsWhenUserDisabled verifies that there is no impact on metrics when a user is re-enabled after being disabled
func TestMetricsWhenUserDisabled(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	// host metrics should be available at this point
	VerifyHostMetricsService(t, hostAwait)
	VerifyMemberMetricsService(t, memberAwait)
	metricsAssertion := InitMetricsAssertion(t, awaitilities)
	t.Cleanup(func() {
		t.Log("waiting for metrics to get back to their baseline values...")
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait.ClusterName)  // wait until counter is back to 0
		metricsAssertion.WaitForMetricBaseline(t, SpacesMetric, "cluster_name", memberAwait2.ClusterName) // wait until counter is back to 0
	})

	// Create UserSignup
	_, mur := NewSignupRequest(awaitilities).
		Username("janedoe").
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t).
		Resources()

	metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 1)
	metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 1) // approved
	metricsAssertion.WaitForMetricDelta(t, UserSignupsBannedMetric, 0)
	metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // space present on member1
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // no space on member2

	// when disabling MUR
	_, err := hostAwait.UpdateMasterUserRecordSpec(t, mur.Name,
		func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = true
		})
	require.NoError(t, err)

	// then
	// verify the metrics
	metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 1)
	metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 1) // still approved even though (temporarily) disabled
	metricsAssertion.WaitForMetricDelta(t, UserSignupsBannedMetric, 0)
	metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // space is on member1
	metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // no space on member2

	t.Run("re-enabled mur", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, awaitilities)

		// When re-enabling MUR
		mur, err = hostAwait.UpdateMasterUserRecordSpec(t, mur.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Spec.Disabled = false
			})
		require.NoError(t, err)

		// then
		// verify the metrics
		metricsAssertion.WaitForMetricDelta(t, UserSignupsMetric, 0)         // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(t, UserSignupsApprovedMetric, 0) // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(t, UserSignupsBannedMetric, 0)
		metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "external") // unchanged, user was already provisioned
		metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
		metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
		metricsAssertion.WaitForMetricDelta(t, SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

	})
}

func banUser(t *testing.T, hostAwait *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	bannedUser := &toolchainv1alpha1.BannedUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.Must(uuid.NewV4()).String(),
			Namespace: hostAwait.Namespace,
			Labels: map[string]string{
				toolchainv1alpha1.BannedUserEmailHashLabelKey: hash.EncodeString(email),
			},
		},
		Spec: toolchainv1alpha1.BannedUserSpec{
			Email: email,
		},
	}
	err := hostAwait.CreateWithCleanup(t, bannedUser)
	require.NoError(t, err)
	return bannedUser
}
