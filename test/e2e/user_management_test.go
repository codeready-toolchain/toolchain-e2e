package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	userv1 "github.com/openshift/api/user/v1"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestUserManagement(t *testing.T) {
	suite.Run(t, &userManagementTestSuite{})
}

type userManagementTestSuite struct {
	baseUserIntegrationTest
}

func (s *userManagementTestSuite) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.ctx, s.hostAwait, s.memberAwait, s.member2Await = WaitForDeployments(s.T(), userSignupList)
}

func (s *userManagementTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userManagementTestSuite) TestUserDeactivation() {
	s.hostAwait.UpdateHostOperatorConfig(
		test.AutomaticApproval().Enabled(),
		test.Deactivation().DeactivatingNotificationDays(-1))

	config := s.hostAwait.GetHostOperatorConfig()
	require.Equal(s.T(), -1, config.Spec.Deactivation.DeactivatingNotificationDays)

	s.T().Run("verify user deactivation on each member cluster", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		// User on member cluster 1
		userSignupMember1, murMember1 := s.createAndCheckUserSignup(true, "usertodeactivate", "usertodeactivate@redhat.com", s.memberAwait, ApprovedByAdmin()...)

		// User on member cluster 2
		userSignupMember2, murMember2 := s.createAndCheckUserSignup(true, "usertodeactivate2", "usertodeactivate2@example.com", s.member2Await, ApprovedByAdmin()...)

		t.Run("verify metrics are correct after creating usersignup", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 2)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // userSignupMember1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // userSignupMember1 is on member1
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // userSignupMember2
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.member2Await.ClusterName) // userSignupMember2 is on member2
		})

		s.deactivateAndCheckUser(userSignupMember1, murMember1)
		s.deactivateAndCheckUser(userSignupMember2, murMember2)

		t.Run("verify metrics are correct after deactivation", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 2)                                   // two more because of deactivated users
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 0)                                         // two less because of deactivated users
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")         // userSignupMember1 deactivated
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.memberAwait.ClusterName)  // userSignupMember1 deactivated
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")         // userSignupMember2 deactivated
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // userSignupMember1 deactivated
		})

		s.T().Run("reactivate a deactivated user", func(t *testing.T) {
			s.reactivateAndCheckUser(userSignupMember1, murMember1)
			s.reactivateAndCheckUser(userSignupMember2, murMember2)

			t.Run("verify metrics are correct after reactivating the user", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)                                              // no change
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 4)                                      // two more because of reactivated user
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 2)                                   // no change
				metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 2)                                         // two more because of reactivated user
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // userSignupMember1
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // userSignupMember1 is on member1
				metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // userSignupMember2
				metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.member2Await.ClusterName) // userSignupMember2 is on member2
			})
		})
	})

	s.T().Run("tests for tiers with automatic deactivation disabled", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		userSignupMember1, murMember1 := s.createAndCheckUserSignup(true, "usernodeactivate", "usernodeactivate@redhat.com", s.memberAwait, ApprovedByAdmin()...)

		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // 1 new signup
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // 1 more approved signup
		metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // signup not deactivated
		metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 1)                                                       // 1 mur for the approved signup
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")                       // 1 mur with email address `@redhat.com`
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)                // 1 user on member1 (userSignupMember1)
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName)               // no user on member2
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 1, "activations", "1")                                // 1 activation
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation

		// Get the base tier that has deactivation disabled
		baseDeactivationDisabledTier, err := s.hostAwait.WaitForNSTemplateTier("basedeactivationdisabled")
		require.NoError(t, err)

		// Move the user to the new tier without deactivation enabled
		murSyncIndex := MoveUserToTier(t, s.hostAwait, userSignupMember1.Spec.Username, *baseDeactivationDisabledTier).Spec.UserAccounts[0].SyncIndex
		murMember1, err = s.hostAwait.WaitForMasterUserRecord(murMember1.Name,
			wait.UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
			wait.UntilMasterUserRecordHasNotSyncIndex(murSyncIndex))
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after moving user to new tier", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                              // 1 new signup
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                      // 1 more approved signup
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                   // signup not deactivated
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 1)                                         // 1 mur for the approved signup
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // 1 mur with email address `@redhat.com`
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // 1 user on member1 (userSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		manyManyDaysAgo := 999999999999999
		durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
		updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
		murMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = updatedProvisionedTime
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", murMember1.Name)

		// Ensure the MUR has the updated ProvisionedTime
		_, err = s.hostAwait.WaitForMasterUserRecord(murMember1.Name, wait.UntilMasterUserRecordHasProvisionedTime(updatedProvisionedTime))
		require.NoError(s.T(), err)

		// The user should not be deactivated so the MUR should not be deleted, expect an error
		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(murMember1.Name)
		require.Error(s.T(), err)

		t.Run("verify metrics are correct after provisioned time changed", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                              // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                      // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                   // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 0)                               // no change
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 1)                                         // no change
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // 1 mur with email address `@redhat.com
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // 1 user on member1 (userSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		userSignupMember1, murMember1 := s.createAndCheckUserSignup(true, "usertoautodeactivate", "usertoautodeactivate@redhat.com", s.memberAwait, ApprovedByAdmin()...)
		deactivationExcludedUserSignupMember1, excludedMurMember1 := s.createAndCheckUserSignup(true, "userdeactivationexcluded", "userdeactivationexcluded@excluded.com", s.memberAwait, ApprovedByAdmin()...)

		// Get the provisioned account's tier
		baseTier, err := s.hostAwait.WaitForNSTemplateTier("base")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name, murMember1.Status.ProvisionedTime.String())

		// Use the same method above to change the provisioned time for the excluded user
		excludedMurMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(excludedMurMember1.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", excludedMurMember1.Name, excludedMurMember1.Status.ProvisionedTime.String())

		// The non-excluded user should be deactivated
		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(murMember1.Name)
		require.NoError(s.T(), err)
		userSignupMember1, err = s.hostAwait.WaitForUserSignup(userSignupMember1.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)
		require.True(t, states.Deactivated(userSignupMember1), "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = s.hostAwait.WaitForMasterUserRecord(excludedMurMember1.Name)
		require.NoError(s.T(), err)
		deactivationExcludedUserSignupMember1, err = s.hostAwait.WaitForUserSignup(deactivationExcludedUserSignupMember1.Name,
			wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)
		require.False(t, states.Deactivated(deactivationExcludedUserSignupMember1), "deactivationExcludedUserSignup should not be deactivated")

		t.Run("verify metrics are correct after auto deactivation", func(t *testing.T) {
			// Only the user with domain not on the exclusion list should be auto-deactivated
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 1)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 1)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")         // userSignupMember1 was deactivated
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // deactivationExcludedUserSignupMember1 is still there
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // 1 user left on member1 (deactivationExcludedUserSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("test deactivating state set OK", func(t *testing.T) {
		// Reset configuration back to 3 days
		s.hostAwait.UpdateHostOperatorConfig(
			test.AutomaticApproval().Enabled(),
			test.Deactivation().DeactivatingNotificationDays(3))

		config := s.hostAwait.GetHostOperatorConfig()
		require.Equal(s.T(), 3, config.Spec.Deactivation.DeactivatingNotificationDays)

		userSignupMember1, murMember1 := s.createAndCheckUserSignup(true, "usertostartdeactivating",
			"usertostartdeactivating@redhat.com", s.memberAwait, ApprovedByAdmin()...)

		// Get the provisioned account's tier
		baseTier, err := s.hostAwait.WaitForNSTemplateTier("base")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
		// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
		// period from the current time and setting this as the provisioned time should test the behaviour of the
		// deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name,
			murMember1.Status.ProvisionedTime.String())

		// The user should be set to deactivating, but not deactivated
		_, err = s.hostAwait.WaitForUserSignup(userSignupMember1.Name, wait.UntilUserSignupHasConditions(Deactivating()...))
		require.NoError(s.T(), err)

		// Verify resources have been provisioned
		VerifyResourcesProvisionedForSignup(t, s.hostAwait, userSignupMember1, "base", s.memberAwait)
	})
}

