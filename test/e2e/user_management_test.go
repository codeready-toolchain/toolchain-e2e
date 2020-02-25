package e2e

import (
	"context"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	apierros "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	//"k8s.io/apimachinery/pkg/types"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	//userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
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
	// Create UserSignup
	userSignup := createAndApproveSignup(s.T(), s.awaitility, "janedoetina117")

	// Get MasterUserRecord
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Get the UserAccount
	userAccount, err := s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(provisioned()),
		wait.UntilUserAccountMatchesMur(mur.Spec, mur.Spec.UserAccounts[0].Spec))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Get User
	_, err = s.memberAwait.WaitForUser(userAccount.Name)
	require.NoError(s.T(), err)

	// Get Identity
	_, err = s.memberAwait.WaitForIdentity(toIdentityName(userAccount.Spec.UserID))
	require.NoError(s.T(), err)

	// Get MasterUserRecord
	mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Disable Accounts
	mur.Spec.Disabled = true
	err = s.awaitility.Host().Client.Update(context.TODO(), mur)
	require.NoError(s.T(), err)

	// Get UserAccount
	userAccount, err = s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(disabled()))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Check that the UserAccount is now set to disabled
	require.True(s.T(), userAccount.Spec.Disabled)

	// Get User
	user := &userv1.User{}
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: userAccount.Namespace}, user)
	require.Error(s.T(), err)
	assert.True(s.T(), apierros.IsNotFound(err))

	// Get Identity
	identity := &userv1.Identity{}
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(userAccount.Spec.UserID)}, identity)
	require.Error(s.T(), err)
	assert.True(s.T(), apierros.IsNotFound(err))

	// Get MasterUserRecord
	mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Enable Accounts
	mur.Spec.Disabled = false
	err = s.awaitility.Host().Client.Update(context.TODO(), mur)
	require.NoError(s.T(), err)

	// Get UserAccount
	userAccount, err = s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(provisioned()),
		wait.UntilUserAccountMatchesMur(mur.Spec, mur.Spec.UserAccounts[0].Spec))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), userAccount)

	// Check that the UserAccount is now enabled
	require.False(s.T(), userAccount.Spec.Disabled)

	// Get User
	user = &userv1.User{}
	_, err = s.memberAwait.WaitForUser(userAccount.Name)
	require.NoError(s.T(), err)

	// Get Identity
	identity = &userv1.Identity{}
	_, err = s.memberAwait.WaitForIdentity(toIdentityName(userAccount.Spec.UserID))
	require.NoError(s.T(), err)

}