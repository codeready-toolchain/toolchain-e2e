package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type userSignupIntegrationTest struct {
	suite.Suite
	namespace  string
	testCtx    *framework.TestCtx
	awaitility *wait.Awaitility
	hostAwait  *wait.HostAwaitility
}

func TestRunUserSignupIntegrationTest(t *testing.T) {
	suite.Run(t, &userSignupIntegrationTest{})
}

func (s *userSignupIntegrationTest) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.testCtx, s.awaitility = testsupport.WaitForDeployments(s.T(), userSignupList)
	s.hostAwait = s.awaitility.Host()
	s.namespace = s.awaitility.HostNs
}

func (s *userSignupIntegrationTest) TearDownTest() {
	s.testCtx.Cleanup()
}

func (s *userSignupIntegrationTest) TestUserSignupApproval() {
	s.T().Run("no approval config - manual approval by default", func(t *testing.T) {
		// when
		// Clear the user approval policy
		err := s.clearApprovalPolicyConfig()
		require.NoError(s.T(), err)
		// then
		s.checkUserSignupManualApproval()
	})

	s.T().Run("manual approval policy", func(t *testing.T) {
		// when
		s.setApprovalPolicyConfig("manual")

		// then
		s.checkUserSignupManualApproval()
	})

	s.T().Run("auto approval policy", func(t *testing.T) {
		s.setApprovalPolicyConfig("automatic")

		t.Run("approved set to false", func(t *testing.T) {
			// UserSignup created with approved flag set to false but it should be approved anyway
			s.createUserSignupAndAssertAutoApproval(false)
		})

		t.Run("approved set to true", func(t *testing.T) {
			// UserSignup created with approved flag set to true and it should be approved with status "ApprovedByAdmin"
			// regardless of the auto approval policy setup
			s.createUserSignupAndAssertManualApproval(true)
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Create user signup
	s.setApprovalPolicyConfig("automatic")
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), "reginald@alpha.com", "reginald@alpha.com")

	// Remove the specified target cluster
	userSignup.Spec.TargetCluster = ""
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedAutomatically()...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created and target cluster was set
	s.assertCreatedMUR(userSignup)
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := s.createAndCheckUserSignup(true, "paul-no-need-to-transform", "paulnoneedtotransform@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-at-hotel-com", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = s.createAndCheckUserSignup(true, "paul-at-hotel-com", "paulathotel@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-at-hotel-com-1", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the same original username but different user ID
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", approvedByAdmin()...)
	require.Equal(s.T(), "paul-at-hotel-com-2", userSignup.Status.CompliantUsername)
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertPendingApproval() *v1alpha1.UserSignup {
	// Create a new UserSignup with approved flag set to false
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), username, email)

	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(pendingApproval()...))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(username)
	require.Error(s.T(), err)
	return userSignup
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertManualApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", approvedByAdmin()...)
}

func (s *userSignupIntegrationTest) createUserSignupAndAssertAutoApproval(specApproved bool) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	id := uuid.NewV4().String()
	return s.createAndCheckUserSignup(specApproved, "testuser"+id, "testuser"+id+"@test.com", approvedAutomatically()...)
}

func (s *userSignupIntegrationTest) createAndCheckUserSignup(specApproved bool, username string, email string, conditions ...v1alpha1.Condition) (*v1alpha1.UserSignup, *v1alpha1.MasterUserRecord) {
	// Create a new UserSignup with the given approved flag
	userSignup := newUserSignup(s.T(), s.awaitility.Host(), username, email)
	userSignup.Spec.Approved = specApproved
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(conditions...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	mur := s.assertCreatedMUR(userSignup)

	return userSignup, mur
}

func (s *userSignupIntegrationTest) checkUserSignupManualApproval() {
	s.T().Run("usersignup created first then manually approved", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to false
		userSignup := s.createUserSignupAndAssertPendingApproval()

		// Manually approve the UserSignup
		userSignup.Spec.Approved = true
		err := s.awaitility.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)

		// Check the UserSignup is approved now
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedByAdmin()...))
		require.NoError(s.T(), err)

		// Confirm the MUR was created
		s.assertCreatedMUR(userSignup)
	})

	s.T().Run("usersignup created with approved set to true", func(t *testing.T) {
		// Create a new UserSignup with approved flag set to true and assert approval
		s.createUserSignupAndAssertManualApproval(true)
	})
}

func (s *userSignupIntegrationTest) assertCreatedMUR(userSignup *v1alpha1.UserSignup) *v1alpha1.MasterUserRecord {
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	require.Len(s.T(), mur.Spec.UserAccounts, 1)
	assert.Equal(s.T(), userSignup.Name, mur.Labels["toolchain.dev.openshift.com/user-id"])
	assert.Equal(s.T(), userSignup.Name, mur.Spec.UserAccounts[0].Spec.UserID)
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

func newUserSignup(t *testing.T, host *wait.HostAwaitility, username string, email string) *v1alpha1.UserSignup {
	memberCluster, ok, err := host.GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(t, err)
	require.True(t, ok)

	return &v1alpha1.UserSignup{
		ObjectMeta: v1.ObjectMeta{
			Name:      uuid.NewV4().String(),
			Namespace: host.Ns,
			Annotations: map[string]string{
				"toolchain.dev.openshift.com/user-email": email,
			},
		},
		Spec: v1alpha1.UserSignupSpec{
			Username:      username,
			TargetCluster: memberCluster.Name,
		},
	}
}

func (s *userSignupIntegrationTest) setApprovalPolicyConfig(policy string) {
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

func (s *userSignupIntegrationTest) clearApprovalPolicyConfig() error {
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
