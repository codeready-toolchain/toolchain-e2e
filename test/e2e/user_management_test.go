package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
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
	require.Len(s.T(), userTiers.Items, 7)

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
		{
			name:                    "intel",
			deactivationTimeoutDays: 60,
		},
	}
	for _, expectedTier := range expectedTiers {
		s.Run(fmt.Sprintf("verify UserTier '%s'", expectedTier.name), func() {
			userTier, err := hostAwait.WaitForUserTier(s.T(), expectedTier.name)
			require.NoError(s.T(), err)
			assert.Equal(s.T(), expectedTier.deactivationTimeoutDays, userTier.Spec.DeactivationTimeoutDays)
		})
	}
}

func (s *userManagementTestSuite) TestUserDeactivation() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()
	hostAwait.UpdateToolchainConfig(s.T(),
		testconfig.AutomaticApproval().Enabled(false),
		testconfig.Deactivation().DeactivatingNotificationDays(0))

	config := hostAwait.GetToolchainConfig(s.T())
	require.Equal(s.T(), 0, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

	s.Run("verify user deactivation on each member cluster", func() {
		// User on member cluster 1
		userMember1 := NewSignupRequest(s.Awaitilities).
			Username("usertodeactivate").
			Email("usertodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		userSignupMember1 := userMember1.UserSignup

		// User on member cluster 2
		userMember2 := NewSignupRequest(s.Awaitilities).
			Username("usertodeactivate2").
			Email("usertodeactivate2@example.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait2).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		userSignupMember2 := userMember2.UserSignup

		DeactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember1)
		DeactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember2)

		s.Run("reactivate a deactivated user", func() {
			ReactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember1)
			ReactivateAndCheckUser(s.T(), s.Awaitilities, userSignupMember2)
		})
	})

	s.Run("verify notification fails on user deactivation with no usersignup email", func() {
		// User on member cluster 1
		uNoEmail := NewSignupRequest(s.Awaitilities).
			Username("usernoemail").
			Email("usernoemail@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())

		// Delete the user's email and set them to deactivated
		userSignup, err := wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(uNoEmail.UserSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					us.Spec.IdentityClaims.Email = ""
					states.SetDeactivated(us, true)
				})
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

		_, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.ApprovedByAdmin(), wait.UserSignupMissingEmail())...))
		require.NoError(s.T(), err)
	})

	s.Run("tests for tiers with automatic deactivation disabled", func() {
		user := NewSignupRequest(s.Awaitilities).
			Username("usernodeactivate").
			Email("usernodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		murMember1 := user.MUR

		// Get the tier that has deactivation disabled
		deactivationDisabledTier, err := hostAwait.WaitForUserTier(s.T(), "nodeactivation")
		require.NoError(s.T(), err)

		// Move the user to the new tier without deactivation enabled
		tiers.MoveMURToTier(s.T(), hostAwait, murMember1.Name, deactivationDisabledTier.Name)
		murMember1, err = hostAwait.WaitForMasterUserRecord(s.T(), murMember1.Name,
			wait.UntilMasterUserRecordHasCondition(wait.Provisioned()),
			wait.UntilMasterUserRecordHasTierName(deactivationDisabledTier.Name)) // ignore other conditions, such as notification sent, etc.
		require.NoError(s.T(), err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		manyManyDaysAgo := 999999999999999
		durationDelta := time.Duration(manyManyDaysAgo) * time.Hour * 24
		updatedProvisionedTime := &metav1.Time{Time: time.Now().Add(-durationDelta)}
		murMember1, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
			UpdateStatus(murMember1.Name, hostAwait.Namespace,
				func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = updatedProvisionedTime
				})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted", murMember1.Name)

		// The user should not be deactivated so the MUR should not be deleted, expect an error
		err = hostAwait.WithRetryOptions(wait.TimeoutOption(3*time.Second)).WaitUntilMasterUserRecordAndSpaceBindingsDeleted(s.T(), murMember1.Name)
		require.Error(s.T(), err)

		// The space should not be deleted either, expect an error
		err = hostAwait.WithRetryOptions(wait.TimeoutOption(3*time.Second)).WaitUntilSpaceAndSpaceBindingsDeleted(s.T(), murMember1.Name)
		require.Error(s.T(), err)
	})

	s.Run("tests for tiers with automatic deactivation enabled", func() {
		userMember1 := NewSignupRequest(s.Awaitilities).
			Username("usertoautodeactivate").
			Email("usertoautodeactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		userSignupMember1 := userMember1.UserSignup
		murMember1 := userMember1.MUR

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(murMember1)

		deactivationExcludedUserMember1 := NewSignupRequest(s.Awaitilities).
			Username("userdeactivationexcluded").
			Email("userdeactivationexcluded@excluded.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		deactivationExcludedUserSignupMember1 := deactivationExcludedUserMember1.UserSignup
		excludedMurMember1 := deactivationExcludedUserMember1.MUR

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(excludedMurMember1)

		// Get the provisioned account's tier
		baseUserTier, err := hostAwait.WaitForUserTier(s.T(), "deactivate30")
		require.NoError(s.T(), err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the provisioned time
		// to a time far enough in the past to trigger auto deactivation. Subtracting the given period from the current time and setting this as the provisioned
		// time should test the behaviour of the deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
			UpdateStatus(murMember1.Name, hostAwait.Namespace,
				func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
				})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name, murMember1.Status.ProvisionedTime.String())

		// Use the same method above to change the provisioned time for the excluded user
		excludedMurMember1, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
			UpdateStatus(excludedMurMember1.Name, hostAwait.Namespace,
				func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
				})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", excludedMurMember1.Name, excludedMurMember1.Status.ProvisionedTime.String())

		_, err = hostAwait.WaitForUserSignup(s.T(), userSignupMember1.Name,
			wait.UntilUserSignupHasStates(toolchainv1alpha1.UserSignupStateDeactivated))
		require.NoError(s.T(), err)

		// The non-excluded user should be deactivated
		err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(s.T(), murMember1.Name)
		require.NoError(s.T(), err)

		err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(s.T(), murMember1.Name)
		require.NoError(s.T(), err)

		userSignupMember1, err = hostAwait.WaitForUserSignup(s.T(), userSignupMember1.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.DeactivatedWithoutDeactivating())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)
		require.True(s.T(), states.Deactivated(userSignupMember1), "usersignup should be deactivated")

		// The excluded user should still be active
		_, err = hostAwait.WaitForMasterUserRecord(s.T(), excludedMurMember1.Name)
		require.NoError(s.T(), err)
		deactivationExcludedUserSignupMember1, err = hostAwait.WaitForUserSignup(s.T(), deactivationExcludedUserSignupMember1.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)
		require.False(s.T(), states.Deactivated(deactivationExcludedUserSignupMember1), "deactivationExcludedUserSignup should not be deactivated")
	})

	s.Run("test deactivating state set OK", func() {
		// Reset configuration back to 3 days
		hostAwait.UpdateToolchainConfig(s.T(),
			testconfig.AutomaticApproval().Enabled(false),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		config := hostAwait.GetToolchainConfig(s.T())
		require.Equal(s.T(), 3, *config.Spec.Host.Deactivation.DeactivatingNotificationDays)

		userMember1 := NewSignupRequest(s.Awaitilities).
			Username("usertostartdeactivating").
			Email("usertostartdeactivating@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		userSignupMember1 := userMember1.UserSignup
		murMember1 := userMember1.MUR

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(murMember1)

		// Get the provisioned account's tier
		baseUserTier, err := hostAwait.WaitForUserTier(s.T(), "deactivate30")
		require.NoError(s.T(), err)

		// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
		// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
		// period from the current time and setting this as the provisioned time should test the behaviour of the
		// deactivation controller reconciliation.
		tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
		murMember1, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
			UpdateStatus(murMember1.Name, hostAwait.Namespace,
				func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
				})
		require.NoError(s.T(), err)
		s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", murMember1.Name,
			murMember1.Status.ProvisionedTime.String())

		// The user should be set to deactivating, but not deactivated
		_, err = hostAwait.WaitForUserSignup(s.T(), userSignupMember1.Name, wait.UntilUserSignupHasConditions(
			wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Deactivating())...))
		require.NoError(s.T(), err)

		// Verify resources still exist
		VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignupMember1)
	})

	s.Run("test full automatic user deactivation lifecycle", func() {
		// Set configuration to 3 days
		hostAwait.UpdateToolchainConfig(s.T(),
			testconfig.AutomaticApproval().Enabled(true),
			testconfig.Deactivation().DeactivatingNotificationDays(3))

		hostConfig := hostAwait.GetToolchainConfig(s.T()).Spec.Host
		require.Equal(s.T(), 3, *hostConfig.Deactivation.DeactivatingNotificationDays)

		// Create a new UserSignup
		user := NewSignupRequest(s.Awaitilities).
			Username("fulldeactivationlifecycle").
			Email("fulldeactivationlifecycle@redhat.com").
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
			Execute(s.T())
		userSignup := user.UserSignup

		// TODO remove once UserTier migration is completed
		s.promoteToDefaultUserTier(user.MUR)

		// Wait for the UserSignup to have the desired state
		userSignup, err := hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
			wait.UntilUserSignupHasScheduledDeactivationTime())
		require.NoError(s.T(), err)

		s.Run("user set to deactivating when provisioned time set in past", func() {
			// Get the provisioned account's tier
			baseUserTier, err := hostAwait.WaitForUserTier(s.T(), "deactivate30")
			require.NoError(s.T(), err)

			mur, err := hostAwait.WaitForMasterUserRecord(s.T(), user.Space.Name,
				wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()))
			require.NoError(s.T(), err)

			// We cannot wait days for testing deactivation so for the purposes of the e2e tests we use a hack to change the
			// provisioned time to a time far enough in the past to trigger the deactivation process. Subtracting the given
			// period from the current time and setting this as the provisioned time should test the behaviour of the
			// deactivation controller reconciliation.
			tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+1) * time.Hour * 24
			mur, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
				UpdateStatus(mur.Name, hostAwait.Namespace,
					func(mur *toolchainv1alpha1.MasterUserRecord) {
						mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
					})
			require.NoError(s.T(), err)
			s.T().Logf("masteruserrecord '%s' provisioned time adjusted to %s", mur.Name,
				mur.Status.ProvisionedTime.String())

			// The user should be set to deactivating, but not deactivated
			userSignup, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name, wait.UntilUserSignupHasScheduledDeactivationTime(),
				wait.UntilUserSignupHasConditions(
					wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically(), wait.Deactivating())...))
			require.NoError(s.T(), err)

			// The scheduled deactivation time should have also been updated, and should now expire in ~3 days
			expected := time.Now().Add(3 * time.Hour * 24)
			comparison := expected.Sub(userSignup.Status.ScheduledDeactivationTimestamp.Time)

			// accept if we're within 1 hour of the expected deactivation time
			require.Less(s.T(), comparison, time.Hour)

			// Verify resources have been provisioned
			VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup)

			s.Run("user set to deactivated after deactivating", func() {
				// Set the provisioned time even further back
				tierDeactivationDuration := time.Duration(baseUserTier.Spec.DeactivationTimeoutDays+4) * time.Hour * 24
				mur, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
					UpdateStatus(mur.Name, hostAwait.Namespace,
						func(mur *toolchainv1alpha1.MasterUserRecord) {
							mur.Status.ProvisionedTime = &metav1.Time{Time: time.Now().Add(-tierDeactivationDuration)}
						})
				murName := mur.Name
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
				require.True(s.T(), found)
				require.Equal(s.T(), deactivatingLastTransitionTime, updated.LastTransitionTime)

				// Save the updated UserSignup's Status
				userSignup, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
					UpdateStatus(userSignup.Name, hostAwait.Namespace, func(u *toolchainv1alpha1.UserSignup) {
						u.Status.Conditions = userSignup.Status.Conditions
					})

				require.NoError(s.T(), err)

				// Trigger a reconciliation of the deactivation controller by updating the MUR annotation
				_, err := wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
					Update(murName, hostAwait.Namespace,
						func(mur *toolchainv1alpha1.MasterUserRecord) {
							if mur.Annotations == nil {
								mur.Annotations = map[string]string{}
							}
							mur.Annotations["update-from-e2e-tests"] = "trigger"
						})
				if err != nil {
					// the mur might already be deleted, so we can continue as long as the error is the mur was not found
					require.EqualError(s.T(), err, fmt.Sprintf("masteruserrecords.toolchain.dev.openshift.com \"%s\" not found", murName))
				}

				// The user should now be set to deactivated
				userSignup, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
					wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.ApprovedAutomatically(), wait.Deactivated())...),
					wait.UntilUserSignupHasNilScheduledDeactivationTime())
				require.NoError(s.T(), err)

				// The MUR should also be deleted
				err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(s.T(), murName)
				require.NoError(s.T(), err)

				err = hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(s.T(), murName)
				require.NoError(s.T(), err)
			})
		})
	})

	s.Run("reactivated but unverified user reverted back to deactivated after timeout", func() {
		hostAwait.UpdateToolchainConfig(s.T(),
			testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup and wait for it to be provisioned
		user := NewSignupRequest(s.Awaitilities).
			Username("usertoreactivate").
			Email("usertoreactivate@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		userSignup := user.UserSignup

		// Wait for the UserSignup to have the desired state
		userSignup, err := hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
		require.NoError(s.T(), err)

		// Now deactivate the user
		userSignup, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(us, true)
				})
		require.NoError(s.T(), err)

		userSignup, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.DeactivatedWithoutPreDeactivation())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated),
			wait.UntilUserSignupHasStates(toolchainv1alpha1.UserSignupStateDeactivated))
		require.NoError(s.T(), err)

		// Set the unverified retention days to 0
		hostAwait.UpdateToolchainConfig(s.T(),
			testconfig.Deactivation().UserSignupUnverifiedRetentionDays(0))

		// Reactivate the user
		userSignup, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivating(us, false)
					states.SetDeactivated(us, false)
					states.SetApprovedManually(us, true)
					states.SetVerificationRequired(us, true)
				})
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' reactivated", userSignup.Name)

		// Since the config for retention days is set to 0, the account should be deactivated again immediately
		_, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.DeactivatedWithoutNotification())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated),
			wait.UntilUserSignupHasNilScheduledDeactivationTime(),
			wait.UntilUserSignupHasStates(toolchainv1alpha1.UserSignupStateDeactivated))
		require.NoError(s.T(), err)

		// Set the unverified retention days to 7
		hostAwait.UpdateToolchainConfig(s.T(),
			testconfig.Deactivation().UserSignupUnverifiedRetentionDays(7))
	})
}

