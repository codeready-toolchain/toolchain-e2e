package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gofrs/uuid"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var httpClient = HTTPClient

func TestUserManagement(t *testing.T) {
	suite.Run(t, &userManagementTestSuite{})
}

type userManagementTestSuite struct {
	suite.Suite
	wait.Awaitilities
}

type userTierTestData struct {
	name                    string
	deactivationTimeoutDays int
}

func (s *userManagementTestSuite) SetupSuite() {
	s.Awaitilities = WaitForDeployments(s.T())
}

// TestVerifyUserTiers lists all UserTiers and validates each one
// Functional testing is covered by the deactivation tests below, it's not necessary to test each
// UserTier with deactivation since it's only the deactivationTimeoutDays value that changes and the deactivation
// logic handles them in the same way
func (s *userManagementTestSuite) TestVerifyUserTiers() {
	hostAwait := s.Host()

	userTiers := &toolchainv1alpha1.UserTierList{}
	err := hostAwait.Client.List(context.TODO(), userTiers, client.InNamespace(hostAwait.Namespace))
	require.NoError(s.T(), err)
	require.Len(s.T(), userTiers.Items, 6)

	expectedTiers := []userTierTestData{
		{
			name:                    "nodeactivation",
			deactivationTimeoutDays: 0,
		},
		{
			name:                    "deactivate30",
			deactivationTimeoutDays: 30,
		},
		{
			name:                    "deactivate80",
			deactivationTimeoutDays: 80,
		},
		{
			name:                    "deactivate90",
			deactivationTimeoutDays: 90,
		},
		{
			name:                    "deactivate180",
			deactivationTimeoutDays: 180,
		},
		{
			name:                    "deactivate365",
			deactivationTimeoutDays: 365,
		},
	}
	for _, expectedTier := range expectedTiers {
		s.T().Run(fmt.Sprintf("verify UserTier '%s'", expectedTier.name), func(t *testing.T) {
			userTier, err := hostAwait.WaitForUserTier(t, expectedTier.name)
			assert.NoError(t, err)
			assert.Equal(t, expectedTier.deactivationTimeoutDays, userTier.Spec.DeactivationTimeoutDays)
		})
	}
}

