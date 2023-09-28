package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestOperatorVersionMetrics(t *testing.T) {

	// given
	awaitilities := WaitForDeployments(t)

	t.Run("host-operator", func(t *testing.T) {
		// given
		hostAwait := awaitilities.Host()
		// host metrics should be available at this point
		hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)

		// when
		labels := hostAwait.GetMetricLabels(t, wait.HostOperatorVersionMetric)

		// verify that the "version" metric exists for Host Operator and that it has a non-empty `commit` label
		require.Len(t, labels, 1)
		commit := labels[0]["commit"]
		require.NotNil(t, commit)
		assert.Len(t, *commit, 7)
	})

	t.Run("member-operators", func(t *testing.T) {
		// given
		member1Await := awaitilities.Member1()
		member2Await := awaitilities.Member1()
		// member metrics should be available at this point
		member1Await.InitMetrics(t)
		member2Await.InitMetrics(t)

		// --- member1 ---
		// when
		labels := member1Await.GetMetricLabels(t, wait.MemberOperatorVersionMetric)

		// verify that the "version" metric exists for the first Member Operator and that it has a non-empty `commit` label
		require.Len(t, labels, 1)
		commit1 := labels[0]["commit"]
		require.NotNil(t, commit1)
		assert.Len(t, *commit1, 7)

		// --- member2 ---
		// when
		labels = member2Await.GetMetricLabels(t, wait.MemberOperatorVersionMetric)

		// verify that the "version" metric exists for the second Member Operator and that it has a non-empty `commit` label
		require.Len(t, labels, 1)
		commit2 := labels[0]["commit"]
		require.NotNil(t, commit2)
		assert.Len(t, *commit2, 7)

		// expect the same version on member1 and member2
		assert.Equal(t, *commit1, *commit2)
	})
}

// TestMetricsWhenUsersManuallyApproved verifies that `UserSignupsApprovedMetric` and `UserSignupsApprovedWithMethodMetric` counters are increased when users are approved
// (also verifies `UsersPerActivationsAndDomainMetric` gauge and `UserSignupsApprovedMetric` counter remain unchanged after deactivation)
func TestMetricsWhenUsersManuallyApprovedAndThenDeactivated(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		// wait until metrics are back to their respective baselines
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName)
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait2.ClusterName)
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
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t).
			Resources()
	}
	// checking the metrics after creation/before deactivation, so we can better understand the changes after deactivations occurred.
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 2)                                                            // all signups
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all activated
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 2)                                                    // all activated
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic")                   // not automatically approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 2, "method", "manual")                      // both manually approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsDeactivatedMetric, 0)                                                 // none deactivated
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 2, "cluster_name", memberAwait2.ClusterName) // 2 spaces created on member-2

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
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 2)                                                            // all signups (even if deactivated)
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all deactivated (but this metric is never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 0)                                                    // all deactivated (but counters are never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic")                   // all deactivated (but counters are never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 2, "method", "manual")                      // all deactivated (but counters are never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsDeactivatedMetric, 2)                                                 // all deactivated
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // 2 spaces deleted from member-2

}

// TestMetricsWhenUsersAutomaticallyApproved verifies that `UserSignupsApprovedMetric` and `UserSignupsApprovedWithMethodMetric` counters are increased when users are approved
func TestMetricsWhenUsersAutomaticallyApprovedAndThenDeactivated(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))
	// host metrics should be available at this point
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		// wait until metrics are back to their respective baselines
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName)
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait2.ClusterName)
	})

	usersignups := map[string]*toolchainv1alpha1.UserSignup{}
	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("userautoapprove-%04d", i)

		// Create UserSignup
		usersignups[username], _ = NewSignupRequest(awaitilities).
			Username(username).
			Email(username + "@redhat.com").
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
			Execute(t).
			Resources()
	}
	// checking the metrics after creation/before deactivation, so we can better understand the changes after deactivations occurred.
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 2)                                                            // all signups
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all activated
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 2)                                                    // all activated
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 2, "method", "automatic")                   // both automatically approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "manual")                      // not manually approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsDeactivatedMetric, 0)                                                 // none deactivated

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
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 2)                                                            // all signups (even if deactivated)
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "internal") // all deactivated (but this metric is never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // never incremented
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 2)                                                    // all deactivated (but counters are never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 2, "method", "automatic")                   // all deactivated (but counters are never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "manual")                      // all deactivated (but counters are never decremented)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsDeactivatedMetric, 2)                                                 // all deactivated

}

