package e2e

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
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

func (s *userSignupIntegrationTest) TestUserSignupCreated() {
	// Clear the user approval policy
	err := s.clearApprovalPolicyConfig()
	require.NoError(s.T(), err)

	// Create user signup
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "foo@somewhere.com")

	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that a MasterUserRecord wasn't created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.Error(s.T(), err)

	// Delete the User Signup
	err = s.awaitility.Client.Delete(context.TODO(), userSignup)
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithNoApprovalConfig() {
	// Clear the user approval policy
	err := s.clearApprovalPolicyConfig()
	require.NoError(s.T(), err)

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "francis@domain.com")
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to false
	// 2) the Approved reason is set to PendingApproval
	// 3) the Complete condition is set to false
	// 4) the Complete reason is set to PendingApproval
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		})
	require.NoError(s.T(), err)

	// Create user signup - approval set to false
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup = s.newUserSignup(uuid.NewV4().String(), "gretel@somewhere.com")
	userSignup.Spec.Approved = false
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that the conditions are the same as if no approval value was set
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		})
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Now update the same userSignup setting approved to true
	userSignup.Spec.Approved = true
	err = s.awaitility.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Check the updated conditions
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)

	// Create user signup - approval set to true
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup = s.newUserSignup(uuid.NewV4().String(), "harold@indigo.com")
	userSignup.Spec.Approved = true
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedByAdmin
	// 3) the Complete condition is set to true
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithManualApproval() {
	// Set the user approval policy to manual
	s.setApprovalPolicyConfig("manual")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "mariecurie@violet.com")
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to false
	// 2) the Approved reason is set to PendingApproval
	// 3) the Complete condition is set to false
	// 4) the Complete reason is set to PendingApproval
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		})
	require.NoError(s.T(), err)

	// Confirm the MUR was NOT created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.Error(s.T(), err)

	// Create user signup - approval set to false
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup = s.newUserSignup(uuid.NewV4().String(), "janedoe@cyan.com")
	userSignup.Spec.Approved = false
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that the conditions are the same as if no approval value was set
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		})
	require.NoError(s.T(), err)

	// Confirm the MUR was NOT created yet
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.Error(s.T(), err)

	// Now, reload the userSignup, manually approve it (setting Approved to true) and update the resource
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	userSignup.Spec.Approved = true

	err = s.awaitility.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that the conditions are updated to reflect that the userSignup was approved
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)

	// Create user signup - approval set to true
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup = s.newUserSignup(uuid.NewV4().String(), "robertjones@magenta.com")
	userSignup.Spec.Approved = true
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedByAdmin
	// 3) the Complete condition is set to true
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "reginald@alpha.com")
	// Remove the specified target cluster
	userSignup.Spec.TargetCluster = ""
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)

	// Lookup the MUR
	mur := &v1alpha1.MasterUserRecord{}
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: s.namespace, Name: userSignup.Status.CompliantUsername}, mur)
	require.NoError(s.T(), err)

	require.Len(s.T(), mur.Spec.UserAccounts, 1)

	// Confirm the target cluster was set
	require.NotEmpty(s.T(), mur.Spec.UserAccounts[0].TargetCluster)
}

func (s *userSignupIntegrationTest) TestDeletedUserSignupIsGarbageCollected() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "oliver@bravo.com")
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Delete the UserSignup
	err = s.awaitility.Client.Delete(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Confirm the UserSignup was deleted
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.Error(s.T(), err)

	// Confirm the MasterUserRecord was deleted
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Name)
	require.Error(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalNoApprovalSet() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "charles@charlie.com")
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Get the newly created UserSignup resource
	userSignup = s.hostAwait.GetUserSignup(userSignup.Name)

	// Confirm the MasterUserRecord was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalMURValuesOK() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "theodore@delta.org")
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)

	// Lookup the MasterUserRecord
	mur := &v1alpha1.MasterUserRecord{}
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: userSignup.Status.CompliantUsername, Namespace: s.namespace}, mur)
	require.NoError(s.T(), err)

	require.Equal(s.T(), userSignup.Name, mur.Spec.UserID)
	require.Len(s.T(), mur.Spec.UserAccounts, 1)
	require.Equal(s.T(), userSignup.Name, mur.Spec.UserAccounts[0].Spec.UserID)
	require.Equal(s.T(), "default", mur.Spec.UserAccounts[0].Spec.NSLimit)
	require.NotNil(s.T(), mur.Spec.UserAccounts[0].Spec.NSTemplateSet)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalAndApprovalSetToFalse() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - approval set to false
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "dorothy@echo.net")
	userSignup.Spec.Approved = false
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that the conditions are as expected
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalAndApprovalSetToTrue() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - approval set to true
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(uuid.NewV4().String(), "edith@foxtrot.com")
	userSignup.Spec.Approved = true
	err := s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm the conditions
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalWhenMURAlreadyExists() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create a MUR
	s.T().Logf("Creating MasterUserRecord with namespace %s", s.namespace)
	userID := uuid.NewV4().String()
	mur := s.newMasterUserRecord("paul-at-hotel-com", userID)
	err := s.awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("MasterUserRecord '%s' created", mur.Name)

	// Confirm the MasterUserRecord was created
	err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
	require.NoError(s.T(), err)

	// Create user signup with the same name and UserID as the MUR
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup := s.newUserSignup(userID, "paul@hotel.com")
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("UserSignup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	err = s.hostAwait.WaitForUserSignupStatusConditions(userSignup.Name,
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		})
	require.NoError(s.T(), err)

}

func (s *userSignupIntegrationTest) newUserSignup(userID, username string) *v1alpha1.UserSignup {

	memberCluster, ok, err := s.awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(s.awaitility.T, err)
	require.True(s.awaitility.T, ok, "KubeFedCluster should exist")

	userSignup := &v1alpha1.UserSignup{
		ObjectMeta: v1.ObjectMeta{
			Name:      userID,
			Namespace: s.namespace,
		},
		Spec: v1alpha1.UserSignupSpec{
			Username:      username,
			TargetCluster: memberCluster.Name,
		},
	}

	return userSignup
}

func (s *userSignupIntegrationTest) newMasterUserRecord(name string, userID string) *v1alpha1.MasterUserRecord {
	memberCluster, ok, err := s.awaitility.Host().GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	require.NoError(s.awaitility.T, err)
	require.True(s.awaitility.T, ok, "KubeFedCluster should exist")

	userAccounts := []v1alpha1.UserAccountEmbedded{
		{
			TargetCluster: memberCluster.Name,
			Spec: v1alpha1.UserAccountSpec{
				UserID:  userID,
				NSLimit: "default",
				NSTemplateSet: v1alpha1.NSTemplateSetSpec{
					Namespaces: []v1alpha1.NSTemplateSetNamespace{},
				},
			},
		},
	}

	mur := &v1alpha1.MasterUserRecord{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
		},
		Spec: v1alpha1.MasterUserRecordSpec{
			UserID:       userID,
			UserAccounts: userAccounts,
		},
	}

	return mur
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