func (s *userManagementTestSuite) TestUserDeactivation() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()
	hostAwait.UpdateToolchainConfig(s.T(),
		testconfig.AutomaticApproval().Enabled(false),
		testconfig.Deactivation().DeactivatingNotificationDays(-1))

	config := hostAwait.GetToolchainConfig(s.T())
	require.Equal(s.T(), -1, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

	s.T().Run("verify user deactivation on each member cluster", func(t *testing.T) {

		// User on member cluster 1
		userSignupMember1, _ := NewSignupRequest(s.Awaitilities).
			Username("usertodeactivate").
			Email("usertodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// User on member cluster 2
		userSignupMember2, _ := NewSignupRequest(s.Awaitilities).
			Username("usertodeactivate2").
			Email("usertodeactivate2@example.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait2).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		DeactivateAndCheckUser(t, s.Awaitilities, userSignupMember1)
		DeactivateAndCheckUser(t, s.Awaitilities, userSignupMember2)

		t.Run("reactivate a deactivated user", func(t *testing.T) {
			ReactivateAndCheckUser(t, s.Awaitilities, userSignupMember1)
			ReactivateAndCheckUser(t, s.Awaitilities, userSignupMember2)
		})
	})

	s.T().Run("verify notification fails on user deactivation with no usersignup email", func(t *testing.T) {

		// User on member cluster 1
		userNoEmail, _ := NewSignupRequest(s.Awaitilities).
			Username("usernoemail").
			Email("usernoemail@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// Delete the user's email and set them to deactivated
		userSignup, err := hostAwait.UpdateUserSignup(t, userNoEmail.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				delete(us.Annotations, toolchainv1alpha1.UserSignupUserEmailAnnotationKey)
				states.SetDeactivated(us, true)
			})
		require.NoError(t, err)
		t.Logf("user signup '%s' set to deactivated", userSignup.Name)

		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(ApprovedByAdmin(), UserSignupMissingEmailAnnotation())...))
		require.NoError(t, err)
	})

	s.T().Run("tests for tiers with automatic deactivation disabled", func(t *testing.T) {

		_, murMember1 := NewSignupRequest(s.Awaitilities).
			Username("usernodeactivate").
			Email("usernodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// Get the tier that has deactivation disabled
		deactivationDisabledTier, err := hostAwait.WaitForUserTier(t, "nodeactivation")
		require.NoError(t, err)

		// Move the user to the new tier without deactivation enabled
		tiers.MoveMURToTier(t, hostAwait, murMember1.Name, deactivationDisabledTier.Name)
		murMember1, err = hostAwait.WaitForMasterUserRecord(t, murMember1.Name,
			wait.UntilMasterUserRecordHasCondition(Provisioned()),
			wait.UntilMasterUserRecordHasTierName(deactivationDisabledTier.Name)) // ignore other conditions, such as notification sent, etc.
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		manyManyDaysAgo := 999999999999999
		durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
		updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
		murMember1, err = hostAwait.UpdateMasterUserRecordStatus(t, murMember1.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = updatedProvisionedTime
			})
		require.NoError(t, err)
		t.Logf("masteruserrecord '%s' provisioned time adjusted", murMember1.Name)

		// The user should not be deactivated so the MUR should not be deleted, expect an error
		err = hostAwait.WithRetryOptions(wait.TimeoutOption(3*time.Second)).WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, murMember1.Name)
		require.Error(t, err)

		// The space should not be deleted either, expect an error
		err = hostAwait.WithRetryOptions(wait.TimeoutOption(3*time.Second)).WaitUntilSpaceAndSpaceBindingsDeleted(t, murMember1.Name)
		require.Error(t, err)
	})

	s.T().Run("tests for tiers with automatic deactivation enabled", func(t *testing.T) {
		userSignupMember1, murMember1 := NewSignupRequest(s.Awaitilities).
			Username("usertoautodeactivate").
			Email("usertoautodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(hostAwait.Client, murMember1)

		deactivationExcludedUserSignupMember1, excludedMurMember1 := NewSignupRequest(s.Awaitilities).
			Username("userdeactivationexcluded").
			Email("userdeactivationexcluded@excluded.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(hostAwait.Client, excludedMurMember1)

		// Get the provisioned account's tier
		baseUserTier, err := hostAwait.WaitForUserTier(t, "deactivate30")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = hostAwait.UpdateMasterUserRecordStatus(t, murMember1.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
		require.NoError(t, err)
		t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name, murMember1.Status.ProvisionedTime.String())

		// Use the same method above to change the provisioned time for the excluded user
		excludedMurMember1, err = hostAwait.UpdateMasterUserRecordStatus(t, excludedMurMember1.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
		require.NoError(t, err)
		t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", excludedMurMember1.Name, excludedMurMember1.Status.ProvisionedTime.String())

		// The non-excluded user should be deactivated
		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, murMember1.Name)
		require.NoError(t, err)

		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, murMember1.Name)
		require.NoError(t, err)

		userSignupMember1, err = hostAwait.WaitForUserSignup(t, userSignupMember1.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(ApprovedByAdmin(), Deactivated())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)
		require.True(t, states.Deactivated(userSignupMember1), "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = hostAwait.WaitForMasterUserRecord(t, excludedMurMember1.Name)
		require.NoError(t, err)
		deactivationExcludedUserSignupMember1, err = hostAwait.WaitForUserSignup(t, deactivationExcludedUserSignupMember1.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(t, err)
		require.False(t, states.Deactivated(deactivationExcludedUserSignupMember1), "deactivationExcludedUserSignup should not be deactivated")
	})

	s.T().Run("test deactivating state set OK", func(t *testing.T) {
		// Reset configuration back to 3 days
		hostAwait.UpdateToolchainConfig(t,
			testconfig.AutomaticApproval().Enabled(false),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		config := hostAwait.GetToolchainConfig(t)
		require.Equal(t, 3, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

		userSignupMember1, murMember1 := NewSignupRequest(s.Awaitilities).
			Username("usertostartdeactivating").
			Email("usertostartdeactivating@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(hostAwait.Client, murMember1)

		// Get the provisioned account's tier
		baseUserTier, err := hostAwait.WaitForUserTier(t, "deactivate30")
		require.NoError(t, err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
		// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
		// period from the current time and setting this as the provisioned time should test the behaviour of the
		// deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = hostAwait.UpdateMasterUserRecordStatus(t, murMember1.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
			})
		require.NoError(t, err)
		t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name,
			murMember1.Status.ProvisionedTime.String())

		// The user should be set to deactivating, but not deactivated
		_, err = hostAwait.WaitForUserSignup(t, userSignupMember1.Name, wait.UntilUserSignupHasConditions(
			ConditionSet(Default(), ApprovedByAdmin(), Deactivating())...))
		require.NoError(t, err)

		// Verify resources still exist
		VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignupMember1, "deactivate30", "base")
	})

	s.T().Run("test full automatic user deactivation lifecycle", func(t *testing.T) {
		// Set configuration to 3 days
		hostAwait.UpdateToolchainConfig(t,
			testconfig.AutomaticApproval().Enabled(true),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		hostConfig := hostAwait.GetToolchainConfig(t).Spec.Host
		require.Equal(t, 3, *hostConfig.Deactivation.DeactivatingNotificationDays)

		// Create a new UserSignup
		userSignup, mur := NewSignupRequest(s.Awaitilities).
			Username("fulldeactivationlifecycle").
			Email("fulldeactivationlifecycle@redhat.com").
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute(t).Resources()

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(hostAwait.Client, mur)

		// Wait for the UserSignup to have the desired state
		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(t, err)

		s.T().Run("user set to deactivating when provisioned time set in past", func(t *testing.T) {
			// Get the provisioned account's tier
			baseUserTier, err := hostAwait.WaitForUserTier(t, "deactivate30")
			require.NoError(t, err)

			mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
				wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
			require.NoError(t, err)

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
			// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
			// period from the current time and setting this as the provisioned time should test the behaviour of the
			// deactivation controller reconciliation.
			tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
			mur, err = hostAwait.UpdateMasterUserRecordStatus(t, mur.Name,
				func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
				})
			require.NoError(t, err)
			t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
				mur.Status.ProvisionedTime.String())

			// The user should be set to deactivating, but not deactivated
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name, wait.UntilUserSignupHasConditions(
				ConditionSet(Default(), ApprovedAutomatically(), Deactivating())...))
			require.NoError(t, err)

			// Verify resources have been provisioned
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")

			t.Run("user set to deactivated after deactivating", func(t *testing.T) {
				// Set the provisioned time even further back
				tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+4) * time.Hour * 24
				mur, err = hostAwait.UpdateMasterUserRecordStatus(t, mur.Name,
					func(mur *toolchainv1alpha1.MasterUserRecord) {
						mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
					})
				murName := mur.Name
				require.NoError(t, err)
				t.Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
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

				// Trigger a reconciliation of the deactivation controller by updating the MUR annotation
				_, err := hostAwait.UpdateMasterUserRecordSpec(t, murName,
					func(mur *toolchainv1alpha1.MasterUserRecord) {
						if mur.Annotations == nil {
							mur.Annotations = map[string]string{}
						}
						mur.Annotations["update-from-e2e-tests"] = "trigger"
					})
				if err != nil {
					// the mur might already be deleted, so we can continue as long as the error is the mur was not found
					require.EqualError(t, err, `masteruserrecords.toolchain.dev.openshift.com "fulldeactivationlifecycle" not found`)
				}

				// The user should now be set to deactivated
				userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
					wait.UntilUserSignupHasConditions(ConditionSet(ApprovedAutomatically(), Deactivated())...))
				require.NoError(t, err)

				// The MUR should also be deleted
				err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, murName)
				require.NoError(t, err)

				err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, murName)
				require.NoError(t, err)
			})
		})
	})

	s.T().Run("reactivated but unverified user reverted back to deactivated after timeout", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t,
			testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup and wait for it to be provisioned
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("usertoreactivate").
			Email("usertoreactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// Wait for the UserSignup to have the desired state
		userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(t, err)

		// Now deactivate the user
		userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
		require.NoError(t, err)

		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), DeactivatedWithoutPreDeactivation())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)
		require.True(t, states.Deactivated(userSignup), "usersignup should be deactivated")

		// Set the unverified retention days to 0
		hostAwait.UpdateToolchainConfig(t,
			testconfig.Deactivation().UserSignupUnverifiedRetentionDays(0))

		// Reactivate the user
		userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivating(us, false)
				states.SetDeactivated(us, false)
				states.SetApprovedManually(us, true)
				states.SetVerificationRequired(us, true)
			})
		require.NoError(t, err)
		t.Logf("user signup '%s' reactivated", userSignup.Name)

		// Since the config for retention days is set to 0, the account should be deactivated again immediately
		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), DeactivatedWithoutNotification())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)
		require.True(t, states.Deactivated(userSignup), "usersignup should be deactivated")

		// Set the unverified retention days to 7
		hostAwait.UpdateToolchainConfig(t,
			testconfig.Deactivation().UserSignupUnverifiedRetentionDays(7))
	})
}

