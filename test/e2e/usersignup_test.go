package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type userSignupIntegrationTest struct {
	baseUserIntegrationTest
}

func TestRunUserSignupIntegrationTest(t *testing.T) {
	suite.Run(t, &userSignupIntegrationTest{})
}

func (s *userSignupIntegrationTest) SetupSuite() {
	userSignupList := &toolchainv1alpha1.UserSignupList{}
	s.ctx, s.hostAwait, s.memberAwait, s.member2Await = WaitForDeployments(s.T(), userSignupList)
}

func (s *userSignupIntegrationTest) TearDownTest() {
	s.ctx.Cleanup()
	s.hostAwait.Clean()
	s.memberAwait.Clean()
	s.member2Await.Clean()
}

func (s *userSignupIntegrationTest) TestAutomaticApproval() {
	// given
	s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

	// when & then
	s.newSignupRequest().
		Username("automatic1").
		Email("automatic1@redhat.com").
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
		Execute()

	s.T().Run("set low capacity threshold and expect that user won't be approved nor provisioned", func(t *testing.T) {
		// given
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled().ResourceCapThreshold(1))

		// when
		userSignup, _ := s.newSignupRequest().
			Username("automatic2").
			Email("automatic2@redhat.com").
			RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
			Execute().Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
		})
	})

	s.T().Run("set low max number of users and expect that user won't be approved nor provisioned but added on waiting list", func(t *testing.T) {
		// given
		toolchainStatus, err := s.hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
		require.NoError(t, err)
		originalMursPerDomainCount := toolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled().MaxUsersNumber(originalMursPerDomainCount["internal"] + originalMursPerDomainCount["external"]))

		// when
		userSignup1, _ := s.newSignupRequest().
			Username("waitinglist1").
			Email("waitinglist1@redhat.com").
			RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
			Execute().Resources()

		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		userSignup2, _ := s.newSignupRequest().
			Username("waitinglist2").
			Email("waitinglist2@redhat.com").
			RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
			Execute().Resources()

		// then
		s.userIsNotProvisioned(t, userSignup1)
		s.userIsNotProvisioned(t, userSignup2)

		t.Run("increment the max number of users and expect the first unapproved user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled().MaxUsersNumber(originalMursPerDomainCount["internal"] + originalMursPerDomainCount["external"] + 1))

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup1.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)

			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
			s.userIsNotProvisioned(t, userSignup2)

			t.Run("reset the max number and expect the second user will be provisioned as well", func(t *testing.T) {
				// when
				s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

				// then
				userSignup, err := s.hostAwait.WaitForUserSignup(userSignup2.Name,
					wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
					wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
				require.NoError(s.T(), err)

				VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
			})
		})
	})
}

func (s *userSignupIntegrationTest) TestProvisionToOtherClusterWhenOneIsFull() {
	s.T().Run("set per member clusters max number of users for both members and expect that users will be provisioned to the other member when one is full", func(t *testing.T) {
		// given
		var memberLimits []testconfig.PerMemberClusterOption
		toolchainStatus, err := s.hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
		require.NoError(t, err)
		for _, m := range toolchainStatus.Status.Members {
			if s.memberAwait.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(s.memberAwait.ClusterName, m.UserAccountCount+1))
			} else if s.member2Await.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(s.member2Await.ClusterName, m.UserAccountCount+1))
			}
		}
		require.Len(s.T(), memberLimits, 2)

		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled().MaxUsersNumber(0, memberLimits...))

		// when
		_, mur1 := s.newSignupRequest().
			Username("multimember-1").
			Email("multi1@redhat.com").
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute().Resources()

		_, mur2 := s.newSignupRequest().
			Username("multimember-2").
			Email("multi2@redhat.com").
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute().Resources()

		// then
		require.NotEqual(s.T(), mur1.Spec.UserAccounts[0].TargetCluster, mur2.Spec.UserAccounts[0].TargetCluster)

		t.Run("after both members are full then new signups won't be approved nor provisioned", func(t *testing.T) {
			// when
			userSignupPending, _ := s.newSignupRequest().
				Username("multimember-3").
				Email("multi3@redhat.com").
				RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
				Execute().Resources()

			// then
			s.userIsNotProvisioned(t, userSignupPending)
		})
	})
}

func (s *userSignupIntegrationTest) userIsNotProvisioned(t *testing.T, userSignup *toolchainv1alpha1.UserSignup) {
	s.hostAwait.CheckMasterUserRecordIsDeleted(userSignup.Spec.Username)
	currentUserSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(t, err)
	assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValuePending, currentUserSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
}

func (s *userSignupIntegrationTest) TestManualApproval() {
	s.T().Run("default approval config - manual", func(t *testing.T) {
		// given
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval())

		t.Run("user is approved manually", func(t *testing.T) {
			// when & then
			userSignup, _ := s.newSignupRequest().
				Username("manual1").
				Email("manual1@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute().Resources()

			assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		})
		t.Run("user is not approved manually thus won't be provisioned", func(t *testing.T) {
			// when
			userSignup, _ := s.newSignupRequest().
				Username("manual2").
				Email("manual2@redhat.com").
				RequireConditions(ConditionSet(Default(), PendingApproval())...).
				Execute().Resources()

			// then
			s.userIsNotProvisioned(t, userSignup)
		})
	})

}

