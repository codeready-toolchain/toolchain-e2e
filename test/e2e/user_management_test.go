package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/condition"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gofrs/uuid"
	userv1 "github.com/openshift/api/user/v1"
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
	userSignupList := &toolchainv1alpha1.UserSignupList{}
	s.ctx, s.hostAwait, s.memberAwait, s.member2Await = WaitForDeployments(s.T(), userSignupList)
}

func (s *userManagementTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userManagementTestSuite) TestUserDeactivation() {
	s.hostAwait.UpdateToolchainConfig(
		testconfig.AutomaticApproval().Disabled(),
		testconfig.Deactivation().DeactivatingNotificationDays(-1))

	config := s.hostAwait.GetToolchainConfig()
	require.Equal(s.T(), -1, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

	s.T().Run("verify user deactivation on each member cluster", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		// User on member cluster 1
		userSignupMember1, murMember1 := s.newUserRequest().
			Username("usertodeactivate").
			Email("usertodeactivate@acme.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// User on member cluster 2
		userSignupMember2, murMember2 := s.newUserRequest().
			Username("usertodeactivate2").
			Email("usertodeactivate2@example.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.member2Await).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		t.Run("verify metrics are correct after creating usersignup", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // userSignupMember1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // userSignupMember1 is on member1
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // userSignupMember2
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.member2Await.ClusterName) // userSignupMember2 is on member2
		})

		s.deactivateAndCheckUser(userSignupMember1, murMember1)
		s.deactivateAndCheckUser(userSignupMember2, murMember2)

		t.Run("verify metrics are correct after deactivation", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 2)                                   // two more because of deactivated users
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

		userSignupMember1, murMember1 := s.newUserRequest().
			Username("usernodeactivate").
			Email("usernodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // 1 new signup
		metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // 1 more approved signup
		metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // signup not deactivated
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "internal")                       // 1 mur with email address `@redhat.com`
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)                // 1 user on member1 (userSignupMember1)
		metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName)               // no user on member2
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "internal") // 1 activation

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
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // 1 new signup
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // 1 more approved signup
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // signup not deactivated
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "internal")                       // 1 mur with email address `@redhat.com`
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)                // 1 user on member1 (userSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName)               // no user on member2
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "internal") // 1 activation
		})

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		manyManyDaysAgo := 999999999999999
		durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
		updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
		murMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
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
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                                                            // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)                                                    // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)                                                 // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 0)                                             // no change
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "internal")                       // 1 mur with email address `@redhat.com
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)                // 1 user on member1 (userSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName)               // no user on member2
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "internal") // 1 activation
		})
	})

	s.T().Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

		userSignupMember1, murMember1 := s.newUserRequest().
			Username("usertoautodeactivate").
			Email("usertoautodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		deactivationExcludedUserSignupMember1, excludedMurMember1 := s.newUserRequest().
			Username("userdeactivationexcluded").
			Email("userdeactivationexcluded@excluded.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Get the provisioned account's tier
		baseTier, err := s.hostAwait.WaitForNSTemplateTier("base")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name, murMember1.Status.ProvisionedTime.String())

		// Use the same method above to change the provisioned time for the excluded user
		excludedMurMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(excludedMurMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", excludedMurMember1.Name, excludedMurMember1.Status.ProvisionedTime.String())

		// The non-excluded user should be deactivated
		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(murMember1.Name)
		require.NoError(s.T(), err)
		userSignupMember1, err = s.hostAwait.WaitForUserSignup(userSignupMember1.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)
		require.True(t, states.Deactivated(userSignupMember1), "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = s.hostAwait.WaitForMasterUserRecord(excludedMurMember1.Name)
		require.NoError(s.T(), err)
		deactivationExcludedUserSignupMember1, err = s.hostAwait.WaitForUserSignup(deactivationExcludedUserSignupMember1.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)
		require.False(t, states.Deactivated(deactivationExcludedUserSignupMember1), "deactivationExcludedUserSignup should not be deactivated")

		t.Run("verify metrics are correct after auto deactivation", func(t *testing.T) {
			// Only the user with domain not on the exclusion list should be auto-deactivated
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 1)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "internal")         // userSignupMember1 was deactivated
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")         // deactivationExcludedUserSignupMember1 is still there
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 1, "cluster_name", s.memberAwait.ClusterName)  // 1 user left on member1 (deactivationExcludedUserSignupMember1)
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("test deactivating state set OK", func(t *testing.T) {
		// Reset configuration back to 3 days
		s.hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().Enabled(),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		config := s.hostAwait.GetToolchainConfig()
		require.Equal(s.T(), 3, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

		userSignupMember1, murMember1 := s.newUserRequest().
			Username("usertostartdeactivating").
			Email("usertostartdeactivating@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Get the provisioned account's tier
		baseTier, err := s.hostAwait.WaitForNSTemplateTier("base")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
		// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
		// period from the current time and setting this as the provisioned time should test the behaviour of the
		// deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = s.hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name,
			murMember1.Status.ProvisionedTime.String())

		// The user should be set to deactivating, but not deactivated
		_, err = s.hostAwait.WaitForUserSignup(userSignupMember1.Name, wait.UntilUserSignupHasConditions(
			ConditionSet(Default(), ApprovedByAdmin(), Deactivating())...))
		require.NoError(s.T(), err)

		// Verify resources have been provisioned
		VerifyResourcesProvisionedForSignup(t, s.hostAwait, userSignupMember1, "base", s.memberAwait)
	})

	s.T().Run("test full automatic user deactivation lifecycle", func(t *testing.T) {
		// Set configuration to 3 days
		s.hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().Enabled(),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		config := s.hostAwait.GetToolchainConfig()
		require.Equal(s.T(), 3, config.Spec.Host.Deactivation.DeactivatingNotificationDays)

		// Create a token and identity to sign up with
		identity0 := authsupport.NewIdentity()
		emailValue := uuid.Must(uuid.NewV4()).String() + "@redhat.com"
		emailClaim0 := authsupport.WithEmailClaim(emailValue)
		token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
		require.NoError(s.T(), err)

		// Call the signup endpoint
		invokeEndpoint(s.T(), "POST", s.hostAwait.RegistrationServiceURL+"/api/v1/signup", token0, "", http.StatusAccepted)

		// Wait for the UserSignup to be created
		userSignup, err := s.hostAwait.WaitForUserSignup(identity0.ID.String(),
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)

		// Wait the Master User Record to be provisioned
		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait)

		s.T().Run("user set to deactivating when provisioned time set in past", func(t *testing.T) {
			// Get the provisioned account's tier
			baseTier, err := s.hostAwait.WaitForNSTemplateTier("base")
			require.NoError(t, err)

			mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
			require.NoError(t, err)

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
			// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
			// period from the current time and setting this as the provisioned time should test the behaviour of the
			// deactivation controller reconciliation.
			tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
			mur, err = s.hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
			require.NoError(s.T(), err)
			s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
				mur.Status.ProvisionedTime.String())

			// The user should be set to deactivating, but not deactivated
			userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
				ConditionSet(Default(), ApprovedAutomatically(), Deactivating())...))
			require.NoError(s.T(), err)

			// Verify resources have been provisioned
			VerifyResourcesProvisionedForSignup(t, s.hostAwait, userSignup, "base", s.memberAwait)

			s.T().Run("user set to deactivated after deactivating", func(t *testing.T) {
				// Set the provisioned time even further back
				tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+4) * time.Hour * 24
				mur, err = s.hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
				})
				require.NoError(s.T(), err)
				s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
					mur.Status.ProvisionedTime.String())

				// Set the LastTransitionTime of the status to 3 days in the past
				deactivatingLastTransitionTime := metav1.Time{Time: time.Now().Add(time.Duration(-3) * time.Hour * 24)}

				// Update the LastTransitionTime of the DeactivatingNotificationCreated condition
				newConditions := make([]toolchainv1alpha1.Condition, len(userSignup.Status.Conditions))
				copy(newConditions, userSignup.Status.Conditions)
				for i, c := range newConditions {
					if c.Type == toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated {
						newCondition := toolchainv1alpha1.Condition{
							Type:               c.Type,
							Status:             c.Status,
							LastTransitionTime: deactivatingLastTransitionTime,
							Reason:             c.Reason,
							Message:            c.Message,
							LastUpdatedTime:    c.LastUpdatedTime,
						}
						newConditions[i] = newCondition
						break
					}
				}
				userSignup.Status.Conditions = newConditions

				// Confirm that the LastTransitionTime has been correctly set
				updated, found := condition.FindConditionByType(userSignup.Status.Conditions,
					toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated)
				require.True(t, found)
				require.Equal(t, deactivatingLastTransitionTime, updated.LastTransitionTime)

				// Save the updated UserSignup's Status
				require.NoError(t, s.hostAwait.Client.Status().Update(context.TODO(), userSignup))

				// Trigger a reconciliation of the deactivation controller by updating the MUR
				// - The SyncIndex property of the UserAccount is intended for the express purpose of triggering
				//   a reconciliation, so we set it to some new unique value here
				mur.Spec.UserAccounts[0].SyncIndex = uuid.Must(uuid.NewV4()).String()
				require.NoError(t, s.hostAwait.Client.Update(context.TODO(), mur))

				// The user should now be set to deactivated
				userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
					wait.UntilUserSignupHasConditions(ConditionSet(ApprovedAutomatically(), Deactivated())...))
				require.NoError(s.T(), err)
			})
		})
	})
}

