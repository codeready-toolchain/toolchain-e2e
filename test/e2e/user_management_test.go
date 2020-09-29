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
	userSignup, mur := s.createAndCheckUserSignup(true, "iris", "iris@redhat.com", true,  ApprovedByAdmin()...)
	deactivationExcludedUserSignup, excludedMur := s.createAndCheckUserSignup(true, "pupil", "pupil@excluded.com", true, ApprovedByAdmin()...)

	s.T().Run("deactivate a user", func(t *testing.T) {
		userSignup.Spec.Deactivated = true
		err := s.hostAwait.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
		require.NoError(s.T(), err)

		notification, err := s.hostAwait.WaitForNotification(userSignup.Status.CompliantUsername+"-deactivated", wait.UntilNotificationHasConditions(Sent()))
		require.NoError(t, err)
		require.NotNil(t, notification)
		assert.Equal(t, userSignup.Status.CompliantUsername+"-deactivated", notification.Name)
		assert.Equal(t, userSignup.Namespace, notification.Namespace)
		assert.Equal(t, "userdeactivated", notification.Spec.Template)
		assert.Equal(t, userSignup.Name, notification.Spec.UserID)

		err = s.hostAwait.WaitUntilNotificationDeleted(userSignup.Status.CompliantUsername + "-deactivated")
		require.NoError(t, err)

		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...))
		require.NoError(s.T(), err)
		require.True(t, userSignup.Spec.Deactivated, "usersignup should be deactivated")
	})

	s.T().Run("reactivate a deactivated user", func(t *testing.T) {
		err := s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: userSignup.Namespace,
			Name:      userSignup.Name,
		}, userSignup)
		require.NoError(s.T(), err)

		userSignup.Spec.Deactivated = false
		err = s.hostAwait.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' reactivated", userSignup.Name)

		_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
		require.NoError(s.T(), err)

		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedByAdmin()...))
		require.NoError(s.T(), err)
		require.False(t, userSignup.Spec.Deactivated, "usersignup should not be deactivated")
	})

	s.T().Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
		tierDeactivationPeriod := 30
		ctx, hostAwait, _ := WaitForDeployments(t, &v1alpha1.NSTemplateTier{})
		defer ctx.Cleanup()
		// Let's create a tier with deactivation enabled
		deactivationTier := CreateNSTemplateTier(t, ctx, hostAwait, "deactivation-tier", DeactivationTimeoutDays(tierDeactivationPeriod))

		// Move 2 users to the new tier with deactivation enabled - 1 with a domain that matches the deactivation exclusion list and 1 that does not
		excludedMur = MoveUserToTier(t, hostAwait, deactivationExcludedUserSignup.Spec.Username, *deactivationTier)
		mur = MoveUserToTier(t, hostAwait, userSignup.Spec.Username, *deactivationTier)

		// We cannot wait days for deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time to a time far enough
		// in the past to trigger auto deactivation. Subtracting the tier deactivation period from the current time and setting this as the provisioned
		// time should cause the deactivation controller to reconcile and see the mur is ready for deactivation.
		tierDeactivationDuration := time.Duration(tierDeactivationPeriod*24) * time.Hour
		mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		err := s.hostAwait.Client.Status().Update(context.TODO(), mur)
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", mur.Name)

		// Use the same method above to change the provisioned time for the excluded user
		excludedMur, err = s.hostAwait.GetMasterUserRecord(wait.WithMurName(excludedMur.Name))
		require.NoError(s.T(), err)
		excludedMur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		err = s.hostAwait.Client.Status().Update(context.TODO(), excludedMur)
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", excludedMur.Name)

		// The non-excluded user should be deactivated
		err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
		require.NoError(s.T(), err)
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...))
		require.NoError(s.T(), err)
		require.True(t, userSignup.Spec.Deactivated, "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = s.hostAwait.WaitForMasterUserRecord(excludedMur.Name)
		require.NoError(s.T(), err)
		deactivationExcludedUserSignup, err = s.hostAwait.WaitForUserSignup(deactivationExcludedUserSignup.Name,
			wait.UntilUserSignupHasConditions(ApprovedByAdmin()...))
		require.NoError(s.T(), err)
		require.False(t, deactivationExcludedUserSignup.Spec.Deactivated, "deactivationExcludedUserSignup should not be deactivated")
	})
}

func (s *userManagementTestSuite) TestUserBanning() {
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
	})

	s.T().Run("create usersignup with preexisting banneduser", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// Check that no MUR created
		_ = s.createAndCheckUserSignupNoMUR(false, "testuser"+id, email, true, Banned()...)
		mur, err := s.hostAwait.GetMasterUserRecord(wait.WithMurName("testuser" + id))
		require.NoError(s.T(), err)
		assert.Nil(s.T(), mur)
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

		t.Run("unban the banned user", func(t *testing.T) {
			// Unban the user
			err = s.hostAwait.Client.Delete(context.TODO(), bannedUser)
			require.NoError(s.T(), err)

			// Confirm the BannedUser is deleted
			err = s.hostAwait.WaitUntilBannedUserDeleted(bannedUser.Name)
			require.NoError(s.T(), err)

			// Confirm the user is provisioned
			_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedAutomatically()...))
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
	err = s.hostAwait.UpdateMasterUserRecord(mur.Name, func(mur *v1alpha1.MasterUserRecord) {
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
		err = s.hostAwait.UpdateMasterUserRecord(mur.Name, func(mur *v1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = false
		})
		require.NoError(s.T(), err)

		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, userSignup, "basic")
	})
}