func (s *userManagementTestSuite) TestUserBanning() {
	s.Run("register new user with preexisting ban", func() {
		hostAwait := s.Host()
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

		id := uuid.Must(uuid.NewV4()).String()
		email := "testuser" + id + "@test.com"
		CreateBannedUser(s.T(), s.Host(), email)

		// Wait for the BannedUser to be available before testing registration service
		emailHash := hash.EncodeString(email)
		_, err := hostAwait.WaitForBannedUser(s.T(), emailHash)
		require.NoError(s.T(), err)

		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		_, token0, err := authsupport.NewToken(authsupport.WithEmail(email))
		require.NoError(s.T(), err)
		s.verifyRegServiceForBannedUser(http.MethodPost, token0)
	})

	s.Run("ban provisioned usersignup", func() {
		hostAwait := s.Host()
		memberAwait := s.Member1()
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false))

		// Create a new UserSignup
		user := NewSignupRequest(s.Awaitilities).
			Username("banandunban").
			Email("banandunban@test.com").
			EnsureMUR().
			ManuallyApprove().
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T())
		userSignup := user.UserSignup

		// Create the BannedUser
		bannedUser := CreateBannedUser(s.T(), s.Host(), userSignup.Spec.IdentityClaims.Email)

		// Confirm the user is banned
		_, err := hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Banned())...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForMasterUserRecord(s.T(), userSignup.Spec.IdentityClaims.PreferredUsername)
		require.Error(s.T(), err)
		// confirm usersignup
		userSignup, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Banned())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
		require.NoError(s.T(), err)
		require.NoError(s.T(), hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(s.T(), user.Space.Name))

		s.verifyRegServiceForBannedUser(http.MethodGet, user.Token)

		s.Run("unban the banned user", func() {
			// Unban the user
			err = hostAwait.Client.Delete(context.TODO(), bannedUser)
			require.NoError(s.T(), err)

			// Confirm the BannedUser is deleted
			err = hostAwait.WaitUntilBannedUserDeleted(s.T(), bannedUser.Name)
			require.NoError(s.T(), err)

			// Confirm the user is provisioned
			userSignup, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(s.T(), userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)

			// Confirm the MUR is created
			_, err = hostAwait.WaitForMasterUserRecord(s.T(), user.MUR.Name)
			require.NoError(s.T(), err)
		})
	})
}

