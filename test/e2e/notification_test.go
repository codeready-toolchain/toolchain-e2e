package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/suite"
)

func TestNotifications(t *testing.T) {
	suite.Run(t, &notificationTestSuite{})
}

type notificationTestSuite struct {
	baseUserIntegrationTest
	memberAwait *wait.MemberAwaitility
}

func (s *notificationTestSuite) SetupSuite() {
	notificationList := &v1alpha1.NotificationList{}
	s.ctx, s.awaitility = testsupport.WaitForDeployments(s.T(), notificationList)
	s.hostAwait = s.awaitility.Host()
	s.memberAwait = s.awaitility.Member()
	s.namespace = s.awaitility.HostNs
}

func (s *notificationTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *notificationTestSuite) TestNotificationCleanup() {

	// Create and approve "janedoe"
	janedoeName := "janedoe"
	janeSignup := createAndApproveSignup(s.T(), s.awaitility, janedoeName)

	s.T().Run("notification created and deleted", func(t *testing.T) {

		hostAwait := wait.NewHostAwaitility(s.awaitility)
		memberAwait := wait.NewMemberAwaitility(s.awaitility)

		// Get the latest signup version
		userSignup, err := s.awaitility.Host().WaitForUserSignup(janeSignup.Name)
		require.NoError(t, err)

		// First, wait for the MasterUserRecord to exist, no matter what status
		mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
		require.NoError(t, err)

		// Then wait for the associated UserAccount to be provisioned
		userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
			wait.UntilUserAccountHasConditions(provisioned()))

		_, err = hostAwait.WaitForMasterUserRecord(mur.Name,
			wait.UntilMasterUserRecordHasConditions(provisioned(), provisionedNotificationCRCreated()))

		notification, err := hostAwait.WaitForNotification(userAccount.Name + "-provisioned")
		require.NoError(t, err)
		require.NotNil(t, notification)
		assert.Equal(t, userAccount.Name+"-provisioned", notification.Name)
		assert.Equal(t, mur.Namespace, notification.Namespace)
		assert.Equal(t, "userprovisioned", notification.Spec.Template)
		assert.Equal(t, userAccount.Spec.UserID, notification.Spec.UserID)

		hostAwait.WaitUntilNotificationDeleted(userAccount.Name + "-provisioned")
	})
}
