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
	suite.Suite
	wait.Awaitilities
}

func (s *userManagementTestSuite) SetupSuite() {
	s.Awaitilities = WaitForDeployments(s.T())
}

func (s *userManagementTestSuite) TestUserDeactivation() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()
	hostAwait.UpdateToolchainConfig(
		testconfig.AutomaticApproval().Enabled(false),
		testconfig.Deactivation().DeactivatingNotificationDays(-1))

	config := hostAwait.GetToolchainConfig()
	require.Equal(s.T(), -1, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

	s.T().Run("verify user deactivation on each member cluster", func(t *testing.T) {

		// User on member cluster 1
		userSignupMember1, _ := NewSignupRequest(t, s.Awaitilities).
			Username("usertodeactivate").
			Email("usertodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// User on member cluster 2
		userSignupMember2, _ := NewSignupRequest(t, s.Awaitilities).
			Username("usertodeactivate2").
			Email("usertodeactivate2@example.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait2).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		DeactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember1)
		DeactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember2)

		t.Run("reactivate a deactivated user", func(t *testing.T) {
			ReactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember1)
			ReactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember2)
		})
	})

	s.T().Run("verify notification fails on user deactivation with no usersignup email", func(t *testing.T) {

		// User on member cluster 1
		userNoEmail, _ := NewSignupRequest(t, s.Awaitilities).
			Username("usernoemail").
			Email("usernoemail@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Delete the user's email and set them to deactivated
		userSignup, err := hostAwait.UpdateUserSignup(userNoEmail.Name, func(us *toolchainv1alpha1.UserSignup) {
			delete(us.Annotations, toolchainv1alpha1.UserSignupUserEmailAnnotationKey)
			states.SetDeactivated(us, true)
		})
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

		_, err = hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(ApprovedByAdmin(), UserSignupMissingEmailAnnotation())...))
		require.NoError(t, err)
	})

	s.T().Run("tests for tiers with automatic deactivation disabled", func(t *testing.T) {

		userSignupMember1, murMember1 := NewSignupRequest(t, s.Awaitilities).
			Username("usernodeactivate").
			Email("usernodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Get the base tier that has deactivation disabled
		baseDeactivationDisabledTier, err := hostAwait.WaitForNSTemplateTier("basedeactivationdisabled")
		require.NoError(t, err)

		// Move the user to the new tier without deactivation enabled
		murSyncIndex := MoveUserToTier(t, hostAwait, userSignupMember1.Spec.Username, baseDeactivationDisabledTier.Name).Spec.UserAccounts[0].SyncIndex
		murMember1, err = hostAwait.WaitForMasterUserRecord(murMember1.Name,
			wait.UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
			wait.UntilMasterUserRecordHasNotSyncIndex(murSyncIndex))
		require.NoError(s.T(), err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		manyManyDaysAgo := 999999999999999
		durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
		updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
		murMember1, err = hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = updatedProvisionedTime
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", murMember1.Name)

		// Ensure the MUR has the updated ProvisionedTime
		_, err = hostAwait.WaitForMasterUserRecord(murMember1.Name, wait.UntilMasterUserRecordHasProvisionedTime(updatedProvisionedTime))
		require.NoError(s.T(), err)

		// The user should not be deactivated so the MUR should not be deleted, expect an error
		err = hostAwait.WaitUntilMasterUserRecordDeleted(murMember1.Name)
		require.Error(s.T(), err)
	})

	s.T().Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
		userSignupMember1, murMember1 := NewSignupRequest(t, s.Awaitilities).
			Username("usertoautodeactivate").
			Email("usertoautodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		deactivationExcludedUserSignupMember1, excludedMurMember1 := NewSignupRequest(t, s.Awaitilities).
			Username("userdeactivationexcluded").
			Email("userdeactivationexcluded@excluded.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Get the provisioned account's tier
		baseTier, err := hostAwait.WaitForNSTemplateTier("base")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name, murMember1.Status.ProvisionedTime.String())

		// Use the same method above to change the provisioned time for the excluded user
		excludedMurMember1, err = hostAwait.UpdateMasterUserRecordStatus(excludedMurMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", excludedMurMember1.Name, excludedMurMember1.Status.ProvisionedTime.String())

		// The non-excluded user should be deactivated
		err = hostAwait.WaitUntilMasterUserRecordDeleted(murMember1.Name)
		require.NoError(s.T(), err)
		userSignupMember1, err = hostAwait.WaitForUserSignup(userSignupMember1.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(ApprovedByAdmin(), Deactivated())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)
		require.True(t, states.Deactivated(userSignupMember1), "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = hostAwait.WaitForMasterUserRecord(excludedMurMember1.Name)
		require.NoError(s.T(), err)
		deactivationExcludedUserSignupMember1, err = hostAwait.WaitForUserSignup(deactivationExcludedUserSignupMember1.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)
		require.False(t, states.Deactivated(deactivationExcludedUserSignupMember1), "deactivationExcludedUserSignup should not be deactivated")
	})

	s.T().Run("test deactivating state set OK", func(t *testing.T) {
		// Reset configuration back to 3 days
		hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().Enabled(false),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		config := hostAwait.GetToolchainConfig()
		require.Equal(s.T(), 3, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

		userSignupMember1, murMember1 := NewSignupRequest(t, s.Awaitilities).
			Username("usertostartdeactivating").
			Email("usertostartdeactivating@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Get the provisioned account's tier
		baseTier, err := hostAwait.WaitForNSTemplateTier("base")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
		// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
		// period from the current time and setting this as the provisioned time should test the behaviour of the
		// deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = hostAwait.UpdateMasterUserRecordStatus(murMember1.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
		})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name,
			murMember1.Status.ProvisionedTime.String())

		// The user should be set to deactivating, but not deactivated
		_, err = hostAwait.WaitForUserSignup(userSignupMember1.Name, wait.UntilUserSignupHasConditions(
			ConditionSet(Default(), ApprovedByAdmin(), Deactivating())...))
		require.NoError(s.T(), err)

		// Verify resources have been provisioned
		VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignupMember1, "base")
	})

	s.T().Run("test full automatic user deactivation lifecycle", func(t *testing.T) {
		// Set configuration to 3 days
		hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().Enabled(true),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		hostConfig := hostAwait.GetToolchainConfig().Spec.Host
		require.Equal(s.T(), 3, *hostConfig.Deactivation.DeactivatingNotificationDays)

		// Create a new UserSignup
		userSignup, _ := NewSignupRequest(t, s.Awaitilities).
			Username("fulldeactivationlifecycle").
			Email("fulldeactivationlifecycle@redhat.com").
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute().Resources()

		// Wait for the UserSignup to have the desired state
		userSignup, err := hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)

		s.T().Run("user set to deactivating when provisioned time set in past", func(t *testing.T) {
			// Get the provisioned account's tier
			baseTier, err := hostAwait.WaitForNSTemplateTier("base")
			require.NoError(t, err)

			mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
			require.NoError(t, err)

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
			// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
			// period from the current time and setting this as the provisioned time should test the behaviour of the
			// deactivation controller reconciliation.
			tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
			mur, err = hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
			require.NoError(s.T(), err)
			s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
				mur.Status.ProvisionedTime.String())

			// The user should be set to deactivating, but not deactivated
			userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
				ConditionSet(Default(), ApprovedAutomatically(), Deactivating())...))
			require.NoError(s.T(), err)

			// Verify resources have been provisioned
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "base")

			t.Run("user set to deactivated after deactivating", func(t *testing.T) {
				// Set the provisioned time even further back
				tierDeactivationDuration := time.Duration(baseTier.Spec.DeactivationTimeoutDays+4) * time.Hour * 24
				mur, err = hostAwait.UpdateMasterUserRecordStatus(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
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
				require.NoError(t, hostAwait.Client.Status().Update(context.TODO(), userSignup))

				// Trigger a reconciliation of the deactivation controller by updating the MUR
				// - The SyncIndex property of the UserAccount is intended for the express purpose of triggering
				//   a reconciliation, so we set it to some new unique value here
				syncIndex := uuid.Must(uuid.NewV4()).String()
				mur, err := hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Spec.UserAccounts[0].SyncIndex = syncIndex
				})
				require.NoError(t, err)

				// The user should now be set to deactivated
				userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name,
					wait.UntilUserSignupHasConditions(ConditionSet(ApprovedAutomatically(), Deactivated())...))
				require.NoError(s.T(), err)

				// The MUR should also be deleted
				err = hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
				require.NoError(s.T(), err)
			})
		})
	})
}