func (s *userManagementTestSuite) TestUserReactivationsMetric() {

	s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())

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
			_, err := s.hostAwait.UpdateUserSignupSpec(usersignups[username].Name, func(usersignup *toolchainv1alpha1.UserSignup) {
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
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // user-0001 has been deleted but metric remains unchanged
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // (unchanged after other usersignup was deleted)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // (unchanged after other usersignup was deleted)

		// when deleting user "user-0002"
		err = s.hostAwait.Client.Delete(context.TODO(), usersignups["user-0002"])

		// then
		require.NoError(t, err)
		// and verify that the values of the `sandbox_users_per_activations` metric
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // (same offset as above)
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "2", "domain", "external") // user-0002 has been deleted but metric remains unchanged
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "3", "domain", "external") // (unchanged after other usersignup was deleted)

	})
}

func (s *userManagementTestSuite) TestUserBanning() {
	// Get metrics assertion helper for testing metrics
	metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait, []string{s.memberAwait.ClusterName, s.member2Await.ClusterName})

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {

		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

		// Create a new UserSignup without requiring verification, and confirm it was approved automatically
		userSignup, _ := s.newUserRequest().
			Username("banprovisioned").
			Email("banprovisioned@test.com").
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute().Resources()

		// Create the BannedUser
		s.createAndCheckBannedUser(userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically(), Banned())...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
		// confirm usersignup
		_, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically(), Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after user banned", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.memberAwait.ClusterName)  // no user on member1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("manually created usersignup with preexisting banneduser", func(t *testing.T) {
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// For this test, we don't want to create the UserSignup via the registration service (the next test does this)
		// Instead, we want to confirm the behaviour when a UserSignup with a banned email address is created manually
		userSignup := NewUserSignup(t, s.hostAwait, "testuser"+id, email)
		userSignup.Spec.TargetCluster = s.memberAwait.ClusterName

		// Create the UserSignup via the Kubernetes API
		err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' created", userSignup.Name)

		// Check the UserSignup is created
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
		require.NoError(s.T(), err)

		// Confirm that the user is banned
		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		mur, err := s.hostAwait.GetMasterUserRecord(wait.WithMurName("testuser" + id))
		require.NoError(s.T(), err)
		assert.Nil(s.T(), mur)
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after user signup", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // not provisioned because banned before signup
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 2)
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.memberAwait.ClusterName)  // no user on member1
			metricsAssertion.WaitForMetricDelta(UserAccountsMetric, 0, "cluster_name", s.member2Await.ClusterName) // no user on member2
		})
	})

	s.T().Run("register new user with preexisting ban", func(t *testing.T) {
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

		id := uuid.Must(uuid.NewV4()).String()
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
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

		// Create a new UserSignup and confirm it was approved automatically
		userSignup, mur := s.newUserRequest().
			Username("banandunban").
			Email("banandunban@test.com").
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute().Resources()

		// Create the BannedUser
		bannedUser := s.createAndCheckBannedUser(userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically(), Banned())...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
		// confirm usersignup
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically(), Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
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
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)

			// Confirm the MUR is created
			_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
			require.NoError(s.T(), err)
		})
	})
}

func (s *userManagementTestSuite) TestUserDisabled() {
	s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval())

	// Create UserSignup
	userSignup := CreateAndApproveSignup(s.T(), s.hostAwait, "janedoe", s.memberAwait.ClusterName)

	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait)

	// Get MasterUserRecord
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Disable MUR
	mur, err = s.hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
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
		mur, err = s.hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = false
		})
		require.NoError(s.T(), err)

		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait)
	})
}
