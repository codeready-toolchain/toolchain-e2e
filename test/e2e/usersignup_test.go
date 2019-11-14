package e2e

import (
	"context"
	"fmt"
	"testing"

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
	"k8s.io/apimachinery/pkg/types"
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

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "foo@somewhere.com")
	require.NoError(s.T(), err)

	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WaitForMasterUserRecord("foo-somewhere-com")
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

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "francis@domain.com")
	require.NoError(s.T(), err)

	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to false
	// 2) the Approved reason is set to PendingApproval
	// 3) the Complete condition is set to false
	// 4) the Complete reason is set to PendingApproval
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		}))
	require.NoError(s.T(), err)

	// Create user signup - approval set to false
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err = newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "gretel@somewhere.com")
	require.NoError(s.T(), err)

	userSignup.Spec.Approved = false
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that the conditions are the same as if no approval value was set
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		}))
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Now update the same userSignup setting approved to true
	userSignup.Spec.Approved = true
	err = s.awaitility.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Check the updated conditions
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)

	// Create user signup - approval set to true
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err = newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "harold@indigo.com")
	require.NoError(s.T(), err)

	userSignup.Spec.Approved = true
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedByAdmin
	// 3) the Complete condition is set to true
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithManualApproval() {
	// Set the user approval policy to manual
	s.setApprovalPolicyConfig("manual")

	s.T().Run("usersignup created first then manually approved", func(t *testing.T) {
		// Create user signup - no approval set
		s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
		userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "mariecurie@violet.com")
		require.NoError(s.T(), err)

		err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' created", userSignup.Name)

		// Confirm the UserSignup was created
		_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
		require.NoError(s.T(), err)

		// Confirm that:
		// 1) the Approved condition is set to false
		// 2) the Approved reason is set to PendingApproval
		// 3) the Complete condition is set to false
		// 4) the Complete reason is set to PendingApproval
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			}))
		require.NoError(s.T(), err)

		// Confirm the CompliantUsername has NOT been set
		require.Empty(s.T(), userSignup.Status.CompliantUsername)

		// Create user signup - approval set to false
		s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
		userSignup, err = newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "janedoe@cyan.com")
		require.NoError(s.T(), err)

		userSignup.Spec.Approved = false
		err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' created", userSignup.Name)

		// Confirm the UserSignup was created
		_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
		require.NoError(s.T(), err)

		// Confirm that the conditions are the same as if no approval value was set
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionFalse,
				Reason: "PendingApproval",
			}))
		require.NoError(s.T(), err)

		// Now, manually approve it (setting Approved to true) and update the resource
		userSignup.Spec.Approved = true

		err = s.awaitility.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)

		// Lookup the UserSignup again
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
		require.NoError(s.T(), err)

		require.NotEmpty(s.T(), userSignup.Status.CompliantUsername)

		// Confirm that the conditions are updated to reflect that the userSignup was approved
		_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionTrue,
				Reason: "ApprovedByAdmin",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionTrue,
			}))
		require.NoError(s.T(), err)

		// Confirm the MUR was created
		s.assertCreatedMUR(userSignup)
	})

	s.T().Run("usersignup created with approved set to true", func(t *testing.T) {
		// Create user signup - approval set to true
		s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
		userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "robertjones@magenta.com")
		require.NoError(s.T(), err)

		userSignup.Spec.Approved = true
		err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
		require.NoError(s.T(), err)
		s.T().Logf("user signup '%s' created", userSignup.Name)

		// Confirm that:
		// 1) Confirm the UserSignup was created
		// 2) the Approved condition is set to true
		// 3) the Approved reason is set to ApprovedByAdmin
		// 4) the Complete condition is set to true
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupApproved,
				Status: corev1.ConditionTrue,
				Reason: "ApprovedByAdmin",
			},
			v1alpha1.Condition{
				Type:   v1alpha1.UserSignupComplete,
				Status: corev1.ConditionTrue,
			}))
		require.NoError(s.T(), err)

		// Confirm the MUR was created
		s.assertCreatedMUR(userSignup)
	})
}


