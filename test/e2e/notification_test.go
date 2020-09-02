package e2e

import (
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestNotifications(t *testing.T) {
	suite.Run(t, &notificationTestSuite{})
}

type notificationTestSuite struct {
	suite.Suite
	ctx         *framework.Context
	hostAwait   *wait.HostAwaitility
	memberAwait *wait.MemberAwaitility
}

func (s *notificationTestSuite) SetupSuite() {
	notificationList := &v1alpha1.NotificationList{}
	s.ctx, s.hostAwait, s.memberAwait = WaitForDeployments(s.T(), notificationList)
}

func (s *notificationTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *notificationTestSuite) TestNotificationCleanup() {

	// Create and approve "janedoe"
	janedoeName := "janedoe"
	CreateAndApproveSignup(s.T(), s.hostAwait, janedoeName)

	s.T().Run("notification created and deleted", func(t *testing.T) {
		mur, err := s.hostAwait.WaitForMasterUserRecord(janedoeName,
			wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
		require.NoError(t, err)

		notification, err := s.hostAwait.WaitForNotification(mur.Name+"-provisioned", wait.UntilNotificationHasConditions(Sent()))
		require.NoError(t, err)
		require.NotNil(t, notification)
		assert.Equal(t, mur.Name+"-provisioned", notification.Name)
		assert.Equal(t, mur.Namespace, notification.Namespace)
		assert.Equal(t, "userprovisioned", notification.Spec.Template)
		assert.Equal(t, mur.Spec.UserID, notification.Spec.UserID)

		err = s.hostAwait.WaitUntilNotificationDeleted(mur.Name + "-provisioned")
		require.NoError(t, err)
	})
}
