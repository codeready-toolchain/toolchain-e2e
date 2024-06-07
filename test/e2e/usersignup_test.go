package e2e

import (
	"testing"
	"time"

	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testSpc "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/google/uuid"
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

	// let's also create a not-ready ToolchainCluster CR and make sure it doesn't get picked in any of the tests below...
	// We also create an enabled SpaceProvisionerConfig with enough capacity to try to "lure" the user signups into it.
	// It should have no effect though, because the unusable cluster is not ready.
	unusableTCName := util.NewObjectNamePrefix(s.T()) + uuid.NewString()[0:20]
	wait.CopyWithCleanup(s.T(), hostAwait.Awaitility,
		client.ObjectKey{
			Name:      memberAwait1.ClusterName,
			Namespace: hostAwait.Namespace,
		},
		client.ObjectKey{
			Name:      unusableTCName,
			Namespace: hostAwait.Namespace,
		},
		&toolchainv1alpha1.ToolchainCluster{},
		func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Spec.SecretRef.Name = ""
			tc.Status = toolchainv1alpha1.ToolchainClusterStatus{}
		},
	)
	spaceprovisionerconfig.CreateSpaceProvisionerConfig(s.T(), hostAwait.Awaitility,
		testSpc.ReferencingToolchainCluster(unusableTCName),
		testSpc.Enabled(true),
		testSpc.MaxNumberOfSpaces(1000),
		testSpc.MaxMemoryUtilizationPercent(100))

	// when & then
	_, mur1 := NewSignupRequest(s.Awaitilities).
		Username("automatic1").
		Email("automatic1@redhat.com").
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
		Execute(s.T()).Resources()

	s.T().Run("set low max number of spaces and expect that space won't be approved nor provisioned but added on waiting list", func(t *testing.T) {
		// given
		// update max number of spaces to current number of spaces provisioned
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(1))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))
		// create additional user to reach max space limits on both members
		_, mur2 := NewSignupRequest(s.Awaitilities).
			Username("automatic2").
			Email("automatic2@redhat.com").
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...).
			Execute(s.T()).Resources()

		// TestProvisionToOtherClusterWhenOneIsFull
		// checks that users will be provisioned to the other member when one is full.
		// if one cluster if full, then a new space will be placed in the another cluster.
		require.NotEqual(t, mur1.Status.UserAccounts[0].Cluster.Name, mur2.Status.UserAccounts[0].Cluster.Name)
		space1, err := hostAwait.WaitForSpace(t, mur1.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		require.NoError(t, err)
		space2, err := hostAwait.WaitForSpace(t, mur2.Name, wait.UntilSpaceHasAnyTargetClusterSet())
		require.NoError(t, err)
		require.NotEqual(t, space1.Spec.TargetCluster, space2.Spec.TargetCluster)

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
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(2))
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(1))
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, waitingList1.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)

			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
			s.userIsNotProvisioned(t, waitlinglist2)

			t.Run("reset the max number of spaces and expect the second user will be provisioned as well", func(t *testing.T) {
				// when
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500))
				spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(500))
				hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))

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
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(1))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))

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
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(80))
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(80))
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

	s.T().Run("add user not matching domains, expect that space won't be approved nor provisioned but added on waiting list", func(t *testing.T) {
		domains := "anotherdomain.edu"
		// when
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true).Domains(domains), testconfig.RegistrationService().Verification().Enabled(false))
		VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasAutoApprovalDomains(domains), wait.UntilToolchainConfigHasVerificationEnabled(false))

		// and
		waitingList3, _ := NewSignupRequest(s.Awaitilities).
			Username("waitinglist3").
			Email("waitinglist3@redhat.com").
			RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...).
			Execute(s.T()).Resources()

		// then
		s.userIsNotProvisioned(t, waitingList3)

		t.Run("add matching domain to domains and expect the user will be provisioned", func(t *testing.T) {
			domains := "anotherdomain.edu,redhat.com"
			// when
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true).Domains(domains))
			VerifyToolchainConfig(t, hostAwait, wait.UntilToolchainConfigHasAutoApprovalDomains(domains))

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, waitingList3.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, s.Awaitilities, userSignup, "deactivate30", "base")
		})

		t.Run("add user with bad email format and expect the user will not be approved nor provisioned", func(t *testing.T) {
			msg := "unable to determine automatic approval: invalid email address: waitinglist4@somedomain.org@anotherdomain.com"
			// when
			waitingList4, _ := NewSignupRequest(s.Awaitilities).
				Username("waitinglist4").
				Email("waitinglist4@somedomain.org@anotherdomain.com").
				RequireConditions(wait.ConditionSet(wait.Default(), wait.PendingApprovalWithMsg(msg), wait.PendingApprovalNoClusterWithMsg(msg))...).
				Execute(s.T()).Resources()

			// then
			s.userIsNotProvisioned(t, waitingList4)
		})
	})
}