// TestVerificationRequiredMetric verifies that `UserSignupVerificationRequiredMetric` counters are increased only once when users are created/reactivated
func TestVerificationRequiredMetric(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	route := hostAwait.RegistrationServiceURL
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false)) // disable automatic approval so that users are created with verification required
	// host metrics should be available at this point
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		// wait until metrics are back to their respective baselines
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName)
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait2.ClusterName)
	})

	var userSignup *toolchainv1alpha1.UserSignup
	t.Run("VerificationRequiredMetric", func(t *testing.T) {
		// given
		username := "user-verification-required"
		// Create a token and identity to sign up with
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@some.domain"
		identity0, token0, err := authsupport.NewToken(authsupport.WithEmail(emailAddress))
		require.NoError(t, err)

		// when
		// Create UserSignup with verification required
		InvokeEndpoint(t, "POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)

		// Wait for the UserSignup to be created and in verification required status
		userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))

		// then
		require.NoError(t, err)
		cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)

		// Confirm the CompliantUsername has NOT been set, since verification is required and it hasn't been approved yet
		require.Empty(t, userSignup.Status.CompliantUsername)
		// verify the value of the `sandbox_user_signups_verification_required_total` metric
		hostAwait.WaitForMetricDelta(t, wait.UserSignupVerificationRequiredMetric, 1) // user is pending verification

		// Pending verification metric should only be incremented the first time verification is required.
		// Try entering a verification code and verify that the metric is not incremented.
		t.Run("no change to metric when verification initiated", func(t *testing.T) {
			// when
			// Initiate the verification process
			InvokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", token0,
				`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

			// then
			// Retrieve the updated UserSignup
			userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username)
			require.NoError(t, err)
			// Confirm there is a verification code annotation value, and store it in a variable
			verificationCode := userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey]
			require.NotEmpty(t, verificationCode)
			// Attempt to verify with an incorrect verification code
			InvokeEndpoint(t, "GET", route+"/api/v1/signup/verification/invalid", token0, "", http.StatusForbidden)
			hostAwait.WaitForMetricDelta(t, wait.UserSignupVerificationRequiredMetric, 1) // no change after verification initiated
		})

		t.Run("no change to metric when user deactivated", func(t *testing.T) {
			// when deactivating the user
			_, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
				func(usersignup *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(usersignup, true)
				})

			// then
			require.NoError(t, err)
			err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, username)
			require.NoError(t, err)
			err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, username)
			require.NoError(t, err)
			hostAwait.WaitForMetricDelta(t, wait.UserSignupVerificationRequiredMetric, 1) // no change
		})

		t.Run("metric incremented when user reactivated", func(t *testing.T) {
			// when reactivating the user
			InvokeEndpoint(t, "POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)
			userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))

			// then
			require.NoError(t, err)
			hostAwait.WaitForMetricDelta(t, wait.UserSignupVerificationRequiredMetric, 2) // additional pending verification since user was reactivated
		})
	})
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
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName) // wait until counter is back to 0
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
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
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
				RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
				Execute(t).
				Resources()
		}
	}

	// then verify the value of the `sandbox_users_per_activations` metric
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "internal") // no activation
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // 1 activation
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "internal") // no activation
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // 1 activation
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "internal") // no activation

	t.Run("restart host-operator pod and verify that metrics are still available", func(t *testing.T) {
		// when deleting the host-operator pod to emulate an operator restart during redeployment.
		err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

		// then check how much time it takes to restart and process all existing resources
		require.NoError(t, err)
		// host metrics should become available again at this point
		_, err = hostAwait.WaitForRouteToBeAvailable(t, hostAwait.Namespace, "host-operator-metrics-service", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics-service' service to be available")
		// also verify that the metric values "survived" the restart
		hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // user-0001 was 1 time (unchanged after pod restarted)
		hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "internal") // no activation
		hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // user-0002 was 2 times (unchanged after pod restarted)
		hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "internal") // no activation
		hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // user-0003 was 3 times (unchanged after pod restarted)
		hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "internal") // no activation
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
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName) // wait until counter is back to 0
	})

	usersignups := map[string]*toolchainv1alpha1.UserSignup{}

	for i := 1; i <= 2; i++ {
		username := fmt.Sprintf("user-%04d", i)
		usersignups[username], _ = NewSignupRequest(awaitilities).
			Username(username).
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
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
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // user-0001 and user-0002 have been provisioned

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
	hostAwait.WaitForMetricDelta(t, wait.UsersPerActivationsAndDomainMetric, 2, "activations", "1", "domain", "external") // same offset as above: users has been deleted but metric remains unchanged
}

// TestMetricsWhenUsersBanned verifies that the relevant gauges are decreased when a user is banned, and increased again when unbanned
func TestMetricsWhenUsersBanned(t *testing.T) {

	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	// given
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		t.Log("waiting for metrics to get back to their baseline values...")
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName)  // wait until counter is back to 0
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait2.ClusterName) // wait until counter is back to 0
	})

	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	// Create a new UserSignup and approve it manually
	userSignup, _ := NewSignupRequest(awaitilities).
		Username("metricsbanprovisioned").
		Email("metricsbanprovisioned@test.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).Resources()

	// when creating the BannedUser resource
	bannedUser := banUser(t, hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

	// then
	// confirm the user is banned
	_, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Banned())...))
	require.NoError(t, err)
	// verify the metrics
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 1)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 1)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic")
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 1, "method", "manual")
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsBannedMetric, 1)
	hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 0, "domain", "external")
	hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait.ClusterName)
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

	t.Run("unban the banned user", func(t *testing.T) {
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
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 1)                                          // unchanged: user signup already existed
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 2)                                  // user approved
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic") // unchanged: unbanning uses previous method of approval
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 2, "method", "manual")    // unbanning uses previous method of approval
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsBannedMetric, 1)                                    // unchanged: banneduser already existed
		hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 1, "domain", "external")
		hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
		hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // space provisioned on member1
		hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // no spaces on member2
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
	hostAwait.InitMetrics(t, awaitilities.Member1().ClusterName, awaitilities.Member2().ClusterName)
	t.Cleanup(func() {
		t.Log("waiting for metrics to get back to their baseline values...")
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait.ClusterName)  // wait until counter is back to 0
		hostAwait.WaitForMetricBaseline(t, wait.SpacesMetric, "cluster_name", memberAwait2.ClusterName) // wait until counter is back to 0
	})

	// Create UserSignup
	_, mur := NewSignupRequest(awaitilities).
		Username("janedoe").
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).
		Resources()

	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 1)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 1)                                  // approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic") // not automatically approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 1, "method", "manual")    // manually approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsBannedMetric, 0)
	hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // space present on member1
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // no space on member2

	// when disabling MUR
	_, err := hostAwait.UpdateMasterUserRecordSpec(t, mur.Name,
		func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = true
		})
	require.NoError(t, err)

	// then
	// verify the metrics
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 1)
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 1)                                  // still approved even though (temporarily) disabled
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic") // not automatically approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 1, "method", "manual")    // manually approved
	hostAwait.WaitForMetricDelta(t, wait.UserSignupsBannedMetric, 0)
	hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
	hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 1, "domain", "external")
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // space is on member1
	hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName) // no space on member2

	t.Run("re-enabled mur", func(t *testing.T) {
		// When re-enabling MUR
		mur, err = hostAwait.UpdateMasterUserRecordSpec(t, mur.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Spec.Disabled = false
			})
		require.NoError(t, err)

		// then
		// verify the metrics
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsMetric, 1)                                          // unchanged, user was already provisioned
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedMetric, 1)                                  // unchanged, user was already provisioned
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 0, "method", "automatic") // unchanged, user was already provisioned
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsApprovedWithMethodMetric, 1, "method", "manual")    // unchanged, user was already provisioned
		hostAwait.WaitForMetricDelta(t, wait.UserSignupsBannedMetric, 0)
		hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 0, "domain", "internal")
		hostAwait.WaitForMetricDelta(t, wait.MasterUserRecordsPerDomainMetric, 1, "domain", "external") // unchanged, user was already provisioned
		hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 1, "cluster_name", memberAwait.ClusterName)  // unchanged, user was already provisioned
		hostAwait.WaitForMetricDelta(t, wait.SpacesMetric, 0, "cluster_name", memberAwait2.ClusterName)

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
