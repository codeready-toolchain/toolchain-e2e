package e2e

import (
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

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

	// when & then
	NewSignupRequest(s.Awaitilities).
		Username("automatic1").
		Email("automatic1@redhat.com").
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
		Execute(s.T())

	s.T().Run("set low capacity threshold and expect that user won't be approved nor provisioned", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).ResourceCapacityThreshold(1))

		// when
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("automatic2").
			Email("automatic2@redhat.com").
			RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
			Execute(t).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).ResourceCapacityThreshold(80))

			// then
			userSignup := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

	s.T().Run("set low max number of users and expect that user won't be approved nor provisioned but added on waiting list", func(t *testing.T) {
		// given
		toolchainStatus := hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
			wait.UntilToolchainStatusUpdatedAfter(time.Now()))
		originalMursPerDomainCount := toolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).MaxNumberOfUsers(originalMursPerDomainCount["internal"]+originalMursPerDomainCount["external"]))

		// when
		userSignup1, _ := NewSignupRequest(s.Awaitilities).
			Username("waitinglist1").
			Email("waitinglist1@redhat.com").
			RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
			Execute(t).Resources()

		// we need to sleep one second to create UserSignup with different creation time
		time.Sleep(time.Second)
		userSignup2, _ := NewSignupRequest(s.Awaitilities).
			Username("waitinglist2").
			Email("waitinglist2@redhat.com").
			RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
			Execute(t).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup1)
		s.userIsNotProvisioned(t, userSignup2)

		t.Run("increment the max number of users and expect the first unapproved user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).MaxNumberOfUsers(originalMursPerDomainCount["internal"]+originalMursPerDomainCount["external"]+1))

			// then
			userSignup := hostAwait.WaitForUserSignup(t, userSignup1.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))

			VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
			s.userIsNotProvisioned(t, userSignup2)

			t.Run("reset the max number and expect the second user will be provisioned as well", func(t *testing.T) {
				// when
				hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).MaxNumberOfUsers(1000))

				// then
				userSignup := hostAwait.WaitForUserSignup(t, userSignup2.Name,
					wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...),
					wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))

				VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
			})
		})
	})
}

func (s *userSignupIntegrationTest) TestProvisionToOtherClusterWhenOneIsFull() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()
	s.T().Run("set per member clusters max number of users for both members and expect that users will be provisioned to the other member when one is full", func(t *testing.T) {
		// given
		var memberLimits []testconfig.PerMemberClusterOptionInt
		toolchainStatus := hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(ToolchainStatusReadyAndUnreadyNotificationNotCreated()...),
			wait.UntilToolchainStatusUpdatedAfter(time.Now()))
		for _, m := range toolchainStatus.Status.Members {
			if memberAwait.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait.ClusterName, m.UserAccountCount+1))
			} else if memberAwait2.ClusterName == m.ClusterName {
				memberLimits = append(memberLimits, testconfig.PerMemberCluster(memberAwait2.ClusterName, m.UserAccountCount+1))
			}
		}
		require.Len(s.T(), memberLimits, 2)

		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).MaxNumberOfUsers(0, memberLimits...))

		// when
		_, mur1 := NewSignupRequest(s.Awaitilities).
			Username("multimember-1").
			Email("multi1@redhat.com").
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute(t).Resources()

		_, mur2 := NewSignupRequest(s.Awaitilities).
			Username("multimember-2").
			Email("multi2@redhat.com").
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
			Execute(t).Resources()

		// then
		require.NotEqual(s.T(), mur1.Spec.UserAccounts[0].TargetCluster, mur2.Spec.UserAccounts[0].TargetCluster)

		t.Run("after both members are full then new signups won't be approved nor provisioned", func(t *testing.T) {
			// when
			userSignupPending, _ := NewSignupRequest(s.Awaitilities).
				Username("multimember-3").
				Email("multi3@redhat.com").
				RequireConditions(ConditionSet(Default(), PendingApproval(), PendingApprovalNoCluster())...).
				Execute(t).Resources()

			// then
			s.userIsNotProvisioned(t, userSignupPending)
		})
	})
}

func (s *userSignupIntegrationTest) userIsNotProvisioned(t *testing.T, userSignup *toolchainv1alpha1.UserSignup) {
	hostAwait := s.Host()
	hostAwait.CheckMasterUserRecordIsDeleted(t, userSignup.Spec.Username)
	currentUserSignup := hostAwait.WaitForUserSignup(t, userSignup.Name)
	assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValuePending, currentUserSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
}

func (s *userSignupIntegrationTest) TestManualApproval() {
	hostAwait := s.Host()
	s.T().Run("default approval config - manual", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).MaxNumberOfUsers(1000).ResourceCapacityThreshold(80))

		t.Run("user is approved manually", func(t *testing.T) {
			// when & then
			userSignup, _ := NewSignupRequest(s.Awaitilities).
				Username("manual1").
				Email("manual1@redhat.com").
				ManuallyApprove().
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute(t).Resources()

			assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		})
		t.Run("user is not approved manually thus won't be provisioned", func(t *testing.T) {
			// when
			userSignup, _ := NewSignupRequest(s.Awaitilities).
				Username("manual2").
				Email("manual2@redhat.com").
				RequireConditions(ConditionSet(Default(), PendingApproval())...).
				Execute(t).Resources()

			// then
			s.userIsNotProvisioned(t, userSignup)
		})
	})

}