func (s *userManagementTestSuite) verifyRegServiceForBannedUser(method, token string) {
	hostAwait := s.Host()
	route := hostAwait.RegistrationServiceURL
	// Call signup endpoint with a valid token to initiate a signup process
	req, err := http.NewRequest(method, route+"/api/v1/signup", nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")

	resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
	require.NoError(s.T(), err)
	defer Close(s.T(), resp)

	body, err := io.ReadAll(resp.Body)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), body)
	assert.Equal(s.T(), http.StatusForbidden, resp.StatusCode)

	// Check the error.
	statusErr := make(map[string]interface{})
	err = json.Unmarshal(body, &statusErr)
	require.NoError(s.T(), err)
	require.Equal(s.T(), "forbidden: Access to the Developer Sandbox has been suspended due to suspicious activity or detected abuse.", statusErr["message"])
}

func (s *userManagementTestSuite) TestUserDisabled() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false))

	// Create UserSignup
	u := NewSignupRequest(s.Awaitilities).
		Username("janedoe").
		EnsureMUR().
		ManuallyApprove().
		TargetCluster(memberAwait).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T())
	userSignup := u.UserSignup

	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup)

	// Disable MUR
	mur, err := wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
		Update(u.MUR.Name, hostAwait.Namespace, func(mur *toolchainv1alpha1.MasterUserRecord) {
			mur.Spec.Disabled = true
		})
	require.NoError(s.T(), err)

	// Wait until the UserAccount status is disabled
	userAccount, err := memberAwait.WaitForUserAccount(s.T(), mur.Name,
		wait.UntilUserAccountHasConditions(wait.Disabled()))
	require.NoError(s.T(), err)

	// Wait until the MUR status is disabled
	mur, err = hostAwait.WaitForMasterUserRecord(s.T(), userSignup.Spec.IdentityClaims.PreferredUsername,
		wait.UntilMasterUserRecordHasConditions(wait.Disabled(), wait.ProvisionedNotificationCRCreated()))
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
	err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: identitypkg.NewIdentityNamingStandard(userAccount.Spec.PropagatedClaims.UserID, "rhd").IdentityName()}, identity)
	require.Error(s.T(), err)
	assert.True(s.T(), apierrors.IsNotFound(err))

	s.Run("re-enabled mur", func() {
		// Re-enable MUR
		mur, err = wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
			Update(mur.Name, hostAwait.Namespace,
				func(mur *toolchainv1alpha1.MasterUserRecord) {
					mur.Spec.Disabled = false
				})
		require.NoError(s.T(), err)

		VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup)
	})
}

