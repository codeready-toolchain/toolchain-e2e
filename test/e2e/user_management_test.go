package e2e

import (
	userv1 "github.com/openshift/api/user/v1"
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
	require.True(s.T(), true)
	//testUserIdentity := &authsupport.Identity{
	//	ID:       uuid.NewV4(),
	//	Username: "janedoe",
	//}

	userSignup := createAndApproveSignup(s.T(), s.awaitility, "janedoetina")

	// Post signUp
	//postSignup(s.T(), s.awaitility.RegistrationServiceURL, *testUserIdentity)
	//userSignup, err := s.awaitility.Host().WaitForUserSignup(testUserIdentity.ID.String(), wait.UntilUserSignupHasConditions(pendingApproval()...))
	//require.NoError(s.T(), err)

	// Approve signUp
	//userSignup.Spec.Approved = true
	//err = s.awaitility.Host().Client.Update(context.TODO(), userSignup)oc
	//require.NoError(s.T(), err)

	// Get MasterUserRecord
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Get the UserAccount
	userAccount, err := s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(provisioned()),
		wait.UntilUserAccountHasSpec(mur.Spec.UserAccounts[0].Spec))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Get User
	_, err = s.memberAwait.WaitForUser(userAccount.Name)
	require.NoError(s.T(), err)

	// Get Identity
	_, err = s.memberAwait.WaitForIdentity(toIdentityName(userAccount.Spec.UserID))
	require.NoError(s.T(), err)

	// Get the latest UserAccount
	userAccount, err = s.memberAwait.WaitForUserAccount(userAccount.Name)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Disable account
	mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	mur.Spec.Disabled = true
	err = s.awaitility.Host().Client.Update(context.TODO(), mur)
	require.NoError(s.T(), err)

	// Get the latest UserAccount
	userAccount, err = s.memberAwait.WaitForUserAccount(userAccount.Name)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Check that the UserAccount is now set to disabled
	require.True(s.T(), userAccount.Spec.Disabled)

	//// Get User
	//user := &userv1.User{}
	//s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: userAccount.Namespace}, user)
	////err = s.memberAwait.WaitUntilUserDeleted(userAccount.Name)
	//require.Error(s.T(), err)
	//
	//// Get Identity
	////err = s.memberAwait.WaitUntilIdentityDeleted(toIdentityName(userAccount.Spec.UserID))
	//identity := &userv1.Identity{}
	//s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(userAccount.Spec.UserID)}, identity)
	//require.Error(s.T(), err)
}