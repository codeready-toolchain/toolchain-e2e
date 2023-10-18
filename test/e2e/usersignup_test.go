package e2e

import (
	"fmt"
	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testcommonspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/redhat-cop/operator-utils/pkg/util"
	v1 "k8s.io/api/core/v1"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type userSignupIntegrationTest struct {
	suite.Suite
	wait.Awaitilities
}

func TestRunUserSignupIntegrationTest(t *testing.T) {
	suite.Run(t, &userSignupIntegrationTest{})
}

func (s *userSignupIntegrationTest) SetupSuite() {
	s.Awaitilities = WaitForDeployments(s.T())
}

func (s *userSignupIntegrationTest) TearDownTest() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()
	hostAwait.Clean(s.T())
	memberAwait.Clean(s.T())
	memberAwait2.Clean(s.T())
}

func (s *userSignupIntegrationTest) TestAutomaticApproval() {
	// given
	hostAwait := s.Host()
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))
	memberAwait1 := s.Member1()
	memberAwait2 := s.Member2()

	// when & then
	NewSignupRequest(s.Awaitilities).
		Username("automatic1").
		Email("automatic1@redhat.com").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T())

	s.T().Run("set low max number of spaces and expect that space won't be approved nor provisioned but added on waiting list", func(t *testing.T) {
		// given
		// update max number of spaces to current number of spaces provisioned
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true),
			testconfig.CapacityThresholds().
				MaxNumberOfSpaces(
					testconfig.PerMemberCluster(memberAwait1.ClusterName, 1),
					testconfig.PerMemberCluster(memberAwait2.ClusterName, 1),
				),
		)
		// create additional user to reach max space limits on both members
		NewSignupRequest(s.Awaitilities).
			Username("automatic2").
			Email("automatic2@redhat.com").
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
			Execute(s.T())

		// when
		waitingList1, _ := NewSignupRequest(s.Awaitilities).
			Username("waitinglist1").
			Email("waitinglist1@redhat.com").
			RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval(), wait.PendingApprovalNoCluster())...).
			Execute(s.T()).Resources()

		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		waitlinglist2, _ := NewSignupRequest(s.Awaitilities).
			Username("waitinglist2").
			Email("waitinglist2@redhat.com").
			RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval(), wait.PendingApprovalNoCluster())...).
			Execute(s.T()).Resources()

		// then
		s.userIsNotProvisioned(t, waitingList1)
		s.userIsNotProvisioned(t, waitlinglist2)

		t.Run("increment the max number of spaces and expect the first unapproved user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true),
				testconfig.CapacityThresholds().
					MaxNumberOfSpaces(
						testconfig.PerMemberCluster(memberAwait1.ClusterName, 2),
						testconfig.PerMemberCluster(memberAwait2.ClusterName, 1),
					),
			)

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, waitingList1.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)

			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
			s.userIsNotProvisioned(t, waitlinglist2)

			t.Run("reset the max number of spaces and expect the second user will be provisioned as well", func(t *testing.T) {
				// when
				hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true),
					testconfig.CapacityThresholds().
						MaxNumberOfSpaces(
							testconfig.PerMemberCluster(memberAwait1.ClusterName, 500),
							testconfig.PerMemberCluster(memberAwait2.ClusterName, 500),
						),
				)

				// then
				userSignup, err := hostAwait.WaitForUserSignup(t, waitlinglist2.Name,
					wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
					wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
				require.NoError(t, err)

				VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
			})
		})
	})

	s.T().Run("set low capacity threshold and expect that user won't be approved nor provisioned", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t,
			testconfig.AutomaticApproval().Enabled(true),
			testconfig.CapacityThresholds().ResourceCapacityThreshold(1),
		)

		// when
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("automatic3").
			Email("automatic3@redhat.com").
			RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval(), wait.PendingApprovalNoCluster())...).
			Execute(t).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(t,
				testconfig.AutomaticApproval().Enabled(true),
				testconfig.CapacityThresholds().ResourceCapacityThreshold(80),
			)

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

}