func (s *userManagementTestSuite) TestUserBanning() {

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup and approve it manually
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("banprovisioned").
			Email("banprovisioned@test.com").
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// Create the BannedUser
		CreateBannedUser(t, s.Host(), userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
		require.NoError(t, err)

		// Confirm that a MasterUserRecord is deleted
		_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForMasterUserRecord(t, userSignup.Spec.Username)
		require.Error(t, err)
		// confirm usersignup
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(t, err)
	})

	s.T().Run("manually created usersignup with preexisting banneduser", func(t *testing.T) {
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))

		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		CreateBannedUser(t, s.Host(), email)

		// For this test, we don't want to create the UserSignup via the registration service (the next test does this)
		// Instead, we want to confirm the behaviour when a UserSignup with a banned email address is created manually
		userSignup := NewUserSignup(hostAwait.Namespace, "testuser"+id, email)
		userSignup.Spec.TargetCluster = memberAwait.ClusterName

		// Create the UserSignup via the Kubernetes API
		err := hostAwait.CreateWithCleanup(t, userSignup)
		require.NoError(t, err)
		t.Logf("user signup '%s' created", userSignup.Name)

		// Check the UserSignup is created
		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name)
		require.NoError(t, err)

		// Confirm that the user is banned
		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueBanned, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, "testuser"+id)
		require.NoError(t, err)

		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, "testuser"+id)
		require.NoError(t, err)
	})

	s.T().Run("register new user with preexisting ban", func(t *testing.T) {
		hostAwait := s.Host()
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))

		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		CreateBannedUser(t, s.Host(), email)

		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		_, token0, err := authsupport.NewToken(authsupport.WithEmail(email))
		require.NoError(t, err)

		route := hostAwait.RegistrationServiceURL

		// Call signup endpoint with a valid token to initiate a signup process
		req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token0)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
		require.NoError(t, err)
		defer Close(t, resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		// Check the error.
		statusErr := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &statusErr)
		require.NoError(t, err)
		require.Equal(t, "forbidden: user has been banned", statusErr["message"])
	})

	s.T().Run("ban provisioned usersignup", func(t *testing.T) {
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup
		userSignup, mur := NewSignupRequest(s.Awaitilities).
			Username("banandunban").
			Email("banandunban@test.com").
			EnsureMUR().
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		// Create the BannedUser
		bannedUser := CreateBannedUser(t, s.Host(), userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...))
		require.NoError(t, err)

		// Confirm that a MasterUserRecord is deleted
		_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForMasterUserRecord(t, userSignup.Spec.Username)
		require.Error(t, err)
		// confirm usersignup
		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(t, err)

		t.Run("unban the banned user", func(t *testing.T) {
			// Unban the user
			err = hostAwait.Client.Delete(context.TODO(), bannedUser)
			require.NoError(t, err)

			// Confirm the BannedUser is deleted
			err = hostAwait.WaitUntilBannedUserDeleted(t, bannedUser.Name)
			require.NoError(t, err)

			// Confirm the user is provisioned
			userSignup, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)

			// Confirm the MUR is created
			_, err = hostAwait.WaitForMasterUserRecord(t, mur.Name)
			require.NoError(t, err)
		})
	})
}

