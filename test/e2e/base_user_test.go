package e2e

import (
	"context"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type baseUserIntegrationTest struct {
	suite.Suite
	ctx         *framework.Context
	hostAwait   *wait.HostAwaitility
	memberAwait *wait.MemberAwaitility
}

// createAndCheckUserSignup creates a new UserSignup resoruce with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// setTargetCluster defines if the UserSignup will be created with Spec.TargetCluster set to the first found member cluster name
//
// The method then waits until the UserSignup contains the given set of conditions and the corresponding MUR is created
func (s *baseUserIntegrationTest) createAndCheckUserSignup(specApproved bool, username string, email string, setTargetCluster bool,
	conditions ...v1alpha1.Condition) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {

	userSignup := s.createAndCheckUserSignupNoMUR(specApproved, username, email, setTargetCluster, conditions...)

	// Confirm the MUR was created and ready

	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, s.memberAwait, *userSignup, "basic")
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	return userSignup, mur
}

// createAndCheckUserSignupNoMUR creates a new UserSignup resoruce with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// setTargetCluster defines if the UserSignup will be created with Spec.TargetCluster set to the first found member cluster name
//
// The method then waits until the UserSignup contains the given set of conditions
func (s *baseUserIntegrationTest) createAndCheckUserSignupNoMUR(specApproved bool, username string, email string, setTargetCluster bool,
	conditions ...v1alpha1.Condition) *v1alpha1.UserSignup {

	// Create a new UserSignup with the given approved flag
	userSignup := NewUserSignup(s.T(), s.hostAwait, s.memberAwait, username, email, setTargetCluster)
	userSignup.Spec.Approved = specApproved
	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(conditions...))
	require.NoError(s.T(), err)

	return userSignup
}

func (s *baseUserIntegrationTest) createAndCheckBannedUser(email string) *v1alpha1.BannedUser {
	// Create the BannedUser
	bannedUser := newBannedUser(s.hostAwait, email)
	err := s.hostAwait.FrameworkClient.Create(context.TODO(), bannedUser, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)

	s.T().Logf("BannedUser '%s' created", bannedUser.Spec.Email)
	return bannedUser
}

func newBannedUser(host *wait.HostAwaitility, email string) *v1alpha1.BannedUser {
	return &v1alpha1.BannedUser{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.NewV4().String(),
			Namespace: host.Namespace,
			Labels: map[string]string{
				v1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: v1alpha1.BannedUserSpec{
			Email: email,
		},
	}
}
