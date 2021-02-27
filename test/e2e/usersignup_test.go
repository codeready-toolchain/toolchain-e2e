package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type userSignupIntegrationTest struct {
	baseUserIntegrationTest
}

func TestRunUserSignupIntegrationTest(t *testing.T) {
	suite.Run(t, &userSignupIntegrationTest{})
}

func (s *userSignupIntegrationTest) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.ctx, s.hostAwait, s.memberAwait, s.memberAwait2 = WaitForDeployments(s.T(), userSignupList)
}

func (s *userSignupIntegrationTest) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userSignupIntegrationTest) TestAutomaticApproval() {
	// given
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

	// when & then
	s.createAndCheckUserSignup(false, "automatic1", "automatic1@redhat.com", nil, ApprovedAutomatically()...)

	s.T().Run("set low capacity threshold and expect that user won't be approved nor provisioned", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled().ResourceCapThreshold(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(false, "automatic2", "automatic2@redhat.com", nil, PendingApprovalNoCluster()...)

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedAutomatically()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait, s.memberAwait2)
		})
	})

	s.T().Run("set low max number of users and expect that user won't be approved nor provisioned but added on waiting list", func(t *testing.T) {
		// given
		toolchainStatus, err := s.hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
		require.NoError(t, err)
		initialMurCount := toolchainStatus.Status.HostOperator.MasterUserRecordCount
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled().MaxUsersNumber(initialMurCount))

		// when
		userSignup1 := s.createAndCheckUserSignupNoMUR(false, "waitinglist1", "waitinglist1@redhat.com", nil, PendingApprovalNoCluster()...)
		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		userSignup2 := s.createAndCheckUserSignupNoMUR(false, "waitinglist2", "waitinglist2@redhat.com", nil, PendingApprovalNoCluster()...)

		// then
		s.userIsNotProvisioned(t, userSignup1)
		s.userIsNotProvisioned(t, userSignup2)

		t.Run("increment the max number of users and expect the first unapproved user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled().MaxUsersNumber(initialMurCount + 1))

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup1.Name,
				wait.UntilUserSignupHasConditions(ApprovedAutomatically()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)

			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait, s.memberAwait2)
			s.userIsNotProvisioned(t, userSignup2)

			t.Run("reset the max number and expect the second user will be provisioned as well", func(t *testing.T) {
				// when
				s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

				// then
				userSignup, err := s.hostAwait.WaitForUserSignup(userSignup2.Name,
					wait.UntilUserSignupHasConditions(ApprovedAutomatically()...),
					wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
				require.NoError(s.T(), err)

				VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait, s.memberAwait2)
			})
		})
	})
}