func (s *userManagementTestSuite) TestUserDisabled() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false))

	// Create UserSignup
	userSignup, mur := NewSignupRequest(s.Awaitilities).
		Username("janedoe").
		EnsureMUR().
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")

	// Disable MUR
	mur, err := hostAwait.UpdateMasterUserRecordSpec(s.T(),
		mur.Name, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = true
		})
	require.NoError(s.T(), err)

	// Wait until the UserAccount status is disabled
	userAccount, err := memberAwait.WaitForUserAccount(s.T(), mur.Name,
		wait.UntilUserAccountHasConditions(Disabled()))
	require.NoError(s.T(), err)

	// Wait until the MUR status is disabled
	mur, err = hostAwait.WaitForMasterUserRecord(s.T(), userSignup.Spec.Username,
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
	err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: identitypkg.NewIdentityNamingStandard(userAccount.Spec.UserID, "rhd").IdentityName()}, identity)
	require.Error(s.T(), err)
	assert.True(s.T(), apierrors.IsNotFound(err))

	s.Run("re-enabled mur", func() {
		// Re-enable MUR
		mur, err = hostAwait.UpdateMasterUserRecordSpec(s.T(), mur.Name,
			func(mur *toolchainv1alpha1.MasterUserRecord) {
				mur.Spec.Disabled = false
			})
		require.NoError(s.T(), err)

		VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
	})
}