func (s *userSignupIntegrationTest) TestProvisionToOtherClusterWhenOneIsFull() {
	hostAwait := s.Host()
	memberAwait1 := s.Member1()
	memberAwait2 := s.Member2()
	s.T().Run("set per member clusters max number of users for both members and expect that users will be provisioned to the other member when one is full", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true),
			testconfig.CapacityThresholds().MaxNumberOfSpaces(
				testconfig.PerMemberCluster(memberAwait1.ClusterName, 1),
				testconfig.PerMemberCluster(memberAwait2.ClusterName, 1),
			))

		// when
		_, mur1 := NewSignupRequest(s.Awaitilities).
			Username("multimember-1").
			Email("multi1@redhat.com").
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
			Execute(s.T()).Resources()

		_, mur2 := NewSignupRequest(s.Awaitilities).
			Username("multimember-2").
			Email("multi2@redhat.com").
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
			Execute(s.T()).Resources()

		// then
		require.NotEqual(t, mur1.Status.UserAccounts[0].Cluster.Name, mur2.Status.UserAccounts[0].Cluster.Name)
		space1, err := hostAwait.WaitForSpace(t, mur1.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		require.NoError(t, err)
		space2, err := hostAwait.WaitForSpace(t, mur2.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		require.NoError(t, err)
		require.NotEqual(t, space1.Spec.TargetCluster, space2.Spec.TargetCluster)

		t.Run("after both members are full then new signups won't be approved nor provisioned", func(t *testing.T) {
			// when
			userSignupPending, _ := NewSignupRequest(s.Awaitilities).
				Username("multimember-3").
				Email("multi3@redhat.com").
				RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval(), wait.PendingApprovalNoCluster())...).
				Execute(s.T()).Resources()

			// then
			s.userIsNotProvisioned(t, userSignupPending)
		})
	})
}

