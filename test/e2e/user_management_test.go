package e2e

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

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
	s.ctx, s.hostAwait, s.memberAwait = WaitForDeployments(s.T(), userSignupList)
}

func (s *userManagementTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userManagementTestSuite) TestUserDeactivation() {
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

	s.T().Run("deactivate a user", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait)

		userSignup, mur := s.createAndCheckUserSignup(true, "usertodeactivate", "usertodeactivate@redhat.com", true, ApprovedByAdmin()...)

		t.Run("verify metrics are correct after creating usersignup", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)
		})

		userSignup, err := s.hostAwait.UpdateUserSignupSpec(userSignup.Name, func(us *v1alpha1.UserSignup) {
			us.Spec.Deactivated = true
		})
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
		require.NoError(s.T(), err)

		// "deactivated"
		notifications, err := s.hostAwait.WaitForNotifications(userSignup.Status.CompliantUsername, "-deactivated", wait.UntilNotificationHasConditions(Sent()))
		require.NoError(t, err)
		require.NotEmpty(t, notifications)
		require.Equal(t, 1, len(notifications))
		notification := notifications[0]
		assert.Contains(t, notification.Name, userSignup.Status.CompliantUsername+"-deactivated")
		assert.Equal(t, userSignup.Namespace, notification.Namespace)
		assert.Equal(t, "userdeactivated", notification.Spec.Template)
		assert.Equal(t, userSignup.Name, notification.Spec.UserID)

		err = s.hostAwait.WaitUntilNotificationDeleted(userSignup.Status.CompliantUsername, "-deactivated")
		require.NoError(t, err)

		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)
		require.True(t, userSignup.Spec.Deactivated, "usersignup should be deactivated")

		t.Run("verify metrics are correct after deactivation", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 0)            // one less because of deactivated user
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 1) // one more because of deactivated user
		})

		s.T().Run("reactivate a deactivated user", func(t *testing.T) {
			err := s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{
				Namespace: userSignup.Namespace,
				Name:      userSignup.Name,
			}, userSignup)
			require.NoError(s.T(), err)

			userSignup, err := s.hostAwait.UpdateUserSignupSpec(userSignup.Name, func(us *v1alpha1.UserSignup) {
				us.Spec.Deactivated = false
			})
			require.NoError(s.T(), err)
			s.T().Logf("user signup '%s' reactivated", userSignup.Name)

			_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
			require.NoError(s.T(), err)

			userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			require.False(t, userSignup.Spec.Deactivated, "usersignup should not be deactivated")

			t.Run("verify metrics are correct after reactivating the user", func(t *testing.T) {
				metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)            // no change
				metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)    // one more because of reactivated user
				metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 1)            // one more because of reactivated user
				metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 1) // no change
			})
		})
	})

	s.T().Run("tests for tiers with automatic deactivation disabled", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait)

		userSignup, mur := s.createAndCheckUserSignup(true, "usernodeactivate", "usernodeactivate@redhat.com", true, ApprovedByAdmin()...)

		// Get the basic tier that has deactivation disabled
		basicDeactivationDisabledTier, err := s.hostAwait.WaitForNSTemplateTier("basicdeactivationdisabled")
		require.NoError(t, err)

		// Move the user to the new tier without deactivation enabled
		murSyncIndex := MoveUserToTier(t, s.hostAwait, userSignup.Spec.Username, *basicDeactivationDisabledTier).Spec.UserAccounts[0].SyncIndex
		mur, err = s.hostAwait.WaitForMasterUserRecord(mur.Name,
			wait.UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
			wait.UntilMasterUserRecordHasNotSyncIndex(murSyncIndex))
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after moving user to new tier", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)            // 1 new signup
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)    // 1 more approved signup
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 1)            // 1 mur for the approved signup
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0) // signup not deactivated
		})

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		manyManyDaysAgo := 999999999999999
		durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
		updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
		mur, err = s.hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = updatedProvisionedTime
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", mur.Name)

		// Ensure the MUR has the updated ProvisionedTime
		_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name, wait.UntilMasterUserRecordHasProvisionedTime(updatedProvisionedTime))
		require.NoError(s.T(), err)

		// The user should not be deactivated so the MUR should not be deleted, expect an error
		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
		require.Error(s.T(), err)

		t.Run("verify metrics are correct after provisioned time changed", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 1)                // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1)        // no change
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 1)                // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 0)     // no change
			metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 0) // no change
		})
	})

	s.T().Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
		// Initialize metrics assertion counts
		metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait)

		userSignup, mur := s.createAndCheckUserSignup(true, "usertoautodeactivate", "usertoautodeactivate@redhat.com", true, ApprovedByAdmin()...)
		deactivationExcludedUserSignup, excludedMur := s.createAndCheckUserSignup(true, "userdeactivationexcluded", "userdeactivationexcluded@excluded.com", true, ApprovedByAdmin()...)

		// Get the basic tier that has deactivation enabled
		basicTier, err := s.hostAwait.WaitForNSTemplateTier("basic")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(basicTier.Spec.DeactivationTimeoutDays) * time.Hour * 24
		mur, err = s.hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", mur.Name)

		// Use the same method above to change the provisioned time for the excluded user
		excludedMur, err = s.hostAwait.UpdateMasterUserRecordStatus(excludedMur.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", excludedMur.Name)

		// The non-excluded user should be deactivated
		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
		require.NoError(s.T(), err)
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)
		require.True(t, userSignup.Spec.Deactivated, "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = s.hostAwait.WaitForMasterUserRecord(excludedMur.Name)
		require.NoError(s.T(), err)
		deactivationExcludedUserSignup, err = s.hostAwait.WaitForUserSignup(deactivationExcludedUserSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)
		require.False(t, deactivationExcludedUserSignup.Spec.Deactivated, "deactivationExcludedUserSignup should not be deactivated")

		t.Run("verify metrics are correct after auto deactivation", func(t *testing.T) {
			// Only the user with domain not on the exclusion list should be auto-deactivated
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 2)
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsDeactivatedMetric, 1)
			metricsAssertion.WaitForMetricDelta(UserSignupsAutoDeactivatedMetric, 1)
		})
	})
}