func (s *userManagementTestSuite) TestReturningUsersProvisionedToLastCluster() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()

	s.T().Run("test returning user provisioned to same cluster", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
		clustersToTest := []*wait.MemberAwaitility{memberAwait, memberAwait2}

		for i, initialTargetCluster := range clustersToTest {
			// when
			t.Run(fmt.Sprintf("cluster %s: user activated->deactivated->reactivated", initialTargetCluster.ClusterName), func(t *testing.T) {
				// given
				userSignup, _ := NewSignupRequest(s.Awaitilities).
					Username(fmt.Sprintf("returninguser%d", i)).
					Email(fmt.Sprintf("returninguser%d@redhat.com", i)).
					EnsureMUR().
					ManuallyApprove().
					TargetCluster(initialTargetCluster). // use TargetCluster initially to force user to provision to the expected cluster
					RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
					Execute(t).Resources()

				// when
				DeactivateAndCheckUser(t, s.Awaitilities, userSignup)
				// If TargetCluster is set it will override the last cluster annotation so remove TargetCluster
				userSignup, err := s.Host().UpdateUserSignup(t, userSignup.Name,
					func(us *toolchainv1alpha1.UserSignup) {
						us.Spec.TargetCluster = ""
					})
				require.NoError(t, err)

				userSignup = ReactivateAndCheckUser(t, s.Awaitilities, userSignup)
				mur2, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
					wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))

				// then
				require.NoError(t, err)
				secondSignupCluster := GetMurTargetMember(t, s.Awaitilities, mur2)
				require.Equal(t, initialTargetCluster.ClusterName, secondSignupCluster.ClusterName)
			})
		}
	})
}

// TODO remove once UserTier migration is completed
func (s *userManagementTestSuite) promoteToDefaultUserTier(cl client.Client, mur *toolchainv1alpha1.MasterUserRecord) {
	mur.Spec.TierName = "deactivate30"
	err := cl.Update(context.TODO(), mur)
	require.NoError(s.T(), err)
}
