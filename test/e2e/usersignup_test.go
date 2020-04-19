package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type userSignupIntegrationTest struct {
	baseUserIntegrationTest
}

func TestRunUserSignupIntegrationTest(t *testing.T) {
	suite.Run(t, &userSignupIntegrationTest{})
}

func (s *userSignupIntegrationTest) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.ctx, s.awaitility = testsupport.WaitForDeployments(s.T(), userSignupList)
	s.hostAwait = s.awaitility.Host()
	s.namespace = s.awaitility.HostNs
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

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Create user signup
	s.setApprovalPolicyConfig("automatic")
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), "reginald@alpha.com", "reginald@alpha.com")

	// Remove the specified target cluster
	userSignup.Spec.TargetCluster = ""
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedAutomatically()...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created and target cluster was set
	verifyResourcesProvisionedForSignup(s.T(), s.awaitility, *userSignup, "basic")
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := s.createAndCheckUserSignup(true, "paul-no-need-to-transform", "paulnoneedtotransform@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = s.createAndCheckUserSignup(true, "paul", "paulathotel@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-2", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the same original username but different user ID
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-3", userSignup.Status.CompliantUsername)
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertPendingApproval() *v1alpha1.UserSignup {
	// Create a new UserSignup with approved flag set to false
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), username, email)

	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(pendingApproval()...))
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
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", approvedByAdmin()...)
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertAutoApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", approvedAutomatically()...)
}

func (s *userSignupIntegrationTest) checkUserSignupManualApproval() {
	s.T().Run("usersignup created first then manually approved", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to false
		userSignup := s.createUserSignupAndAssertPendingApproval()

		// Manually approve the UserSignup
		userSignup.Spec.Approved = true
		err := s.awaitility.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)

		// Check the UserSignup is approved now
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedByAdmin()...))
		require.NoError(s.T(), err)

		// Confirm the MUR was created
		verifyResourcesProvisionedForSignup(s.T(), s.awaitility, *userSignup, "basic")
	})

	s.T().Run("usersignup created with approved set to true", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to true and assert approval
		s.createUserSignupAndAssertManualApproval(true)
	})
}

func newUserSignup(t *testing.T, host *wait.HostAwaitility, username string, email string) *v1alpha1.UserSignup {
	memberCluster, ok, err := host.GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(t, err)
	require.True(t, ok)

	return &v1alpha1.UserSignup{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.NewV4().String(),
			Namespace: host.Ns,
			Annotations: map[string]string{
				v1alpha1.UserSignupUserEmailAnnotationKey: email,
			},
			Labels: map[string]string{
				v1alpha1.UserSignupUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: v1alpha1.UserSignupSpec{
			Username:      username,
			TargetCluster: memberCluster.Name,
		},
	}
}
