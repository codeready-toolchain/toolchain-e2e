package e2e

import (
	"context"

	"github.com/codeready-toolchain/toolchain-common/pkg/states"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type baseUserIntegrationTest struct {
	suite.Suite
	ctx          *framework.Context
	hostAwait    *wait.HostAwaitility
	memberAwait  *wait.MemberAwaitility
	member2Await *wait.MemberAwaitility
}

// createAndCheckUserSignup creates a new UserSignup resource with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// targetCluster ensures the UserSignup is created with Spec.TargetCluster set to member cluster associated with the provided member awaitility, a 'nil' value will skip setting Spec.TargetCluster
//
// The method then waits until the UserSignup contains the given set of conditions and the corresponding MUR is created
func (s *baseUserIntegrationTest) createAndCheckUserSignup(specApproved bool, username string, email string, targetCluster *wait.MemberAwaitility,
	conditions ...toolchainv1alpha1.Condition) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord) {

	userSignup := s.createAndCheckUserSignupNoMUR(specApproved, username, email, targetCluster, conditions...)

	// Confirm the MUR was created and ready
	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, userSignup, "base", s.memberAwait, s.member2Await)
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	return userSignup, mur
}

// createAndCheckUserSignupNoMUR creates a new UserSignup resoruce with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// targetCluster ensures the UserSignup is created with Spec.TargetCluster set to member cluster associated with the provided member awaitility, a 'nil' value will skip setting Spec.TargetCluster
//
// The method then waits until the UserSignup contains the given set of conditions
func (s *baseUserIntegrationTest) createAndCheckUserSignupNoMUR(specApproved bool, username string, email string, targetCluster *wait.MemberAwaitility,
	conditions ...toolchainv1alpha1.Condition) *toolchainv1alpha1.UserSignup {

	WaitUntilBaseNSTemplateTierIsUpdated(s.T(), s.hostAwait)
	// Create a new UserSignup with the given approved flag
	userSignup := NewUserSignup(s.T(), s.hostAwait, username, email)
	states.SetApproved(userSignup, specApproved)
	if targetCluster != nil {
		userSignup.Spec.TargetCluster = targetCluster.ClusterName
	}
	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(conditions...))
	require.NoError(s.T(), err)

	return userSignup
}

func (s *baseUserIntegrationTest) createAndCheckBannedUser(email string) *toolchainv1alpha1.BannedUser {
	// Create the BannedUser
	bannedUser := newBannedUser(s.hostAwait, email)
	err := s.hostAwait.FrameworkClient.Create(context.TODO(), bannedUser, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)

	s.T().Logf("BannedUser '%s' created", bannedUser.Spec.Email)
	return bannedUser
}

func newBannedUser(host *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	return &toolchainv1alpha1.BannedUser{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.NewV4().String(),
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

func (s *baseUserIntegrationTest) deactivateAndCheckUser(userSignup *toolchainv1alpha1.UserSignup, mur *toolchainv1alpha1.MasterUserRecord) {
	userSignup, err := s.hostAwait.UpdateUserSignupSpec(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		states.SetDeactivated(us, true)
	})
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' set to deactivated", userSignup.Name)

	err = s.hostAwait.WaitUntilMasterUserRecordDeleted(mur.Name)
	require.NoError(s.T(), err)

	// "deactivated"
	notifications, err := s.hostAwait.WaitForNotifications(userSignup.Status.CompliantUsername, toolchainv1alpha1.NotificationTypeDeactivated, 1, wait.UntilNotificationHasConditions(Sent()))
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), notifications)
	require.Len(s.T(), notifications, 1)
	notification := notifications[0]
	assert.Contains(s.T(), notification.Name, userSignup.Status.CompliantUsername+"-deactivated-")
	assert.Equal(s.T(), userSignup.Namespace, notification.Namespace)
	assert.Equal(s.T(), "userdeactivated", notification.Spec.Template)
	assert.Equal(s.T(), userSignup.Name, notification.Spec.UserID)

	err = s.hostAwait.WaitUntilNotificationsDeleted(userSignup.Status.CompliantUsername, toolchainv1alpha1.NotificationTypeDeactivated)
	require.NoError(s.T(), err)

	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(DeactivatedWithoutPreDeactivation()...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
	require.NoError(s.T(), err)
	require.True(s.T(), states.Deactivated(userSignup), "usersignup should be deactivated")
}

func (s *baseUserIntegrationTest) reactivateAndCheckUser(userSignup *toolchainv1alpha1.UserSignup, mur *toolchainv1alpha1.MasterUserRecord) {
	err := s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: userSignup.Namespace,
		Name:      userSignup.Name,
	}, userSignup)
	require.NoError(s.T(), err)

	userSignup, err = s.hostAwait.UpdateUserSignupSpec(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		states.SetDeactivating(us, false)
		states.SetDeactivated(us, false)
		states.SetApproved(us, true)
	})
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' reactivated", userSignup.Name)

	_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
	require.NoError(s.T(), err)

	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
	require.NoError(s.T(), err)
	require.False(s.T(), states.Deactivated(userSignup), "usersignup should not be deactivated")
}
