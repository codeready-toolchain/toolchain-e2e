package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/assert"

	uuid "github.com/satori/go.uuid"
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
	userSignupList := &v1alpha1.UserSignupList{}
	s.ctx, s.hostAwait, s.memberAwait = WaitForDeployments(s.T(), userSignupList)
}

func (s *userSignupIntegrationTest) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userSignupIntegrationTest) TestAutomaticApproval() {
	// given
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

	// when & then
	s.createAndCheckUserSignup(false, "automatic1", "automatic1@redhat.com", false, ApprovedAutomatically()...)

	s.T().Run("set low capacity threshold and expect that user won't be approved nor provisioned", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled().ResourceCapThreshold(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(false, "automatic2", "automatic2@redhat.com", false, PendingApprovalNoCluster()...)

		// then
		assert.Equal(t, v1alpha1.UserSignupStateLabelValuePending, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		err := s.hostAwait.WaitUntilMasterUserRecordDeleted("automatic2")
		require.NoError(t, err)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedAutomatically()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
		})
	})

	s.T().Run("set low max number of users and expect that user won't be approved nor provisioned", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled().MaxUsersNumber(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(false, "automatic3", "automatic3@redhat.com", false, PendingApprovalNoCluster()...)

		// then
		assert.Equal(t, v1alpha1.UserSignupStateLabelValuePending, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		err := s.hostAwait.WaitUntilMasterUserRecordDeleted("automatic3")
		require.NoError(t, err)

		t.Run("reset the max number and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedAutomatically()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
		})
	})
}

func (s *userSignupIntegrationTest) TestManualApproval() {
	s.T().Run("default approval config - manual", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval())

		t.Run("user is approved manually", func(t *testing.T) {
			// when & then
			userSignup, _ := s.createAndCheckUserSignup(true, "manual1", "manual1@redhat.com", false, ApprovedByAdmin()...)
			assert.Equal(t, v1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		})
		t.Run("user is not approved manually thus won't be provisioned", func(t *testing.T) {
			// when
			userSignup := s.createAndCheckUserSignupNoMUR(false, "manual2", "manual2@redhat.com", false, PendingApproval()...)

			// then
			err := s.hostAwait.WaitUntilMasterUserRecordDeleted("manual2")
			require.NoError(t, err)
			assert.Equal(t, v1alpha1.UserSignupStateLabelValuePending, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		})
	})

}

func (s *userSignupIntegrationTest) TestCapacityManagementWithManualApproval() {
	// given
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

	// when & then
	s.createAndCheckUserSignup(true, "manualwithcapacity1", "manualwithcapacity1@redhat.com", false, ApprovedByAdmin()...)

	s.T().Run("set low capacity threshold and expect that user won't provisioned even when is approved manually", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled().ResourceCapThreshold(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(true, "manualwithcapacity2", "manualwithcapacity2@redhat.com", false, ApprovedByAdminNoCluster()...)

		// then
		err := s.hostAwait.WaitUntilMasterUserRecordDeleted("manualwithcapacity2")
		require.NoError(t, err)
		assert.Equal(t, v1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
		})
	})

	s.T().Run("set low max number of users and expect that user won't be provisioned even when is approved manually", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled().MaxUsersNumber(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(true, "manualwithcapacity3", "manualwithcapacity3@redhat.com", false, ApprovedByAdminNoCluster()...)

		// then
		err := s.hostAwait.WaitUntilMasterUserRecordDeleted("manualwithcapacity3")
		require.NoError(t, err)
		assert.Equal(t, v1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])

		t.Run("reset the max number and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
		})
	})

	s.T().Run("when approved and set target cluster manually, then the limits will be ignored", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled().ResourceCapThreshold(1).MaxUsersNumber(1))

		// when & then
		userSignup, _ := s.createAndCheckUserSignup(true, "withtargetcluster", "withtargetcluster@redhat.com", true, ApprovedByAdmin()...)
		assert.Equal(t, v1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
	})
}

func (s *userSignupIntegrationTest) TestUserSignupVerificationRequired() {
	s.T().Run("automatic approval with verification required", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval())

		t.Run("verification required set to true", func(t *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Create user signup
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

	userSignup := NewUserSignup(s.T(), s.hostAwait, s.memberAwait, "reginald@alpha.com", "reginald@alpha.com", false)

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(ApprovedAutomatically()...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created and target cluster was set
	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := s.createAndCheckUserSignup(true, "paul-no-need-to-transform", "paulnoneedtotransform@hotel.com", true, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", true, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = s.createAndCheckUserSignup(true, "paul", "paulathotel@hotel.com", true, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-2", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the same original username but different user ID
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", true, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-3", userSignup.Status.CompliantUsername)
}

func (s *userSignupIntegrationTest) createUserSignupVerificationRequiredAndAssertNotProvisioned() *v1alpha1.UserSignup {
	// Create a new UserSignup
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := NewUserSignup(s.T(), s.hostAwait, s.memberAwait, username, email, true)

	// Set approved to true
	userSignup.Spec.Approved = true

	// Set verification required
	userSignup.Spec.VerificationRequired = true

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(VerificationRequired()...),
		wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(username)
	require.Error(s.T(), err)
	return userSignup
}
