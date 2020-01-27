package e2e

import (
	"context"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"

	"github.com/stretchr/testify/suite"
)

func TestUserManagement(t *testing.T) {
	suite.Run(t, &userManagementTestSuite{})
}

type userManagementTestSuite struct {
	suite.Suite
	namespace  string
	route      string
	testCtx    *framework.TestCtx
	awaitility *wait.Awaitility
	hostAwait  *wait.HostAwaitility
	memberAwait *wait.MemberAwaitility
}


func (s *userManagementTestSuite) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.testCtx, s.awaitility = testsupport.WaitForDeployments(s.T(), userSignupList)
	s.hostAwait = s.awaitility.Host()
	s.memberAwait = s.awaitility.Member()
	s.namespace = s.awaitility.HostNs
}

func (s *userManagementTestSuite) TearDownTest() {
	s.testCtx.Cleanup()
}

func (s *userManagementTestSuite) TestUserDisabled() {
	testUserIdentity := &authsupport.Identity{
		ID:       uuid.NewV4(),
		Username: "janedoe",
	}

	// Post signUp
	postSignup(s.T(), s.awaitility.RegistrationServiceURL, *testUserIdentity)
	userSignup, err := s.awaitility.Host().WaitForUserSignup(testUserIdentity.ID.String(), wait.UntilUserSignupHasConditions(pendingApproval()...))
	require.NoError(s.T(), err)

	// Approve signUp
	userSignup.Spec.Approved = true
	err = s.awaitility.Host().Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	_, err = s.awaitility.Host().WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedByAdmin()...))
	require.NoError(s.T(), err)

	// Get MasterUserRecord
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Get the UserAccount
	userAccount, err := s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(provisioned()))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Get User
	_, err = s.memberAwait.WaitForUser(userAccount.Name)
	require.NoError(s.T(), err)

	// Get Identity
	_, err = s.memberAwait.WaitForIdentity(toIdentityName(userAccount.Spec.UserID))
	require.NoError(s.T(), err)

	tmplTier := &v1alpha1.NSTemplateTier{}
	err = s.awaitility.Member().Client.Get(context.TODO(), types.NamespacedName{Namespace: s.awaitility.HostNs, Name: "basic"}, tmplTier)
	require.NoError(s.T(), err)

	// Get the latest UserAccount
	userAccount, err = s.memberAwait.WaitForUserAccount(userAccount.Name)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Disable account
	userAccount.Spec.Disabled = true
	err = s.awaitility.Member().Client.Update(context.TODO(), userAccount)
	require.NoError(s.T(), err)

	tmplTier = &v1alpha1.NSTemplateTier{}
	err = s.awaitility.Member().Client.Get(context.TODO(), types.NamespacedName{Namespace: s.awaitility.HostNs, Name: "basic"}, tmplTier)
	require.NoError(s.T(), err)


	// Get User - should be not found
	err = s.memberAwait.WaitUntilUserDeleted(userAccount.Name)
	require.NoError(s.T(), err)

	// Get Identity - should be not found
	err = s.memberAwait.WaitUntilIdentityDeleted(toIdentityName(userAccount.Spec.UserID))
	require.NoError(s.T(), err)

	// Get the latest UserAccount
	userAccount, err = s.memberAwait.WaitForUserAccount(userAccount.Name,
		wait.UntilUserAccountHasConditions(disabled()))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Disable account
	userAccount.Spec.Disabled = false
	err = s.awaitility.Member().Client.Update(context.TODO(), userAccount)
	require.NoError(s.T(), err)

	// Get User
	_, err = s.memberAwait.WaitForUser(userAccount.Name)
	require.NoError(s.T(), err)

	// Get Identity
	_, err = s.memberAwait.WaitForIdentity(toIdentityName(userAccount.Spec.UserID))
	require.NoError(s.T(), err)

	tmplTier = &v1alpha1.NSTemplateTier{}
	err = s.awaitility.Member().Client.Get(context.TODO(), types.NamespacedName{Namespace: s.awaitility.HostNs, Name: "basic"}, tmplTier)
	require.NoError(s.T(), err)
}