func (s *userSignupIntegrationTest) TestProvisionToOtherClusterWhenOneIsFull() {
	s.T().Run("set per member max number of users for both members and expect that users will be provisioned to the other member when one is full", func(t *testing.T) {
		// given

		// murList := &v1alpha1.MasterUserRecordList{}
		// err := s.hostAwait.Client.List(context.TODO(), murList, client.InNamespace(s.hostAwait.Namespace))
		// require.NoError(s.T(), err)
		// start counting member user accounts starting with 1 that we would like to add to each member

		// fmt.Printf("xListing MasterUserRecordsx\n")
		// for _, mur := range murList.Items {
		// 	fmt.Printf("found MasterUserRecord: %+v\n\n", mur)
		// 	if mur.Spec.UserAccounts[0].TargetCluster == s.memberAwait.ClusterName {
		// 		maxMember1++
		// 	} else {
		// 		maxMember2++
		// 	}
		// }

		var memberLimits []test.PerMemberClusterOption
		toolchainStatus, err := s.hostAwait.WaitForToolchainStatus(wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...))
		require.NoError(t, err)
		for _, m := range toolchainStatus.Status.Members {
			if s.memberAwait.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, test.PerMemberCluster(s.memberAwait.ClusterName, m.UserAccountCount+1))
				fmt.Printf("xmember1 max = %d\n", m.UserAccountCount+1)
			} else if s.memberAwait2.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, test.PerMemberCluster(s.memberAwait2.ClusterName, m.UserAccountCount+1))
				fmt.Printf("xmember2 max = %d\n", m.UserAccountCount+1)
			}
		}
		require.Len(s.T(), memberLimits, 2)

		userAccounts := &v1alpha1.UserAccountList{}
		err = s.memberAwait.Client.List(context.TODO(), userAccounts)
		require.NoError(s.T(), err)
		fmt.Printf("xuseraccounts1 = %d\n", len(userAccounts.Items))

		userAccounts = &v1alpha1.UserAccountList{}
		err = s.memberAwait2.Client.List(context.TODO(), userAccounts)
		require.NoError(s.T(), err)
		fmt.Printf("xuseraccounts2 = %d\n", len(userAccounts.Items))
		// maxMember2 := len(userAccounts.Items) + 1

		// if err := s.memberAwait.Client.Client.List(context.TODO(), userAccounts); err != nil {
		// 	return false, err
		// }

		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled().MaxUsersNumber(0, memberLimits...))
		// time.Sleep(30 * time.Second)
		fmt.Printf("xhostOperatorConfig = %+v\n", s.hostAwait.GetHostOperatorConfig())

		// when
		userSignup1, mur1 := s.createAndCheckUserSignup(false, "multimember-1", "multi1@redhat.com", nil, ApprovedAutomatically()...)
		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		userSignup2, mur2 := s.createAndCheckUserSignup(false, "multimember-2", "multi2@redhat.com", nil, ApprovedAutomatically()...)

		// then
		expectedCluster1 := s.memberAwait
		expectedCluster2 := s.memberAwait2
		if mur1.Spec.UserAccounts[0].TargetCluster == s.memberAwait2.ClusterName {
			expectedCluster1 = s.memberAwait2
			expectedCluster2 = s.memberAwait
		}
		t.Logf("TargetCluster1: %s", mur1.Spec.UserAccounts[0].TargetCluster)
		t.Logf("TargetCluster2: %s", mur2.Spec.UserAccounts[0].TargetCluster)
		require.Equal(s.T(), mur1.Spec.UserAccounts[0].TargetCluster, expectedCluster1.ClusterName)
		require.Equal(s.T(), mur2.Spec.UserAccounts[0].TargetCluster, expectedCluster2.ClusterName)

		// verify each usersignup is provisioned only on the cluster it was assigned
		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup1, "basic", expectedCluster1)
		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup2, "basic", expectedCluster2)

		t.Run("after both members are full then new signups won't be approved nor provisioned", func(t *testing.T) {
			// when
			userSignupPending := s.createAndCheckUserSignupNoMUR(false, "multimember-3", "multi3@redhat.com", nil, PendingApprovalNoCluster()...)

			// then
			s.userIsNotProvisioned(t, userSignupPending)
		})
	})
}

func (s *userSignupIntegrationTest) userIsNotProvisioned(t *testing.T, userSignup *v1alpha1.UserSignup) {
	s.hostAwait.CheckMasterUserRecordIsDeleted(userSignup.Spec.Username)
	currentUserSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.UserSignupStateLabelValuePending, currentUserSignup.Labels[v1alpha1.UserSignupStateLabelKey])
}

func (s *userSignupIntegrationTest) TestManualApproval() {
	s.T().Run("default approval config - manual", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval())

		t.Run("user is approved manually", func(t *testing.T) {
			// when & then
			userSignup, _ := s.createAndCheckUserSignup(true, "manual1", "manual1@redhat.com", nil, ApprovedByAdmin()...)
			assert.Equal(t, v1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
		})
		t.Run("user is not approved manually thus won't be provisioned", func(t *testing.T) {
			// when
			userSignup := s.createAndCheckUserSignupNoMUR(false, "manual2", "manual2@redhat.com", nil, PendingApproval()...)

			// then
			s.userIsNotProvisioned(t, userSignup)
		})
	})

}

