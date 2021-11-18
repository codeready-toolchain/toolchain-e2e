package e2e

import (
	"context"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type baseUserIntegrationTest struct {
	suite.Suite
	wait.Awaitilities
}

func (s *baseUserIntegrationTest) createAndCheckBannedUser(email string) *toolchainv1alpha1.BannedUser {
	hostAwait := s.Host()
	// Create the BannedUser
	bannedUser := newBannedUser(hostAwait, email)
	err := hostAwait.CreateWithCleanup(context.TODO(), bannedUser)
	require.NoError(s.T(), err)

	s.T().Logf("BannedUser '%s' created", bannedUser.Spec.Email)
	return bannedUser
}

func newBannedUser(host *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	return &toolchainv1alpha1.BannedUser{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.Must(uuid.NewV4()).String(),
			Namespace: host.Namespace,
			Labels: map[string]string{
				toolchainv1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: toolchainv1alpha1.BannedUserSpec{
			Email: email,
		},
	}
}

func (s *baseUserIntegrationTest) deactivateAndCheckUser(userSignup *toolchainv1alpha1.UserSignup) *toolchainv1alpha1.UserSignup {
	hostAwait := s.Host()
	userSignup, err := hostAwait.UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		states.SetDeactivated(us, true)
	})
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

	err = hostAwait.WaitUntilMasterUserRecordDeleted(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// "deactivated"
	notifications, err := hostAwait.WaitForNotifications(userSignup.Status.CompliantUsername, toolchainv1alpha1.NotificationTypeDeactivated, 1, wait.UntilNotificationHasConditions(Sent()))
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), notifications)
	require.Len(s.T(), notifications, 1)
	notification := notifications[0]
	assert.Contains(s.T(), notification.Name, userSignup.Status.CompliantUsername+"-deactivated-")
	assert.Equal(s.T(), userSignup.Namespace, notification.Namespace)
	assert.Equal(s.T(), "userdeactivated", notification.Spec.Template)
	assert.Equal(s.T(), userSignup.Name, notification.Spec.Context["UserID"])

	err = hostAwait.WaitUntilNotificationsDeleted(userSignup.Status.CompliantUsername, toolchainv1alpha1.NotificationTypeDeactivated)
	require.NoError(s.T(), err)

	// We wait for the "Approved()" condition status here because it doesn't specify a reason for the approval,
	// and the reason should not be necessary for the purpose of this test.
	userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), DeactivatedWithoutPreDeactivation())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
	require.NoError(s.T(), err)
	require.True(s.T(), states.Deactivated(userSignup), "usersignup should be deactivated")

	return userSignup
}

func (s *baseUserIntegrationTest) reactivateAndCheckUser(userSignup *toolchainv1alpha1.UserSignup) *toolchainv1alpha1.UserSignup {
	hostAwait := s.Host()
	err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: userSignup.Namespace,
		Name:      userSignup.Name,
	}, userSignup)
	require.NoError(s.T(), err)

	userSignup, err = hostAwait.UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		states.SetDeactivating(us, false)
		states.SetDeactivated(us, false)
		states.SetApproved(us, true)
	})
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' reactivated", userSignup.Name)

	userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
	require.NoError(s.T(), err)
	require.False(s.T(), states.Deactivated(userSignup), "usersignup should not be deactivated")

	_, err = hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	return userSignup
}