func (s *userSignupIntegrationTest) TestProvisionToOtherClusterWhenOneIsFull() {
	hostAwait := s.Host()
	memberAwait1 := s.Member1()
	memberAwait2 := s.Member2()
	s.T().Run("set per member clusters max number of users for both members and expect that users will be provisioned to the other member when one is full", func(t *testing.T) {
		// given
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(1))

		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(true))
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

	id := uuid.New()

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

	NewHTTPRequest(s.T()).InvokeEndpoint("GET", hostAwait.RegistrationServiceURL+"/api/v1/signup", token, "", 200)

	// Reload the UserSignup
	userSignup, err = hostAwait.WaitForUserSignupByUserIDAndUsername(s.T(), userIdentity.ID.String(), userIdentity.Username)
	require.NoError(s.T(), err)

	// Confirm the IdentityClaims properties have been updated
	require.Equal(s.T(), "test-user-updated@redhat.com", userSignup.Spec.IdentityClaims.Email)
	require.Equal(s.T(), "updated-original-sub", userSignup.Spec.IdentityClaims.OriginalSub)
	require.Equal(s.T(), "111222", userSignup.Spec.IdentityClaims.UserID)
	require.Equal(s.T(), "999111", userSignup.Spec.IdentityClaims.AccountID)
	require.Equal(s.T(), "Jane", userSignup.Spec.IdentityClaims.GivenName)
	require.Equal(s.T(), "Turner", userSignup.Spec.IdentityClaims.FamilyName)
	require.Equal(s.T(), "Acme", userSignup.Spec.IdentityClaims.Company)
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
	// TestUserIDAndAccountIDClaimsPropagated and TestUserResourcesCreatedWhenUserIDIsSet
	// 1. sub claim is generated automatically
	// 2. user ID and account ID are set by test
	// 3. no original sub claim is set
	// This scenario is expected with the regular RHD SSO client
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
	identityID := uuid.New()

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
	hostAwait.CheckMasterUserRecordIsDeleted(t, userSignup.Spec.IdentityClaims.PreferredUsername)
	currentUserSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name)
	require.NoError(t, err)
	assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValuePending, currentUserSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
}

func (s *userSignupIntegrationTest) TestManualApproval() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	s.T().Run("default approval config - manual", func(t *testing.T) {
		// given
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait.ClusterName, testSpc.MaxNumberOfSpaces(1000), testSpc.MaxMemoryUtilizationPercent(80))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

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
	spaceprovisionerconfig.UpdateForCluster(s.T(), hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500), testSpc.MaxMemoryUtilizationPercent(80))
	spaceprovisionerconfig.UpdateForCluster(s.T(), hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(500), testSpc.MaxMemoryUtilizationPercent(80))
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false))

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
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(1))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
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
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxNumberOfSpaces(500))
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxNumberOfSpaces(500))
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

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
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(1))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

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
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(80))
			spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(80))
			hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

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
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait1.ClusterName, testSpc.MaxMemoryUtilizationPercent(1), testSpc.MaxNumberOfSpaces(1))
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait2.ClusterName, testSpc.MaxMemoryUtilizationPercent(1), testSpc.MaxNumberOfSpaces(1))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

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
		spaceprovisionerconfig.UpdateForCluster(t, hostAwait.Awaitility, memberAwait.ClusterName, testSpc.MaxMemoryUtilizationPercent(80), testSpc.MaxNumberOfSpaces(1000))
		hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))

		t.Run("verification required set to true", func(_ *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	// Create user signup
	spaceprovisionerconfig.UpdateForCluster(s.T(), hostAwait.Awaitility, memberAwait.ClusterName, testSpc.MaxMemoryUtilizationPercent(80), testSpc.MaxNumberOfSpaces(1000))
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

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

func (s *userSignupIntegrationTest) createUserSignupVerificationRequiredAndAssertNotProvisioned() *toolchainv1alpha1.UserSignup {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	// Create a new UserSignup
	username := "testuser" + uuid.NewString()
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
	require.Equal(s.T(), "true", userSignup.Annotations[toolchainv1alpha1.SkipAutoCreateSpaceAnnotationKey])

	VerifyResourcesProvisionedForSignupWithoutSpace(s.T(), s.Awaitilities, userSignup, "deactivate30")
}
