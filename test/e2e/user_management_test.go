package e2e

import (
	"context"
	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
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
	s.setApprovalPolicyConfig("automatic")



}