func (s *userManagementTestSuite) TestUserReactivationsMetric() {

	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

	// activate and deactivate a few users, and check the metrics.
	// user-0001 will be activated 1 time
	// user-0002 will be activated 2 times
	// user-0003 will be activated 3 times
	// user-0004 will be activated 4 times

	// Initialize metrics assertion counts
	metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})
	usersignups := map[string]*toolchainv1alpha1.UserSignup{}
	for i := 1; i <= 3; i++ {
		username := fmt.Sprintf("user-%04d", i)
		usersignups[username] = CreateAndApproveSignup(s.T(), s.hostAwait, username, s.memberAwait.ClusterName)

		for j := 1; j < i; j++ { // deactivate and reactivate as many times as necessary (based on its "number")
			// deactivate the user
			_, err := s.hostAwait.UpdateUserSignupSpec(usersignups[username].Name, func(usersignup *v1alpha1.UserSignup) {
				states.SetDeactivated(usersignup, true)
			})
			require.NoError(s.T(), err)
			err = s.hostAwait.WaitUntilMasterUserRecordDeleted(username)
			require.NoError(s.T(), err)
			// reactivate the user
			CreateAndApproveSignup(s.T(), s.hostAwait, username, s.memberAwait.ClusterName, WithIdentityID(usersignups[username].Spec.Userid))
		}
	}
	// then verify the value of the `sandbox_users_per_activations` metric
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 1, "activations", "1")                                // user-0001 was 1 time
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 1, "activations", "2")                                // user-0002 was 2 times
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 1, "activations", "3")                                // user-0003 was 3 times
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "2", "domain", "external") // 1 activation
	metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "3", "domain", "external") // 1 activation

	s.T().Run("restart host-operator pod and verify that metrics are still available", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		// when deleting the host-operator pod to emulate an operator restart during redeployment.
		err := s.hostAwait.DeletePods(client.MatchingLabels{"name": "host-operator"})

		// then check how much time it takes to restart and process all existing resources
		require.NoError(t, err)

		// host metrics should become available again at this point
		_, err = s.hostAwait.WaitForRouteToBeAvailable(s.hostAwait.Namespace, "host-operator-metrics", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics' service to be available")

		// then verify that the metric values "survived" the restart
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "1")                                // user-0001 was 1 time (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "2")                                // user-0002 was 2 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "3")                                // user-0003 was 3 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 was 1 time (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 was 2 times (unchanged after pod restarted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // user-0003 was 3 times (unchanged after pod restarted)
	})

	s.T().Run("delete usersignups", func(t *testing.T) {
		// given
		metricsAssertion := InitMetricsAssertion(t, s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		// when deleting user "user-0001"
		err := s.hostAwait.Client.Delete(context.TODO(), usersignups["user-0001"])

		// then
		require.NoError(t, err)
		// and verify that the values of the `sandbox_users_per_activations` metric
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "1")                                // user-0001 has been deleted but metric remains unchanged
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "2")                                // (unchanged after other usersignup was deleted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "3")                                // (unchanged after other usersignup was deleted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 has been deleted but metric remains unchanged
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // (unchanged after other usersignup was deleted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // (unchanged after other usersignup was deleted)

		// when deleting user "user-0002"
		err = s.hostAwait.Client.Delete(context.TODO(), usersignups["user-0002"])

		// then
		require.NoError(t, err)
		// and verify that the values of the `sandbox_users_per_activations` metric
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "1")                                // (same offset as above)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "2")                                // user-0002 has been deleted but metric remains unchanged
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsMetric, 0, "activations", "3")                                // (unchanged after other usersignup was deleted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // (same offset as above)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 has been deleted but metric remains unchanged
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // (unchanged after other usersignup was deleted)

	})
}

