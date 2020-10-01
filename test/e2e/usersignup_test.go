package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

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

func (s *userSignupIntegrationTest) TestUserSignupApproval() {
	s.T().Run("no approval config - manual approval by default", func(t *testing.T) {
		// when
		// Clear the user approval policy
		err := s.clearApprovalPolicyConfig()
		require.NoError(s.T(), err)
		// then
		s.checkUserSignupManualApproval()
	})

	s.T().Run("manual approval policy", func(t *testing.T) {
		// when
		s.setApprovalPolicyConfig("manual")

		// then
		s.checkUserSignupManualApproval()
	})

	s.T().Run("auto approval policy", func(t *testing.T) {
		s.setApprovalPolicyConfig("automatic")

		t.Run("approved set to false", func(t *testing.T) {
			// UserSignup created with approved flag set to false but it should be approved anyway
			s.createUserSignupAndAssertAutoApproval(false)
		})

		t.Run("approved set to true", func(t *testing.T) {
			// UserSignup created with approved flag set to true and it should be approved with status "ApprovedByAdmin"
			// regardless of the auto approval policy setup
			s.createUserSignupAndAssertManualApproval(true)
		})
	})
}

func (s *userSignupIntegrationTest) TestUserSignupVerificationRequired() {
	s.T().Run("manual approval with verification required", func(t *testing.T) {
		s.setApprovalPolicyConfig("manual")

		t.Run("verification required set to true", func(t *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Create user signup
	s.setApprovalPolicyConfig("automatic")
	userSignup := NewUserSignup(s.T(), s.hostAwait, s.memberAwait, "reginald@alpha.com", "reginald@alpha.com")

	// Remove the specified target cluster
	userSignup.Spec.TargetCluster = ""
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
	userSignup, _ := s.createAndCheckUserSignup(true, "paul-no-need-to-transform", "paulnoneedtotransform@hotel.com", ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", ApprovedByAdmin()...)
	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = s.createAndCheckUserSignup(true, "paul", "paulathotel@hotel.com", ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-2", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the same original username but different user ID
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-3", userSignup.Status.CompliantUsername)
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertPendingApproval() *v1alpha1.UserSignup {
	// Create a new UserSignup with approved flag set to false
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := NewUserSignup(s.T(), s.hostAwait, s.memberAwait, username, email)

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(PendingApproval()...))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(username)
	require.Error(s.T(), err)
	return userSignup
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertManualApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", ApprovedByAdmin()...)
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertAutoApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", ApprovedAutomatically()...)
}

func (s *userSignupIntegrationTest) createUserSignupVerificationRequiredAndAssertNotProvisioned() *v1alpha1.UserSignup {
	// Create a new UserSignup
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := NewUserSignup(s.T(), s.hostAwait, s.memberAwait, username, email)

	// Set approved to true
	userSignup.Spec.Approved = true

	// Set verification required
	userSignup.Spec.VerificationRequired = true

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(ApprovedAndVerificationRequired()...))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(username)
	require.Error(s.T(), err)
	return userSignup
}

func (s *userSignupIntegrationTest) checkUserSignupManualApproval() {
	s.T().Run("usersignup created first then manually approved", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to false
		userSignup := s.createUserSignupAndAssertPendingApproval()

		// Manually approve the UserSignup
		userSignup.Spec.Approved = true
		err := s.hostAwait.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)

		// Check the UserSignup is approved now
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(ApprovedByAdmin()...))
		require.NoError(s.T(), err)

		// Confirm the MUR was created
		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
	})

	s.T().Run("usersignup created with approved set to true", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to true and assert approval
		s.createUserSignupAndAssertManualApproval(true)
	})
}
