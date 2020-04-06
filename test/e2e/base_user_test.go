package e2e

import (
	"context"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type baseUserIntegrationTest struct {
	suite.Suite
	namespace  string
	ctx        *framework.Context
	awaitility *wait.Awaitility
	hostAwait  *wait.HostAwaitility
}

func (s *baseUserIntegrationTest) clearApprovalPolicyConfig() error {
	cm := &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name: "toolchain-saas-config",
		},
	}

	err := s.awaitility.KubeClient.CoreV1().ConfigMaps(s.namespace).Delete(cm.Name, nil)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (s *baseUserIntegrationTest) setApprovalPolicyConfig(policy string) {
	// Create a new ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name: "toolchain-saas-config",
		},
	}

	// Clear the current approval policy
	err := s.clearApprovalPolicyConfig()
	require.NoError(s.T(), err)

	cmValues := make(map[string]string)
	cmValues["user-approval-policy"] = policy
	cm.Data = cmValues
	_, err = s.awaitility.KubeClient.CoreV1().ConfigMaps(s.namespace).Create(cm)
	require.NoError(s.T(), err)

	// Confirm it was updated
	cm, err = s.awaitility.KubeClient.CoreV1().ConfigMaps(s.namespace).Get("toolchain-saas-config", v1.GetOptions{})
	require.NoError(s.T(), err)
	require.Equal(s.T(), policy, cm.Data["user-approval-policy"])
}

func (s *baseUserIntegrationTest) createAndCheckUserSignup(specApproved bool, username string, email string,
	conditions ...v1alpha1.Condition) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {

	userSignup := s.createAndCheckUserSignupNoMUR(specApproved, username, email, conditions...)

	// Confirm the MUR was created and ready
	r := tiers.GetRevisions(s.awaitility, "basic", "code", "dev", "stage")
	verifyResourcesProvisionedForSignup(s.T(), s.awaitility, *userSignup, r, "basic")
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	return userSignup, mur
}

func (s *baseUserIntegrationTest) createAndCheckUserSignupNoMUR(specApproved bool, username string, email string,
	conditions ...v1alpha1.Condition) *v1alpha1.UserSignup {

	// Create a new UserSignup with the given approved flag
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), username, email)
	userSignup.Spec.Approved = specApproved
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(conditions...))
	require.NoError(s.T(), err)

	return userSignup
}

func (s *baseUserIntegrationTest) createAndCheckBannedUser(email string) *v1alpha1.BannedUser {
	// Create the BannedUser
	bannedUser := newBannedUser(s.awaitility.Host(), email)
	err := s.awaitility.Client.Create(context.TODO(), bannedUser, testsupport.CleanupOptions(s.ctx))
	require.NoError(s.T(), err)

	s.T().Logf("BannedUser '%s' created", bannedUser.Spec.Email)
	return bannedUser
}

func newBannedUser(host *wait.HostAwaitility, email string) *v1alpha1.BannedUser {
	return &v1alpha1.BannedUser{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.NewV4().String(),
			Namespace: host.Ns,
			Labels: map[string]string{
				v1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: v1alpha1.BannedUserSpec{
			Email: email,
		},
	}
}