func (s *userManagementTestSuite) TestUserBanning() {
	// Get metrics assertion helper for testing metrics
	metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {

		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		// Create a new UserSignup and confirm it was approved automatically
		userSignup, _ := s.createAndCheckUserSignup(false, "banprovisioned",
			"banprovisioned@test.com", s.memberAwait, ApprovedAutomatically()...)

		// Create the BannedUser
		s.createAndCheckBannedUser(userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedAutomaticallyAndBanned()...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
		// confirm usersignup
		_, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedAutomaticallyAndBanned()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after user banned", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 0)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.memberAwait.ClusterName)  // no user on member1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("create usersignup with preexisting banneduser", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// Check that no MUR created
		userSignup := s.createAndCheckUserSignupNoMUR(false, "testuser"+id, email, s.memberAwait, Banned()...)
		assert.Equal(t, v1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		mur, err := s.hostAwait.GetMasterUserRecord(wait.WithMurName("testuser" + id))
		require.NoError(s.T(), err)
		assert.Nil(s.T(), mur)
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after user signup", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // not provisioned because banned before signup
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 2)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordMetric, 0)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.memberAwait.ClusterName)  // no user on member1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("register new user with preexisting ban", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity0 := authsupport.NewIdentity()
		emailClaim0 := authsupport.WithEmailClaim(email)
		token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
		require.NoError(s.T(), err)

		route := s.hostAwait.RegistrationServiceURL

		// Call signup endpoint with a valid token to initiate a signup process
		req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token0)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)
		assert.Equal(s.T(), http.StatusForbidden, resp.StatusCode)

		// Check the error.
		statusErr := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &statusErr)
		require.NoError(s.T(), err)
		require.Equal(s.T(), "forbidden: user has been banned", statusErr["message"])
	})

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		// Create a new UserSignup and confirm it was approved automatically
		userSignup, mur := s.createAndCheckUserSignup(false, "banandunban", "banandunban@test.com", s.memberAwait, ApprovedAutomatically()...)

		// Create the BannedUser
		bannedUser := s.createAndCheckBannedUser(userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedAutomaticallyAndBanned()...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
		// confirm usersignup
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedAutomaticallyAndBanned()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(s.T(), err)

		t.Run("unban the banned user", func(t *testing.T) {
			// Unban the user
			err = s.hostAwait.Client.Delete(context.TODO(), bannedUser)
			require.NoError(s.T(), err)

			// Confirm the BannedUser is deleted
			err = s.hostAwait.WaitUntilBannedUserDeleted(bannedUser.Name)
			require.NoError(s.T(), err)

			// Confirm the user is provisioned
			userSignup, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedAutomatically()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)

			// Confirm the MUR is created
			_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
			require.NoError(s.T(), err)
		})
	})
}