func (s *userSignupIntegrationTest) TestCapacityManagementWithManualApproval() {
	// given
	s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())

	// when & then
	s.newSignupRequest().
		Username("manualwithcapacity1").
		Email("manualwithcapacity1@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute()

	s.T().Run("set low capacity threshold and expect that user won't provisioned even when is approved manually", func(t *testing.T) {
		// given
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled().ResourceCapThreshold(1))

		// when
		userSignup, _ := s.newSignupRequest().
			Username("manualwithcapacity2").
			Email("manualwithcapacity2@redhat.com").
			ManuallyApprove().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin(), ApprovedByAdminNoCluster())...).
			Execute().Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
		})
	})

	s.T().Run("set low max number of users and expect that user won't be provisioned even when is approved manually", func(t *testing.T) {
		// given
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled().MaxUsersNumber(1))

		// when
		userSignup, _ := s.newSignupRequest().
			Username("manualwithcapacity3").
			Email("manualwithcapacity3@redhat.com").
			ManuallyApprove().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin(), ApprovedByAdminNoCluster())...).
			Execute().Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the max number and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Disabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
		})
	})

	s.T().Run("when approved and set target cluster manually, then the limits will be ignored", func(t *testing.T) {
		// given
		s.hostAwait.UpdateToolchainConfig(
			testconfig.AutomaticApproval().Disabled().
				ResourceCapThreshold(1).
				MaxUsersNumber(1))

		// when & then
		userSignup, _ := s.newSignupRequest().
			Username("withtargetcluster").
			Email("withtargetcluster@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(s.memberAwait).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
	})
}

func (s *userSignupIntegrationTest) TestUserSignupVerificationRequired() {
	s.T().Run("automatic approval with verification required", func(t *testing.T) {
		s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval())

		t.Run("verification required set to true", func(t *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Create user signup
	s.hostAwait.UpdateToolchainConfig(testconfig.AutomaticApproval().Enabled())

	userSignup := NewUserSignup(s.T(), s.hostAwait, "reginald@alpha.com", "reginald@alpha.com")

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created and target cluster was set
	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := s.newSignupRequest().
		Username("paul-no-need-to-transform").
		Email("paulnoneedtotransform@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = s.newSignupRequest().
		Username("paul@hotel.com").
		Email("paul@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = s.newSignupRequest().
		Username("paul").
		Email("paulathotel@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	require.Equal(s.T(), "paul-2", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the same original username but different user ID
	userSignup, _ = s.newSignupRequest().
		Username("paul@hotel.com").
		Email("paul@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().Resources()

	require.Equal(s.T(), "paul-3", userSignup.Status.CompliantUsername)

	// Create another UserSignups with a forbidden prefix
	for _, prefix := range []string{"kube", "openshift", "default", "redhat", "sandbox"} {
		// prefix with hyphen
		userSignup, _ = s.newSignupRequest().
			Username(prefix + "-paul").
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%s-paul", prefix), userSignup.Status.CompliantUsername)

		// prefix without delimiter
		userSignup, _ = s.newSignupRequest().
			Username(prefix + "paul").
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%spaul", prefix), userSignup.Status.CompliantUsername)

		// prefix as a name
		userSignup, _ = s.newSignupRequest().
			Username(prefix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%s", prefix), userSignup.Status.CompliantUsername)
	}

	// Create another UserSignups with a forbidden suffix
	for _, suffix := range []string{"admin"} {
		// suffix with hyphen
		userSignup, _ = s.newSignupRequest().
			Username("paul-" + suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(s.T(), fmt.Sprintf("paul-%s-crt", suffix), userSignup.Status.CompliantUsername)

		// suffix without delimiter
		userSignup, _ = s.newSignupRequest().
			Username("paul" + suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(s.T(), fmt.Sprintf("paul%s-crt", suffix), userSignup.Status.CompliantUsername)

		// suffix as a name
		userSignup, _ = s.newSignupRequest().
			Username(suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().Resources()

		require.Equal(s.T(), fmt.Sprintf("%s-crt", suffix), userSignup.Status.CompliantUsername)
	}
}

func (s *userSignupIntegrationTest) createUserSignupVerificationRequiredAndAssertNotProvisioned() *toolchainv1alpha1.UserSignup {
	// Create a new UserSignup
	username := "testuser" + uuid.Must(uuid.NewV4()).String()
	email := username + "@test.com"
	userSignup := NewUserSignup(s.T(), s.hostAwait, username, email)
	userSignup.Spec.TargetCluster = s.memberAwait.ClusterName

	// Set approved to true
	states.SetApproved(userSignup, true)

	// Set verification required
	states.SetVerificationRequired(userSignup, true)

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(username)
	require.Error(s.T(), err)
	return userSignup
}