func (s *userManagementTestSuite) TestUserBanning() {

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup and approve it manually
		userSignup, _ := NewSignupRequest(t, s.Awaitilities).
			Username("banprovisioned").
			Email("banprovisioned@test.com").
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Create the BannedUser
		CreateBannedUser(s.T(), s.Host(), userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
		// confirm usersignup
		_, err = hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(s.T(), err)
	})

	s.T().Run("manually created usersignup with preexisting banneduser", func(t *testing.T) {
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(true))

		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		CreateBannedUser(s.T(), s.Host(), email)

		// For this test, we don't want to create the UserSignup via the registration service (the next test does this)
		// Instead, we want to confirm the behaviour when a UserSignup with a banned email address is created manually
		userSignup := NewUserSignup(t, hostAwait, "testuser"+id, email)
		userSignup.Spec.TargetCluster = memberAwait.ClusterName

		// Create the UserSignup via the Kubernetes API
		err := hostAwait.CreateWithCleanup(context.TODO(), userSignup)
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' created", userSignup.Name)

		// Check the UserSignup is created
		userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name)
		require.NoError(s.T(), err)

		// Confirm that the user is banned
		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		err = hostAwait.WaitUntilMasterUserRecordDeleted("testuser" + id)
		require.NoError(s.T(), err)

	})

	s.T().Run("register new user with preexisting ban", func(t *testing.T) {
		hostAwait := s.Host()
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(true))

		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		CreateBannedUser(s.T(), s.Host(), email)

		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity0 := authsupport.NewIdentity()
		emailClaim0 := authsupport.WithEmailClaim(email)
		token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
		require.NoError(s.T(), err)

		route := hostAwait.RegistrationServiceURL

		// Call signup endpoint with a valid token to initiate a signup process
		req, err := http.NewRequestWithContext(context.TODO(), "POST", route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token0)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
		require.NoError(s.T(), err)
		defer Close(s.T(), resp)

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
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup
		userSignup, mur := NewSignupRequest(t, s.Awaitilities).
			Username("banandunban").
			Email("banandunban@test.com").
			EnsureMUR().
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		// Create the BannedUser
		bannedUser := CreateBannedUser(s.T(), s.Host(), userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
		// confirm usersignup
		userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(s.T(), err)

		t.Run("unban the banned user", func(t *testing.T) {
			// Unban the user
			err = hostAwait.Client.Delete(context.TODO(), bannedUser)
			require.NoError(s.T(), err)

			// Confirm the BannedUser is deleted
			err = hostAwait.WaitUntilBannedUserDeleted(bannedUser.Name)
			require.NoError(s.T(), err)

			// Confirm the user is provisioned
			userSignup, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)

			// Confirm the MUR is created
			_, err = hostAwait.WaitForMasterUserRecord(mur.Name)
			require.NoError(s.T(), err)
		})
	})
}

