package e2e

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

)

type userManagementIntegrationTest struct {
	baseUserIntegrationTest
}

func TestRunUserManagementIntegrationTest(t *testing.T) {
	suite.Run(t, &userManagementIntegrationTest{})
}

func (s *userManagementIntegrationTest) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.testCtx, s.awaitility = testsupport.WaitForDeployments(s.T(), userSignupList)
	s.hostAwait = s.awaitility.Host()
	s.namespace = s.awaitility.HostNs
}

func (s *userManagementIntegrationTest) TearDownTest() {
	s.testCtx.Cleanup()
}

func (s *userManagementIntegrationTest) TestUserDeactivation() {
	s.setApprovalPolicyConfig("automatic")

	userSignup, mur := s.createAndCheckUserSignup(true, "iris-at-redhat-com", "iris@redhat.com", approvedByAdmin()...)

	// Deactivate the user
	userSignup.Spec.Deactivated = true
	err := s.awaitility.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

	err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
	require.NoError(s.T(), err)
}

func (s *userManagementIntegrationTest) TestUserBanning() {

	s.T().Run("user banning", func(t *testing.T) {
		// when
		s.setApprovalPolicyConfig("automatic")

		// then
		s.checkUserBanned()
	})


}

func (s *userManagementIntegrationTest) checkUserBanned() {
	var userSignup *v1alpha1.UserSignup

	s.T().Run("usersignup created first with auto approval and not banned", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to true
		userSignup, _ = s.createUserSignupAndAssertAutoApproval(true)

		// Check the UserSignup is approved now
		userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedByAdmin()...))
		require.NoError(s.T(), err)

		// Confirm the MUR was created
		s.assertCreatedMUR(userSignup)
	})

	s.T().Run("banneduser created with approved set to true", func(t *testing.T) {
		// Create the BannedUser
		s.createAndCheckBannedUser(userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm that a MasterUserRecord is deleted
		_, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		require.Error(s.T(), err)
	})
}

func (s *userManagementIntegrationTest) createUserSignupAndAssertAutoApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", approvedAutomatically()...)
}

func (s *userManagementIntegrationTest) createUserSignupAndAssertPendingApproval() *v1alpha1.UserSignup {
	// Create a new UserSignup with approved flag set to false
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), username, email)

	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
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

func newBannedUser(t *testing.T, host *wait.HostAwaitility, email string) *v1alpha1.BannedUser {
	md5hash := md5.New()
	_, _ = md5hash.Write([]byte(email))
	emailHash := hex.EncodeToString(md5hash.Sum(nil))

	return &v1alpha1.BannedUser{
		ObjectMeta: v1.ObjectMeta{
			Name: uuid.NewV4().String(),
			Labels: map[string]string{
				v1alpha1.BannedUserEmailHashLabelKey: emailHash,
			},
		},
		Spec:       v1alpha1.BannedUserSpec{
			Email: email,
		},
	}
}

