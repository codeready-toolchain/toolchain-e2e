package e2e

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"crypto/md5"
	"encoding/hex"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/prometheus/common/log"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"io/ioutil"
	apierros "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"time"
)

func TestUserManagement(t *testing.T) {
	suite.Run(t, &userManagementTestSuite{})
}

type userManagementTestSuite struct {
	baseUserIntegrationTest
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

func (s *userManagementTestSuite) TestUserDeactivation() {
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

func (s *userManagementTestSuite) TestUserBanning() {

	s.T().Run("user banning", func(t *testing.T) {
		// when
		s.setApprovalPolicyConfig("automatic")

		// then
		s.checkUserBanned()
	})

}

func (s *userManagementTestSuite) checkUserBanned() {
	s.T().Run("ban provisioned usersignup", func(t *testing.T) {
		s.setApprovalPolicyConfig("automatic")

		// Create a new UserSignup and confirm it was approved automatically
		userSignup, _ := s.createUserSignupAndAssertAutoApproval(false)

		// Create the BannedUser
		s.createAndCheckBannedUser(userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey])

		// Confirm the user is banned
		_, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(approvedAutomaticallyAndBanned()...))
		require.NoError(s.T(), err)

		// Confirm that a MasterUserRecord is deleted
		mur, err := s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(userSignup.Spec.Username)
		log.Infof("### Found MUR: ", mur.Name)
		require.Error(s.T(), err)
	})

	s.T().Run("create usersignup with preexisting banneduser", func(t *testing.T) {
		s.setApprovalPolicyConfig("automatic")

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		_ = s.createAndCheckUserSignupNoMUR(false, "testuser"+id, email, banned()...)
	})

	s.T().Run("register new user with preexisting ban", func(t *testing.T) {
		s.setApprovalPolicyConfig("automatic")

		id := uuid.NewV4().String()
		email := "testuser" + id + "@test.com"
		s.createAndCheckBannedUser(email)

		// Get valid generated token for e2e tests. IAT claim is overriden
		// to avoid token used before issued error.
		identity0 := authsupport.NewIdentity()
		emailClaim0 := authsupport.WithEmailClaim(email)
		token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
		require.NoError(s.T(), err)

		route := s.awaitility.RegistrationServiceURL

		// Call signup endpoint with a valid token to initiate a signup process
		req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token0)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)
		assert.Equal(s.T(), http.StatusAccepted, resp.StatusCode)
	})
}

func (s *userManagementTestSuite) TestUserDisabled() {
	// Create UserSignup
	userSignup := createAndApproveSignup(s.T(), s.awaitility, "janedoe")

	// Expected revisions
	revisions, err := getRevisions(s.awaitility, "basic", "code", "dev", "stage")
	require.NoError(s.T(), err)

	verifyResourcesProvisionedForSignup(s.T(), s.awaitility, userSignup, revisions, "basic")

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

	s.Run("re-enabled mur", func() {
		// Get MasterUserRecord
		mur, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Spec.Username)
		require.NoError(s.T(), err)

		// Re-enable MUR
		mur.Spec.Disabled = false
		err = s.awaitility.Host().Client.Update(context.TODO(), mur)
		require.NoError(s.T(), err)

		verifyResourcesProvisionedForSignup(s.T(), s.awaitility, userSignup, revisions, "basic")
	})
}

func (s *userManagementTestSuite) createUserSignupAndAssertAutoApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", approvedAutomatically()...)
}

func newBannedUser(host *wait.HostAwaitility, email string) *v1alpha1.BannedUser {
	md5hash := md5.New()
	_, _ = md5hash.Write([]byte(email))
	emailHash := hex.EncodeToString(md5hash.Sum(nil))

	return &v1alpha1.BannedUser{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.NewV4().String(),
			Namespace: host.Ns,
			Labels: map[string]string{
				v1alpha1.BannedUserEmailHashLabelKey: emailHash,
			},
		},
		Spec: v1alpha1.BannedUserSpec{
			Email: email,
		},
	}
}
