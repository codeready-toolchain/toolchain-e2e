package migration

import (
	"context"
	"sync"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	test "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

const (
	ProvisionedUser             = "migration-provisioned"
	DeactivatedUser             = "migration-deactivated"
	BannedUser                  = "migration-banned-provisioned"
	AppStudioProvisionedUser    = "migration-appstudio-provisioned"
	SecondMemberProvisionedUser = "migration-second-member-provisioned-user"

	ProvisionedAppStudioSpace    = "migration-appstudio-provisioned-space"
	SecondMemberProvisionedSpace = "migration-second-member-provisioned-space"
)

type SetupMigrationRunner struct {
	Awaitilities wait.Awaitilities
	WithCleanup  bool
}

func (r *SetupMigrationRunner) Run(t *testing.T) {
	var wg sync.WaitGroup

	toRun := []func(t *testing.T){
		r.prepareAppStudioProvisionedSpace,
		r.prepareSecondMemberProvisionedSpace,
		r.prepareProvisionedUser,
		r.prepareSecondMemberProvisionedUser,
		r.prepareDeactivatedUser,
		r.prepareBannedUser,
		r.prepareAppStudioProvisionedUser}

	for _, funcToRun := range toRun {
		wg.Add(1)
		go func(t *testing.T, run func(t *testing.T)) {
			defer wg.Done()
			run(t)
		}(t, funcToRun)
	}

	wg.Wait()
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedSpace(t *testing.T) {
	r.createAndWaitForSpace(t, ProvisionedAppStudioSpace, "appstudio", r.Awaitilities.Member1().ClusterName)
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedSpace(t *testing.T) {
	r.createAndWaitForSpace(t, SecondMemberProvisionedSpace, "base", r.Awaitilities.Member2().ClusterName)
}

func (r *SetupMigrationRunner) createAndWaitForSpace(t *testing.T, name, tierName string, targetCluster string) {
	hostAwait := r.Awaitilities.Host()
	space := test.NewSpace(t, r.Awaitilities.Host().Namespace, test.WithName(name), test.WithTierName(tierName), test.WithTargetCluster(targetCluster))
	err := hostAwait.Client.Create(context.TODO(), space)
	require.NoError(t, err)

	test.CreateMurWithAdminSpaceBindingForSpace(t, r.Awaitilities, space, false)

	hostAwait.WaitForSpace(t, space.Name,
		wait.UntilSpaceHasConditions(test.Provisioned()))
	if r.WithCleanup {
		cleanup.AddCleanTasks(t, r.Awaitilities.Host(), space)
	}
}

func (r *SetupMigrationRunner) prepareProvisionedUser(t *testing.T) {
	r.prepareUser(t, ProvisionedUser, r.Awaitilities.Member1().ClusterName)
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedUser(t *testing.T) {
	r.prepareUser(t, SecondMemberProvisionedUser, r.Awaitilities.Member2().ClusterName)
}

func (r *SetupMigrationRunner) prepareDeactivatedUser(t *testing.T) {
	userSignup := r.prepareUser(t, DeactivatedUser, r.Awaitilities.Member1().ClusterName)
	hostAwait := r.Awaitilities.Host()

	// deactivate the UserSignup
	userSignup = hostAwait.UpdateUserSignup(t, userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		states.SetDeactivated(us, true)
	})
	t.Logf("user signup '%s' set to deactivated", userSignup.Name)

	// verify that MUR is deleted
	hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, userSignup.Status.CompliantUsername) // TODO wait for space deletion too after Space migration is done
}

func (r *SetupMigrationRunner) prepareBannedUser(t *testing.T) {
	userSignup := r.prepareUser(t, BannedUser, r.Awaitilities.Member1().ClusterName)
	hostAwait := r.Awaitilities.Host()

	// Create the BannedUser
	bannedUser := test.NewBannedUser(hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])
	err := hostAwait.Client.Create(context.TODO(), bannedUser)
	require.NoError(t, err)

	t.Logf("BannedUser '%s' created", bannedUser.Spec.Email)

	// Confirm the user is banned
	hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(t, userSignup.Name,
		wait.ContainsCondition(test.Banned()[0]))
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedUser(t *testing.T) {
	r.prepareUser(t, AppStudioProvisionedUser, r.Awaitilities.Member1().ClusterName)
	hostAwait := r.Awaitilities.Host()

	// promote to appstudio
	tiers.MoveSpaceToTier(t, hostAwait, AppStudioProvisionedUser, "appstudio")

	t.Logf("user %s was promoted to appstudio tier", AppStudioProvisionedUser)

	// verify that it's promoted
	r.Awaitilities.Host().WaitForMasterUserRecord(t, AppStudioProvisionedUser,
		wait.UntilMasterUserRecordHasConditions(test.Provisioned(), test.ProvisionedNotificationCRCreated()))
}

func (r *SetupMigrationRunner) prepareUser(t *testing.T, name string, targetCluster string) *toolchainv1alpha1.UserSignup {
	requestBuilder := test.NewSignupRequest(r.Awaitilities).
		Username(name).
		ManuallyApprove().
		TargetCluster(targetCluster)
	if !r.WithCleanup {
		requestBuilder = requestBuilder.DisableCleanup()
	}

	signup, _ := requestBuilder.
		RequireConditions(test.ConditionSet(test.Default(), test.ApprovedByAdmin())...).
		Execute(t).
		Resources()
	r.Awaitilities.Host().WaitForMasterUserRecord(t, signup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(test.Provisioned(), test.ProvisionedNotificationCRCreated()))
	return signup
}