func (s *userManagementTestSuite) TestUserDisabled() {
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval())

	// Create UserSignup
	userSignup := CreateAndApproveSignup(s.T(), s.hostAwait, "janedoe", s.memberAwait.ClusterName)

	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait)

	// Get MasterUserRecord
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Disable MUR
	mur, err = s.hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *v1alpha1.MasterUserRecord) {
		mur.Spec.Disabled = true
	})
	require.NoError(s.T(), err)

	// Wait until the UserAccount status is disabled
	userAccount, err := s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(Disabled()))
	require.NoError(s.T(), err)

	// Wait until the MUR status is disabled
	mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username,
		wait.UntilMasterUserRecordHasConditions(Disabled(), ProvisionedNotificationCRCreated()))
	require.NoError(s.T(), err)

	// Check that the UserAccount is now set to disabled
	require.True(s.T(), userAccount.Spec.Disabled)

	// Check the User is deleted
	user := &userv1.User{}
	err = s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: userAccount.Namespace}, user)
	require.Error(s.T(), err)
	assert.True(s.T(), apierrors.IsNotFound(err))

	// Check the Identity is deleted
	identity := &userv1.Identity{}
	err = s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(userAccount.Spec.UserID)}, identity)
	require.Error(s.T(), err)
	assert.True(s.T(), apierrors.IsNotFound(err))

	s.Run("re-enabled mur", func() {
		// Re-enable MUR
		mur, err = s.hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = false
		})
		require.NoError(s.T(), err)

		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait)
	})
}