func (s *userSignupIntegrationTest) TestUserIDAndAccountIDClaimsPropagated() {
	hostAwait := s.Host()

	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("test-user").
		Email("test-user@redhat.com").
		UserID("123456789").
		AccountID("987654321").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	// then
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

func (s *userSignupIntegrationTest) TestGetSignupEndpointUpdatesIdentityClaims() {
	hostAwait := s.Host()

	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	id, err := uuid.NewV4()
	require.NoError(s.T(), err)

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("test-user-identityclaims").
		Email("test-user-identityclaims@redhat.com").
		IdentityID(id).
		UserID("000999").
		AccountID("111999").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	// then
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")

	// Create a token and identity to invoke the GetSignup endpoint with
	userIdentity := &commonauth.Identity{
		ID:       id,
		Username: "test-user-identityclaims",
	}
	claims := []commonauth.ExtraClaim{commonauth.WithEmailClaim("test-user-updated@redhat.com")}
	claims = append(claims, commonauth.WithOriginalSubClaim("updated-original-sub"))
	claims = append(claims, commonauth.WithUserIDClaim("111222"))
	claims = append(claims, commonauth.WithAccountIDClaim("999111"))
	claims = append(claims, commonauth.WithGivenNameClaim("Jane"))
	claims = append(claims, commonauth.WithFamilyNameClaim("Turner"))
	claims = append(claims, commonauth.WithCompanyClaim("Acme"))

	token, err := authsupport.NewTokenFromIdentity(userIdentity, claims...)
	require.NoError(s.T(), err)

	InvokeEndpoint(s.T(), "GET", hostAwait.RegistrationServiceURL+"/api/v1/signup", token, "", 200)

	// Reload the UserSignup
	userSignup, err = hostAwait.WaitForUserSignupByUserIDAndUsername(s.T(), userIdentity.ID.String(), userIdentity.Username)
	require.NoError(s.T(), err)

	// Confirm the IdentityClaims properties have been updated
	require.Equal(s.T(), "test-user-updated@redhat.com", userSignup.Spec.IdentityClaims.Email)
	require.Equal(s.T(), "updated-original-sub", userSignup.Spec.IdentityClaims.OriginalSub)
	require.Equal(s.T(), "111222", userSignup.Spec.IdentityClaims.UserID)
	require.Equal(s.T(), "999111", userSignup.Spec.IdentityClaims.AccountID)
	require.Equal(s.T(), "test-user-updated", userSignup.Spec.IdentityClaims.PreferredUsername)
	require.Equal(s.T(), "Jane", userSignup.Spec.IdentityClaims.GivenName)
	require.Equal(s.T(), "Turner", userSignup.Spec.IdentityClaims.FamilyName)
	require.Equal(s.T(), "Acme", userSignup.Spec.IdentityClaims.Company)

}

// TestUserResourcesCreatedWhenUserIDIsSet tests the case where:
//
// 1. sub claim is generated automatically
// 2. user ID is set by test
// 3. no original sub claim is set
//
// This scenario is expected with the regular RHD SSO client
func (s *userSignupIntegrationTest) TestUserResourcesCreatedWhenUserIDIsSet() {
	hostAwait := s.Host()

	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("test-user-with-userid").
		Email("test-user-with-userid@redhat.com").
		UserID("111222333").
		AccountID("jnwww029837").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	// then
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

// TestUserResourcesCreatedWhenOriginalSubIsSet tests the case where:
//
// 1. sub claim is generated automatically
// 2. user id is not set
// 3. original sub claim set
func (s *userSignupIntegrationTest) TestUserResourcesCreatedWhenOriginalSubIsSet() {
	hostAwait := s.Host()

	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("test-user-with-originalsub").
		Email("test-user-with-originalsub@redhat.com").
		OriginalSub("abc:fff000111-bbbccc").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	// then
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

func (s *userSignupIntegrationTest) TestUserResourcesUpdatedWhenPropagatedClaimsModified() {
	hostAwait := s.Host()

	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("test-user-resources-updated").
		Email("test-user-resources-updated@redhat.com").
		UserID("43215432").
		AccountID("ppqnnn00099").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	// then
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")

	// Update the UserSignup
	userSignup, err := hostAwait.UpdateUserSignup(s.T(), userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		// Modify the user's AccountID
		us.Spec.IdentityClaims.AccountID = "nnnbbb111234"
	})
	require.NoError(s.T(), err)

	// Confirm the AccountID is updated
	require.Equal(s.T(), "nnnbbb111234", userSignup.Spec.IdentityClaims.AccountID)

	// Verify that the resources are updated with the propagated claim
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

// TestUserResourcesCreatedWhenOriginalSubIsSetAndUserIDSameAsSub tests the case where:
//
// 1. sub claim set manually by test
// 2. user id set by test and equal to sub claim
// 3. original sub is set to a different value
//
// This scenario is expected when using the current sandbox RHD SSO client
func (s *userSignupIntegrationTest) TestUserResourcesCreatedWhenOriginalSubIsSetAndUserIDSameAsSub() {
	hostAwait := s.Host()

	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	// generate a new identity ID
	identityID := uuid.Must(uuid.NewV4())

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("test-user-with-userid-and-originalsub").
		Email("test-user-with-userid-and-originalsub@redhat.com").
		IdentityID(identityID).
		UserID(identityID.String()).
		AccountID("abc-8783").
		OriginalSub("def:98734987234").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	// then
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

func (s *userSignupIntegrationTest) userIsNotProvisioned(t *testing.T, userSignup *toolchainv1alpha1.UserSignup) {
	hostAwait := s.Host()
	hostAwait.CheckMasterUserRecordIsDeleted(t, userSignup.Spec.Username)
	currentUserSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name)
	require.NoError(t, err)
	assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValuePending, currentUserSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
}

func (s *userSignupIntegrationTest) TestManualApproval() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	s.T().Run("default approval config - manual", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false),
			testconfig.
				CapacityThresholds().
				MaxNumberOfSpaces(
					testconfig.PerMemberCluster(memberAwait.ClusterName, 1000),
				).
				ResourceCapacityThreshold(80))

		t.Run("user is approved manually", func(t *testing.T) {
			// when & then
			userSignup, _ := NewSignupRequest(s.Awaitilities).
				Username("manual1").
				Email("manual1@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
				Execute(s.T()).Resources()

			assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		})
		t.Run("user is not approved manually thus won't be provisioned", func(t *testing.T) {
			// when
			userSignup, _ := NewSignupRequest(s.Awaitilities).
				Username("manual2").
				Email("manual2@redhat.com").
				RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...).
				Execute(s.T()).Resources()

			// then
			s.userIsNotProvisioned(t, userSignup)
		})
	})

}

