package e2e

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	apierros "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func TestUserManagement(t *testing.T) {
	suite.Run(t, &userManagementTestSuite{})
}

type userManagementTestSuite struct {
	suite.Suite
	namespace   string
	testCtx     *framework.TestCtx
	awaitility  *wait.Awaitility
	hostAwait   *wait.HostAwaitility
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
	userSignup := createAndApproveSignup(s.T(), s.awaitility, "janedoe")

	// Expected revisions
	revisions, err := getRevisions(s.awaitility)
	require.NoError(s.T(), err)

	verifyResourcesProvisionedForSignup(s.T(), s.awaitility, userSignup, revisions)

	// Get MasterUserRecord
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	// Disable MUR
	mur.Spec.Disabled = true
	err = s.awaitility.Host().Client.Update(context.TODO(), mur)
	require.NoError(s.T(), err)

	// Wait until the UserAccount status is disabled
	userAccount, err := s.memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(disabled()))
	require.NoError(s.T(), err)

	// Wait until the MUR status is disabled
	mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username,
		wait.UntilMasterUserRecordHasConditions(disabled()))
	require.NoError(s.T(), err)

	// Check that the UserAccount is now set to disabled
	require.True(s.T(), userAccount.Spec.Disabled)

	// Check the User is deleted
	user := &userv1.User{}
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: userAccount.Namespace}, user)
	require.Error(s.T(), err)
	assert.True(s.T(), apierros.IsNotFound(err))

	// Check the Identity is deleted
	identity := &userv1.Identity{}
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(userAccount.Spec.UserID)}, identity)
	require.Error(s.T(), err)
	assert.True(s.T(), apierros.IsNotFound(err))

	// Get MasterUserRecord
	mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
	require.NoError(s.T(), err)

	s.Run("re-enabled mur", func() {
		// Re-enable MUR
		mur.Spec.Disabled = false
		err = s.awaitility.Host().Client.Update(context.TODO(), mur)
		require.NoError(s.T(), err)

		verifyResourcesProvisionedForSignup(s.T(), s.awaitility, userSignup, revisions)
	})
}
