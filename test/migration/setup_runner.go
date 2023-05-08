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
	ProvisionedUser             = "mig-prov"
	DeactivatedUser             = "mig-deact"
	BannedUser                  = "mig-banned"
	AppStudioProvisionedUser    = "mig-appst"
	SecondMemberProvisionedUser = "mig-m2-user"

	ProvisionedAppStudioSpace    = "mig-appst-space"
	SecondMemberProvisionedSpace = "mig-m2-space"
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
		go func(run func(t *testing.T)) {
			defer wg.Done()
			run(t)
		}(funcToRun)
	}

	wg.Wait()
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedSpace(t *testing.T) {
	r.createAndWaitForSpace(t, ProvisionedAppStudioSpace, "appstudio", r.Awaitilities.Member1())
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedSpace(t *testing.T) {
	r.createAndWaitForSpace(t, SecondMemberProvisionedSpace, "base", r.Awaitilities.Member2())
}

func (r *SetupMigrationRunner) createAndWaitForSpace(t *testing.T, name, tierName string, targetCluster *wait.MemberAwaitility) {
	hostAwait := r.Awaitilities.Host()
	space := test.NewSpace(t, r.Awaitilities, test.WithName(name), test.WithTierName(tierName), test.WithTargetCluster(targetCluster.ClusterName))
	err := hostAwait.Client.Create(context.TODO(), space)
	require.NoError(t, err)

	test.CreateMurWithAdminSpaceBindingForSpace(t, r.Awaitilities, space, r.WithCleanup)

	_, err = hostAwait.WaitForSpace(t, space.Name,
		wait.UntilSpaceHasConditions(test.Provisioned()))
	require.NoError(t, err)
	if r.WithCleanup {
		cleanup.AddCleanTasks(t, r.Awaitilities.Host().Client, space)
	}
}

func (r *SetupMigrationRunner) prepareProvisionedUser(t *testing.T) {
	r.prepareUser(t, ProvisionedUser, r.Awaitilities.Member1())
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedUser(t *testing.T) {
	r.prepareUser(t, SecondMemberProvisionedUser, r.Awaitilities.Member2())
}

func (r *SetupMigrationRunner) prepareDeactivatedUser(t *testing.T) {
	userSignup := r.prepareUser(t, DeactivatedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// deactivate the UserSignup
	userSignup, err := hostAwait.UpdateUserSignup(t, userSignup.Name,
		func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})
	require.NoError(t, err)
	t.Logf("user signup '%s' set to deactivated", userSignup.Name)

	// verify that MUR is deleted
	err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, userSignup.Status.CompliantUsername) // TODO wait for space deletion too after Space migration is done
	require.NoError(t, err)
}

func (r *SetupMigrationRunner) prepareBannedUser(t *testing.T) {
	userSignup := r.prepareUser(t, BannedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// Create the BannedUser
	bannedUser := test.NewBannedUser(hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])
	err := hostAwait.Client.Create(context.TODO(), bannedUser)
	require.NoError(t, err)

	t.Logf("BannedUser '%s' created", bannedUser.Spec.Email)

	// Confirm the user is banned
	_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(t, userSignup.Name,
		wait.ContainsCondition(test.Banned()[0]))
	require.NoError(t, err)
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedUser(t *testing.T) {
	usersignup := r.prepareUser(t, AppStudioProvisionedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// promote to appstudio
	tiers.MoveSpaceToTier(t, hostAwait, usersignup.Status.CompliantUsername, "appstudio")

	t.Logf("user %s was promoted to appstudio tier", AppStudioProvisionedUser)

	// verify that it's promoted
	_, err := r.Awaitilities.Host().WaitForMasterUserRecord(t, usersignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(test.Provisioned(), test.ProvisionedNotificationCRCreated()))
	require.NoError(t, err)
}

func (r *SetupMigrationRunner) prepareUser(t *testing.T, name string, targetCluster *wait.MemberAwaitility) *toolchainv1alpha1.UserSignup {
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
	_, err := r.Awaitilities.Host().WaitForMasterUserRecord(t, signup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(test.Provisioned(), test.ProvisionedNotificationCRCreated()))
	require.NoError(t, err)
	return signup
}