func (s *userSignupIntegrationTest) TestCapacityManagementWithManualApproval() {
	hostAwait := s.Host()
	memberAwait1 := s.Member1()
	memberAwait2 := s.Member2()
	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false),
		testconfig.CapacityThresholds().
			MaxNumberOfSpaces(
				testconfig.PerMemberCluster(memberAwait1.ClusterName, 500),
				testconfig.PerMemberCluster(memberAwait2.ClusterName, 500),
			).
			ResourceCapacityThreshold(80))

	// when & then
	NewSignupRequest(s.Awaitilities).
		Username("manualwithcapacity1").
		Email("manualwithcapacity1@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T())

	s.T().Run("set low max number of spaces and expect that user won't be provisioned even when is approved manually", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false),
			testconfig.CapacityThresholds().
				MaxNumberOfSpaces(
					testconfig.PerMemberCluster(memberAwait1.ClusterName, 1),
					testconfig.PerMemberCluster(memberAwait2.ClusterName, 1),
				),
		)
		// create usersignup to reach max number of spaces on both members
		NewSignupRequest(s.Awaitilities).
			Username("manualwithcapacity2").
			Email("manualwithcapacity2@redhat.com").
			ManuallyApprove().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		// when
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("manualwithcapacity3").
			Email("manualwithcapacity3@redhat.com").
			ManuallyApprove().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.ApprovedByAdminNoCluster())...).
			Execute(s.T()).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the max number and expect the user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false),
				testconfig.CapacityThresholds().
					MaxNumberOfSpaces(
						testconfig.PerMemberCluster(memberAwait1.ClusterName, 500),
						testconfig.PerMemberCluster(memberAwait2.ClusterName, 500),
					),
			)

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

	s.T().Run("set low capacity threshold and expect that user won't provisioned even when is approved manually", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t,
			testconfig.AutomaticApproval().Enabled(false),
			testconfig.CapacityThresholds().ResourceCapacityThreshold(1),
		)

		// when
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("manualwithcapacity4").
			Email("manualwithcapacity4@redhat.com").
			ManuallyApprove().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.ApprovedByAdminNoCluster())...).
			Execute(s.T()).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(t,
				testconfig.AutomaticApproval().Enabled(false),
				testconfig.CapacityThresholds().ResourceCapacityThreshold(80),
			)

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

	s.T().Run("when approved and set target cluster manually, then the limits will be ignored", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(t,
			testconfig.AutomaticApproval().Enabled(false),
			testconfig.CapacityThresholds().
				ResourceCapacityThreshold(1).
				MaxNumberOfSpaces(
					testconfig.PerMemberCluster(memberAwait1.ClusterName, 1),
					testconfig.PerMemberCluster(memberAwait2.ClusterName, 1),
				),
		)

		// when & then
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("withtargetcluster").
			Email("withtargetcluster@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait1).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
	})
}

func (s *userSignupIntegrationTest) TestUserSignupVerificationRequired() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	s.T().Run("automatic approval with verification required", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false),
			testconfig.CapacityThresholds().MaxNumberOfSpaces(testconfig.PerMemberCluster(memberAwait.ClusterName, 1000)).
				ResourceCapacityThreshold(80))

		t.Run("verification required set to true", func(t *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	// Create user signup
	hostAwait.UpdateToolchainConfig(s.T(),
		testconfig.AutomaticApproval().Enabled(true),
		testconfig.CapacityThresholds().
			MaxNumberOfSpaces(
				testconfig.PerMemberCluster(memberAwait.ClusterName, 1000),
			).
			ResourceCapacityThreshold(80))

	userSignup := NewUserSignup(hostAwait.Namespace, "reginald@alpha.com", "reginald@alpha.com")
	err := hostAwait.CreateWithCleanup(s.T(), userSignup)
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name, wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...))
	require.NoError(s.T(), err)

	// Confirm the MUR was created and target cluster was set
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