func (s *userSignupIntegrationTest) TestCapacityManagementWithManualApproval() {
	// given
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

	// when & then
	s.createAndCheckUserSignup(true, "manualwithcapacity1", "manualwithcapacity1@redhat.com", nil, ApprovedByAdmin()...)

	s.T().Run("set low capacity threshold and expect that user won't provisioned even when is approved manually", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled().ResourceCapThreshold(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(true, "manualwithcapacity2", "manualwithcapacity2@redhat.com", nil, ApprovedByAdminNoCluster()...)

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait, s.memberAwait2)
		})
	})

	s.T().Run("set low max number of users and expect that user won't be provisioned even when is approved manually", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled().MaxUsersNumber(1))

		// when
		userSignup := s.createAndCheckUserSignupNoMUR(true, "manualwithcapacity3", "manualwithcapacity3@redhat.com", nil, ApprovedByAdminNoCluster()...)

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the max number and expect the user will be provisioned", func(t *testing.T) {
			// when
			s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled())

			// then
			userSignup, err := s.hostAwait.WaitForUserSignup(userSignup.Name,
				wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
				wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(s.T(), err)
			VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait, s.memberAwait2)
		})
	})

	s.T().Run("when approved and set target cluster manually, then the limits will be ignored", func(t *testing.T) {
		// given
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Disabled().ResourceCapThreshold(1).MaxUsersNumber(1))

		// when & then
		userSignup, _ := s.createAndCheckUserSignup(true, "withtargetcluster", "withtargetcluster@redhat.com", s.memberAwait, ApprovedByAdmin()...)
		assert.Equal(t, v1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[v1alpha1.UserSignupStateLabelKey])
	})
}

func (s *userSignupIntegrationTest) TestUserSignupVerificationRequired() {
	s.T().Run("automatic approval with verification required", func(t *testing.T) {
		s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval())

		t.Run("verification required set to true", func(t *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	// Create user signup
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())

	userSignup := NewUserSignup(s.T(), s.hostAwait, "reginald@alpha.com", "reginald@alpha.com")

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(ApprovedAutomatically()...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created and target cluster was set
	VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait, s.memberAwait2)
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := s.createAndCheckUserSignup(true, "paul-no-need-to-transform", "paulnoneedtotransform@hotel.com", s.memberAwait, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", s.memberAwait, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = s.createAndCheckUserSignup(true, "paul", "paulathotel@hotel.com", s.memberAwait, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-2", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the same original username but different user ID
	userSignup, _ = s.createAndCheckUserSignup(true, "paul@hotel.com", "paul@hotel.com", s.memberAwait, ApprovedByAdmin()...)
	require.Equal(s.T(), "paul-3", userSignup.Status.CompliantUsername)

	// Create another UserSignups with a forbidden prefix
	for _, prefix := range []string{"kube", "openshift", "default", "redhat", "sandbox"} {
		// prefix with hyphen
		userSignup, _ = s.createAndCheckUserSignup(true, prefix+"-paul", "paul@hotel.com", s.memberAwait, ApprovedByAdmin()...)
		require.Equal(s.T(), fmt.Sprintf("crt-%s-paul", prefix), userSignup.Status.CompliantUsername)

		// prefix without delimiter
		userSignup, _ = s.createAndCheckUserSignup(true, prefix+"paul", "paul@hotel.com", s.memberAwait, ApprovedByAdmin()...)
		require.Equal(s.T(), fmt.Sprintf("crt-%spaul", prefix), userSignup.Status.CompliantUsername)

		// prefix as a name
		userSignup, _ = s.createAndCheckUserSignup(true, prefix, "paul@hotel.com", s.memberAwait, ApprovedByAdmin()...)
		require.Equal(s.T(), fmt.Sprintf("crt-%s", prefix), userSignup.Status.CompliantUsername)
	}
}

func (s *userSignupIntegrationTest) createUserSignupVerificationRequiredAndAssertNotProvisioned() *v1alpha1.UserSignup {
	// Create a new UserSignup
	username := "testuser" + uuid.NewV4().String()
	email := username + "@test.com"
	userSignup := NewUserSignup(s.T(), s.hostAwait, username, email)
	userSignup.Spec.TargetCluster = s.memberAwait.ClusterName

	// Set approved to true
	userSignup.Spec.Approved = true

	// Set verification required
	userSignup.Spec.VerificationRequired = true

	err := s.hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(s.ctx))
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(VerificationRequired()...),
		wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = s.hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 10)).WaitForMasterUserRecord(username)
	require.Error(s.T(), err)
	return userSignup
}