func (s *userSignupIntegrationTest) TestCapacityManagementWithManualApproval() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	// given
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).MaxNumberOfUsers(1000).ResourceCapacityThreshold(80))

	// when & then
	NewSignupRequest(s.Awaitilities).
		Username("manualwithcapacity1").
		Email("manualwithcapacity1@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(s.T())

	s.T().Run("set low capacity threshold and expect that user won't provisioned even when is approved manually", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).ResourceCapacityThreshold(1))

		// when
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("manualwithcapacity2").
			Email("manualwithcapacity2@redhat.com").
			ManuallyApprove().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin(), ApprovedByAdminNoCluster())...).
			Execute(t).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the threshold and expect the user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).ResourceCapacityThreshold(80))

			// then
			userSignup := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

	s.T().Run("set low max number of users and expect that user won't be provisioned even when is approved manually", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).MaxNumberOfUsers(1))

		// when
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("manualwithcapacity3").
			Email("manualwithcapacity3@redhat.com").
			ManuallyApprove().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin(), ApprovedByAdminNoCluster())...).
			Execute(t).Resources()

		// then
		s.userIsNotProvisioned(t, userSignup)

		t.Run("reset the max number and expect the user will be provisioned", func(t *testing.T) {
			// when
			hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).MaxNumberOfUsers(1000))

			// then
			userSignup := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
			VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
		})
	})

	s.T().Run("when approved and set target cluster manually, then the limits will be ignored", func(t *testing.T) {
		// given
		hostAwait.UpdateToolchainConfig(s.T(),
			testconfig.AutomaticApproval().Enabled(false).
				ResourceCapacityThreshold(1).
				MaxNumberOfUsers(1))

		// when & then
		userSignup, _ := NewSignupRequest(s.Awaitilities).
			Username("withtargetcluster").
			Email("withtargetcluster@redhat.com").
			ManuallyApprove().
			EnsureMUR().
			TargetCluster(memberAwait.ClusterName).
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(t).Resources()

		assert.Equal(t, toolchainv1alpha1.UserSignupStateLabelValueApproved, userSignup.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
	})
}

func (s *userSignupIntegrationTest) TestUserSignupVerificationRequired() {
	hostAwait := s.Host()
	s.T().Run("automatic approval with verification required", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false).MaxNumberOfUsers(1000).ResourceCapacityThreshold(80))

		t.Run("verification required set to true", func(t *testing.T) {
			s.createUserSignupVerificationRequiredAndAssertNotProvisioned()
		})
	})
}

func (s *userSignupIntegrationTest) TestTargetClusterSelectedAutomatically() {
	hostAwait := s.Host()
	// Create user signup
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true).MaxNumberOfUsers(1000).ResourceCapacityThreshold(80))

	userSignup := NewUserSignup(hostAwait.Namespace, "reginald@alpha.com", "reginald@alpha.com")
	hostAwait.CreateWithCleanup(s.T(), userSignup)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is approved now
	userSignup = hostAwait.WaitForUserSignup(s.T(), userSignup.Name, wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedAutomatically())...))

	// Confirm the MUR was created and target cluster was set
	VerifyResourcesProvisionedForSignup(s.T(), s.Awaitilities, userSignup, "deactivate30", "base")
}

func (s *userSignupIntegrationTest) TestTransformUsername() {
	// Create UserSignup with a username that we don't need to transform
	userSignup, _ := NewSignupRequest(s.Awaitilities).
		Username("paul-no-need-to-transform").
		Email("paulnoneedtotransform@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "paul-no-need-to-transform", userSignup.Status.CompliantUsername)

	// Create UserSignup with a username to transform
	userSignup, _ = NewSignupRequest(s.Awaitilities).
		Username("paul@hotel.com").
		Email("paul@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(s.T()).Resources()

	require.Equal(s.T(), "paul", userSignup.Status.CompliantUsername)

	// Create another UserSignup with the original username matching the transformed username of the existing signup
	userSignup, _ = NewSignupRequest(s.Awaitilities).
		Username("paul").
		Email("paulathotel@hotel.com").
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
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
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%s-paul", prefix), userSignup.Status.CompliantUsername)

		// prefix without delimiter
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(prefix + "paul").
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("crt-%spaul", prefix), userSignup.Status.CompliantUsername)

		// prefix as a name
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(prefix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
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
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("paul-%s-crt", suffix), userSignup.Status.CompliantUsername)

		// suffix without delimiter
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username("paul" + suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("paul%s-crt", suffix), userSignup.Status.CompliantUsername)

		// suffix as a name
		userSignup, _ = NewSignupRequest(s.Awaitilities).
			Username(suffix).
			Email("paul@hotel.com").
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute(s.T()).Resources()

		require.Equal(s.T(), fmt.Sprintf("%s-crt", suffix), userSignup.Status.CompliantUsername)
	}
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

	hostAwait.CreateWithCleanup(s.T(), userSignup)
	s.T().Logf("user signup '%s' created", userSignup.Name)

	// Check the UserSignup is pending approval now
	userSignup = hostAwait.WaitForUserSignup(s.T(), userSignup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))

	// Confirm the CompliantUsername has NOT been set
	require.Empty(s.T(), userSignup.Status.CompliantUsername)

	// Confirm that a MasterUserRecord wasn't created
	hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*10)).WaitForMasterUserRecord(s.T(), username)
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
		RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
		Execute(s.T()).
		Resources()

	// then

	// annotation should be set
	require.True(s.T(), userSignup.Annotations[toolchainv1alpha1.SkipAutoCreateSpaceAnnotationKey] == "true")

	VerifyResourcesProvisionedForSignupWithoutSpace(s.T(), s.Awaitilities, userSignup, "deactivate30")
}