func (s *userSignupIntegrationTest) TestTransformUsernameWithSpaceConflict() {
	// given
	conflictingSpace, _, _ := CreateSpace(s.T(), s.Awaitilities, testcommonspace.WithName("conflicting"))

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username(conflictingSpace.Name).
		TargetCluster(s.Member1()).
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	// then
	compliantUsername := userSignup.Status.CompliantUsername
	require.Equal(s.T(), fmt.Sprintf("%s-2", conflictingSpace.Name), compliantUsername)

	s.T().Run("when signup is deactivated, Space is stuck in terminating state, and when it reactivates then it should generate a new name", func(t *testing.T) {
		// given
		// let's get a namespace of the space
		space, err := s.Host().WaitForSpace(t, compliantUsername, wait.UntilSpaceHasAnyProvisionedNamespaces())
		require.NoError(t, err)
		namespaceName := space.Status.ProvisionedNamespaces[0].Name
		// and add a dummy finalizer there so it will get stuck
		_, err = s.Member1().UpdateNamespace(t, namespaceName, func(ns *v1.Namespace) {
			util.AddFinalizer(ns, "test/finalizer.toolchain.e2e.tests")
		})
		require.NoError(t, err)

		// don't forget to clean the finalizer up
		defer func() {
			t.Log("cleaning up the finalizer")
			_, err = s.Member1().UpdateNamespace(t, namespaceName, func(ns *v1.Namespace) {
				util.RemoveFinalizer(ns, "test/finalizer.toolchain.e2e.tests")
			})
			require.NoError(t, err)
		}()

		// now deactivate the usersignup
		_, err = s.Host().UpdateUserSignup(t, userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})
		require.NoError(t, err)

		// wait until it is deactivated, SpaceBinding is gone, and Space is in terminating state
		_, err = s.Host().WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)
		err = s.Host().WaitUntilSpaceBindingsWithLabelDeleted(t, toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey, compliantUsername)
		require.NoError(t, err)
		_, err = s.Host().WaitForSpace(t, compliantUsername, wait.UntilSpaceIsBeingDeleted())
		require.NoError(t, err)

		// when
		userSignup, err = s.Host().UpdateUserSignup(t, userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
			states.SetApprovedManually(us, true)
		})
		require.NoError(t, err)

		// then
		userSignup, _ = VerifyUserRelatedResources(t, s.Awaitilities, userSignup, "deactivate30", ExpectAnyUserAccount())
		VerifySpaceRelatedResources(t, s.Awaitilities, userSignup, "base")
		VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
		require.Equal(t, fmt.Sprintf("%s-3", conflictingSpace.Name), userSignup.Status.CompliantUsername)
	})
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("paul-no-transform").
		Email("paulnotransform@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "paul-no-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = NewSignupRequest(s.Awaitilities).
		Username("paul@hotel.com").
		Email("paul@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = NewSignupRequest(s.Awaitilities).
		Username("paul").
		Email("paulathotel@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "paul-2", userSignup.Status.CompliantUsername)

	// Create another UserSignups with a forbidden prefix
	for _, prefix := range []string{"kube", "openshift", "default", "redhat", "sandbox"} {
		// prefix with hyphen
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(prefix + "-paul").
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%s-paul", prefix), userSignup.Status.CompliantUsername)

		// prefix without delimiter
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(prefix + "paul").
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%spaul", prefix), userSignup.Status.CompliantUsername)

		// prefix as a name
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(prefix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%s", prefix), userSignup.Status.CompliantUsername)
	}

	// Create another UserSignups with a forbidden suffix
	for _, suffix := range []string{"admin"} {
		// suffix with hyphen
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username("paul-" + suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("paul-%s-crt", suffix), userSignup.Status.CompliantUsername)

		// suffix without delimiter
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username("paul" + suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("paul%s-crt", suffix), userSignup.Status.CompliantUsername)

		// suffix as a name
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("%s-crt", suffix), userSignup.Status.CompliantUsername)
	}

	// create a usersignup where the length of the username is greater than 20 chars, and is transformed by truncating
	userSignup, _ = NewSignupRequest(s.Awaitilities).
		Username("username-greater-than-20").
		Email("paulathotel@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "username-greater-tha", userSignup.Status.CompliantUsername)

	// create a usersignup where the name is greater than 20 chars, but when truncating the username it has a forbidden suffix. Check the compliant username is replacing the suffix, instead of adding
	userSignup, _ = NewSignupRequest(s.Awaitilities).
		Username("username-with-admin-more-than-20-chars").
		Email("paulathotel@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "username-with-ad-crt", userSignup.Status.CompliantUsername)
}

func (s *userSignupIntegrationTest) createUserSignupVerificationRequiredAndAssertNotProvisioned() *toolchainv1alpha1.UserSignup {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	// Create a new UserSignup
	username := "testuser" + uuid.Must(uuid.NewV4()).String()
	email := username + "@test.com"
	userSignup := NewUserSignup(hostAwait.Namespace, username, email)
	userSignup.Spec.TargetCluster = memberAwait.ClusterName

	// Set approved to true
	states.SetApprovedManually(userSignup, true)

	// Set verification required
	states.SetVerificationRequired(userSignup, true)

	err := hostAwait.CreateWithCleanup(s.T(), userSignup)
	require.NoError(s.T(), err)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup, err = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForMasterUserRecord(s.T(), username)
	require.Error(s.T(), err)
	return userSignup
}

func (s *userSignupIntegrationTest) TestSkipSpaceCreation() {
	// given
	hostAwait := s.Host()
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	// when
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("nospace").
		Email("nospace@redhat.com").
		NoSpace().
		WaitForMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).
		Resources()

	// then

	// annotation should be set
	require.True(s.T(), userSignup.Annotations[toolchainv1alpha1.SkipAutoCreateSpaceAnnotationKey] == "true")

	VerifyResourcesProvisionedForSignupWithoutSpace(s.T(), s.Awaitilities, userSignup, "deactivate30")
}