func (s *userManagementTestSuite) TestReturningUsersProvisionedToLastCluster() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()

	s.Run("test returning user provisioned to same cluster", func() {
		// given
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false))
		clustersToTest := []*wait.MemberAwaitility{memberAwait, memberAwait2}

		for i, initialTargetCluster := range clustersToTest {
			// when
			s.Run(fmt.Sprintf("cluster %s: user activated->deactivated->reactivated", initialTargetCluster.ClusterName), func() {
				// given
				user := NewSignupRequest(s.Awaitilities).
					Username(fmt.Sprintf("returninguser%d", i)).
					Email(fmt.Sprintf("returninguser%d@redhat.com", i)).
					EnsureMUR().
					ManuallyApprove().
					TargetCluster(initialTargetCluster). // use TargetCluster initially to force user to provision to the expected cluster
					RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
					Execute(s.T())
				userSignup := user.UserSignup
				// when
				DeactivateAndCheckUser(s.T(), s.Awaitilities, userSignup)
				// If TargetCluster is set it will override the last cluster annotation so remove TargetCluster
				userSignup, err := wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
					Update(userSignup.Name, hostAwait.Namespace,
						func(us *toolchainv1alpha1.UserSignup) {
							us.Spec.TargetCluster = ""
						})
				require.NoError(s.T(), err)

				ReactivateAndCheckUser(s.T(), s.Awaitilities, userSignup)
				mur2, err := hostAwait.WaitForMasterUserRecord(s.T(), user.Space.Name,
					wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()))

				// then
				require.NoError(s.T(), err)
				secondSignupCluster := GetMurTargetMember(s.T(), s.Awaitilities, mur2)
				require.Equal(s.T(), initialTargetCluster.ClusterName, secondSignupCluster.ClusterName)
			})
		}
	})
}

// TODO remove once UserTier migration is completed
func (s *userManagementTestSuite) promoteToDefaultUserTier(mur *toolchainv1alpha1.MasterUserRecord) {
	hostAwait := s.Host()
	_, err := wait.For(s.T(), hostAwait.Awaitility, &toolchainv1alpha1.MasterUserRecord{}).
		Update(mur.Name, hostAwait.Namespace, func(m *toolchainv1alpha1.MasterUserRecord) {
			m.Spec.TierName = "deactivate30"
		})

	require.NoError(s.T(), err)
}