func (s *userManagementTestSuite) TestUserBanning() {
	// Get metrics assertion helper for testing metrics
	metricsAssertion := InitMetricsAssertion(s.T(), s.hostAwait)

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {

		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		// Create a new UserSignup and confirm it was approved automatically
		userSignup, _ := s.createAndCheckUserSignup(false, "banprovisioned", "banprovisioned@test.com", true, ApprovedAutomatically()...)

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
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 0)
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 1)
		})
	})

	s.T().Run("create usersignup with preexisting banneduser", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// Check that no MUR created
		userSignup := s.createAndCheckUserSignupNoMUR(false, "testuser"+id, email, true, Banned()...)
		assert.Equal(t, v1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		mur, err := s.hostAwait.GetMasterUserRecord(wait.WithMurName("testuser" + id))
		require.NoError(s.T(), err)
		assert.Nil(s.T(), mur)
		require.NoError(s.T(), err)

		t.Run("verify metrics are correct after user signup", func(t *testing.T) {
			metricsAssertion.WaitForMetricDelta(UserSignupsMetric, 2)
			metricsAssertion.WaitForMetricDelta(UserSignupsApprovedMetric, 1) // not provisioned because banned before signup
			metricsAssertion.WaitForMetricDelta(CurrentMURsMetric, 0)
			metricsAssertion.WaitForMetricDelta(UserSignupsBannedMetric, 2)
		})
	})

	s.T().Run("register new user with preexisting ban", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// Get valid generated token for e2e tests. IAT claim is overriden
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
		assert.Equal(s.T(), http.StatusInternalServerError, resp.StatusCode)

		// Check the error.
		statusErr := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &statusErr)
		require.NoError(s.T(), err)
		require.Equal(s.T(), "user has been banned", statusErr["message"])
	})

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		// Create a new UserSignup and confirm it was approved automatically
		userSignup, mur := s.createAndCheckUserSignup(false, "banandunban", "banandunban@test.com", true, ApprovedAutomatically()...)

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
	userSignup := CreateAndApproveSignup(s.T(), s.hostAwait, "janedoe")

	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, userSignup, "basic")

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

		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, userSignup, "basic")
	})
}