func (s *userManagementTestSuite) TestUserDisabled() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))

	// Create UserSignup
	userSignup, mur := NewSignupRequest(s.T(), s.Awaitilities).
		Username("janedoe").
		EnsureMUR().
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "base")

	// Disable MUR
	mur, err := hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
		mur.Spec.Disabled = true
	})
	require.NoError(s.T(), err)

	// Wait until the UserAccount status is disabled
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(Disabled()))
	require.NoError(s.T(), err)

	// Wait until the MUR status is disabled
	mur, err = hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username,
		wait.UntilMasterUserRecordHasConditions(Disabled(), ProvisionedNotificationCRCreated()))
	require.NoError(s.T(), err)

	// Check that the UserAccount is now set to disabled
	require.True(s.T(), userAccount.Spec.Disabled)

	// Check the User is deleted
	user := &userv1.User{}
	err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: userAccount.Namespace}, user)
	require.Error(s.T(), err)
	assert.True(s.T(), apierrors.IsNotFound(err))

	// Check the Identity is deleted
	identity := &userv1.Identity{}
	err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: ToIdentityName(userAccount.Spec.UserID)}, identity)
	require.Error(s.T(), err)
	assert.True(s.T(), apierrors.IsNotFound(err))

	s.Run("re-enabled mur", func() {
		// Re-enable MUR
		mur, err = hostAwait.UpdateMasterUserRecordSpec(mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = false
		})
		require.NoError(s.T(), err)

		VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "base")
	})
}

func (s *userManagementTestSuite) TestReturningUsersProvisionedToLastCluster() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()

	s.T().Run("test returning user provisioned to same cluster", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled(false))
		clustersToTest := []*wait.MemberAwaitility{memberAwait, memberAwait2}

		for i, initialTargetCluster := range clustersToTest {
			// when
			t.Run(fmt.Sprintf("cluster %s: user activated->deactivated->reactivated", initialTargetCluster.ClusterName), func(t *testing.T) {
				// given
				userSignup, _ := NewSignupRequest(t, s.Awaitilities).
					Username(fmt.Sprintf("returninguser%d", i)).
					Email(fmt.Sprintf("returninguser%d@redhat.com", i)).
					EnsureMUR().
					ManuallyApprove().
					TargetCluster(initialTargetCluster). // use TargetCluster initially to force user to provision to the expected cluster
					RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
					Execute().Resources()

				// when
				DeactivateAndCheckUser(s.T(), s.Awaitilities, userSignup)
				// If TargetCluster is set it will override the last cluster annotation so remove TargetCluster
				userSignup, err := s.Host().UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
					us.Spec.TargetCluster = ""
				})
				require.NoError(t, err)

				userSignup = ReactivateAndCheckUser(s.T(), s.Awaitilities, userSignup)
				mur2, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))

				// then
				require.NoError(t, err)
				secondSignupCluster := GetMurTargetMember(t, s.Awaitilities, mur2)
				require.Equal(t, initialTargetCluster.ClusterName, secondSignupCluster.ClusterName)
			})
		}
	})
}
