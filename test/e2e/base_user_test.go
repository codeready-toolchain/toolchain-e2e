package e2e

import (
	"context"
	"github.com/stretchr/testify/assert"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type baseUserIntegrationTest struct {
	suite.Suite
	namespace  string
	testCtx    *framework.TestCtx
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

	// Confirm the MUR was created
	mur := s.assertCreatedMUR(userSignup)

	return userSignup, mur
}

func (s *baseUserIntegrationTest) createAndCheckUserSignupNoMUR(specApproved bool, username string, email string,
	conditions ...v1alpha1.Condition) *v1alpha1.UserSignup {

	// Create a new UserSignup with the given approved flag
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), username, email)
	userSignup.Spec.Approved = specApproved
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
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
	err := s.awaitility.Client.Create(context.TODO(), bannedUser, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)

	s.T().Logf("BannedUser '%s' created", bannedUser.Spec.Email)

	bannedUser, err = s.hostAwait.WaitForBannedUser(email)
	require.NoError(s.T(), err)

	return bannedUser
}

func (s *baseUserIntegrationTest) assertCreatedMUR(userSignup *v1alpha1.UserSignup) *v1alpha1.MasterUserRecord {
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	require.Len(s.T(), mur.Spec.UserAccounts, 1)
	assert.Equal(s.T(), userSignup.Name, mur.Labels["toolchain.dev.openshift.com/user-id"])
	assert.Equal(s.T(), "default", mur.Spec.UserAccounts[0].Spec.NSLimit)
	assert.NotNil(s.T(), mur.Spec.UserAccounts[0].Spec.NSTemplateSet)
	if userSignup.Spec.TargetCluster != "" {
		// Target cluster set manually from spec
		assert.Equal(s.T(), userSignup.Spec.TargetCluster, mur.Spec.UserAccounts[0].TargetCluster)
	} else {
		// Target cluster set automatically
		assert.NotEmpty(s.T(), mur.Spec.UserAccounts[0].TargetCluster)
	}

	return mur
}