func (s *userSignupIntegrationTest) assertCreatedMUR(userSignup *v1alpha1.UserSignup) {
	mur, err := s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	require.Len(s.T(), mur.Spec.UserAccounts, 1)
	require.Equal(s.T(), userSignup.Name, mur.Labels["toolchain.dev.openshift.com/user-id"])
	require.Equal(s.T(), userSignup.Name, mur.Spec.UserAccounts[0].Spec.UserID)
	require.Equal(s.T(), "default", mur.Spec.UserAccounts[0].Spec.NSLimit)
	require.NotNil(s.T(), mur.Spec.UserAccounts[0].Spec.NSTemplateSet)
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "reginald@alpha.com")
	require.NoError(s.T(), err)

	// Remove the specified target cluster
	userSignup.Spec.TargetCluster = ""
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Get the newly created UserSignup resource
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
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

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "oliver@bravo.com")
	require.NoError(s.T(), err)
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))

	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Get the newly created UserSignup resource
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Delete the UserSignup
	err = s.awaitility.Client.Delete(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Confirm the UserSignup was deleted
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.Error(s.T(), err)

	// Confirm the MasterUserRecord was deleted
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Name)
	require.Error(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalNoApprovalSet() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "charles@charlie.com")
	require.NoError(s.T(), err)
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))

	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Get the newly created UserSignup resource
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalMURValuesOK() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - no approval set
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "theodore@delta.org")
	require.NoError(s.T(), err)
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))

	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Get the newly created UserSignup resource
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord was created
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Approved condition is set to true
	// 2) the Approved reason is set to ApprovedAutomatically
	// 3) the Complete condition is (eventually) set to true
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)

	// Check the MasterUserRecord
	s.assertCreatedMUR(userSignup)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalAndApprovalSetToFalse() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - approval set to false
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "dorothy@echo.net")
	require.NoError(s.T(), err)

	userSignup.Spec.Approved = false
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Confirm that the conditions are as expected
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalAndApprovalSetToTrue() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup - approval set to true
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "edith@foxtrot.com")
	require.NoError(s.T(), err)

	userSignup.Spec.Approved = true
	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Lookup the reconciled UserSignup
	err = s.awaitility.Client.Get(context.TODO(), types.NamespacedName{Namespace: userSignup.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Confirm the MUR was created
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)

	require.NoError(s.T(), err)

	// Confirm the conditions
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
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
	_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
	require.NoError(s.T(), err)

	// Create user signup with the same name and UserID as the MUR
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)

	userSignup, err := newUserSignup(s.awaitility.Host(), uuid.NewV4().String(), "paul@hotel.com")
	require.NoError(s.T(), err)

	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("UserSignup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(s.T(), err)

	// Confirm that:
	// 1) the Complete condition is set to true
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupWithAutoApprovalWhenMultipleMURAlreadyExists() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create a MUR
	s.T().Logf("Creating MasterUserRecord with namespace %s", s.namespace)
	userID := uuid.NewV4().String()
	mur := s.newMasterUserRecord("robert-at-hotel-com", userID)
	err := s.awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("MasterUserRecord '%s' with label %s created", mur.Name, mur.Labels["toolchain.dev.openshift.com/user-id"])

	// Confirm the MasterUserRecord was created
	_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
	require.NoError(s.T(), err)

	// Create a second MUR with the same UserID but different name
	s.T().Logf("Creating MasterUserRecord with namespace %s", s.namespace)
	mur = s.newMasterUserRecord("another-robert-at-hotel-com", userID)
	err = s.awaitility.Client.Create(context.TODO(), mur, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("MasterUserRecord '%s' with label %s created", mur.Name, mur.Labels["toolchain.dev.openshift.com/user-id"])

	// Confirm the MasterUserRecord was created
	_, err = s.hostAwait.WaitForMasterUserRecord(mur.Name)
	require.NoError(s.T(), err)

	// Create user signup with the same UserID as the MURs
	s.T().Logf("Creating UserSignup with namespace %s", s.namespace)
	userSignup, err := newUserSignup(s.awaitility.Host(), userID, "robert@hotel.com")
	require.NoError(s.T(), err)

	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("UserSignup '%s' created", userSignup.Name)

	// Confirm the UserSignup was created
	_, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:    v1alpha1.UserSignupComplete,
			Status:  corev1.ConditionFalse,
			Reason:  "InvalidMURState",
			Message: "multiple matching MasterUserRecord resources found",
		}))
	require.NoError(s.T(), err)
}

func (s *userSignupIntegrationTest) TestUserSignupsWithTheSameUsername() {
	// Set the user approval policy to automatic
	s.setApprovalPolicyConfig("automatic")

	// Create user signup with username that we don't need to transform and confirm the compliant username matches the original username
	s.createAndAssertUserSignup("bill-no-need-to-transform", "bill-no-need-to-transform")

	// Create user signup and confirm the generated compliant username
	s.createAndAssertUserSignup("bill@domain.org", "bill-at-domain-org")

	// Create another user signup with the same username but different user ID
	// and confirm the generated compliant username
	s.createAndAssertUserSignup("bill@domain.org", "bill-at-domain-org-1")
}

func (s *userSignupIntegrationTest) createAndAssertUserSignup(actualUsername, expectedCompliantUsername string) {
	// Create user signup
	userID := uuid.NewV4().String()
	userSignup, err := newUserSignup(s.awaitility.Host(), userID, actualUsername)
	require.NoError(s.T(), err)

	err = s.awaitility.Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(s.testCtx))
	require.NoError(s.T(), err)
	s.T().Logf("UserSignup with name '%s' and username %s created", userSignup.Name, userSignup.Spec.Username)

	// Confirm the UserSignup is completed and CompliantUsername status set
	userSignup, err = s.hostAwait.WaitForUserSignup(userID, wait.UntilUserSignupHasConditions(
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		v1alpha1.Condition{
			Type:   v1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		}))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), expectedCompliantUsername, userSignup.Status.CompliantUsername)

	// Confirm MUR was created too
	s.assertCreatedMUR(userSignup)
}

func newUserSignup(host *wait.HostAwaitility, userID, username string) (*v1alpha1.UserSignup, error) {
	memberCluster, ok, err := host.GetKubeFedCluster(cluster.Member, wait.ReadyKubeFedCluster)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("KubeFedCluster should exist")
	}

	userSignup := &v1alpha1.UserSignup{
		ObjectMeta: v1.ObjectMeta{
			Name:      userID,
			Namespace: host.Ns,
		},
		Spec: v1alpha1.UserSignupSpec{
			Username:      username,
			TargetCluster: memberCluster.Name,
		},
	}

	return userSignup, nil
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
			Labels:    map[string]string{v1alpha1.MasterUserRecordUserIDLabelKey: userID},
		},
		Spec: v1alpha1.MasterUserRecordSpec{
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
