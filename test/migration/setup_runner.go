package migration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	test "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
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
	T            *testing.T
	Awaitilities wait.Awaitilities
	WithCleanup  bool
}

func (r *SetupMigrationRunner) Run() {
	var wg sync.WaitGroup

	toRun := []func(){
		r.prepareAppStudioProvisionedSpace,
		r.prepareSecondMemberProvisionedSpace,
		r.prepareProvisionedUser,
		r.prepareSecondMemberProvisionedUser,
		r.prepareDeactivatedUser,
		r.prepareBannedUser,
		r.prepareAppStudioProvisionedUser}

	for _, funcToRun := range toRun {
		wg.Add(1)
		go func(run func()) {
			defer wg.Done()
			run()
		}(funcToRun)
	}

	wg.Wait()
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedSpace() {
	r.createAndWaitForSpace(ProvisionedAppStudioSpace, "appstudio", r.Awaitilities.Member1())
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedSpace() {
	r.createAndWaitForSpace(SecondMemberProvisionedSpace, "base", r.Awaitilities.Member2())
}

func (r *SetupMigrationRunner) createAndWaitForSpace(name, tierName string, targetCluster *wait.MemberAwaitility) {
	hostAwait := r.Awaitilities.Host()
	space := test.NewSpace(hostAwait.Namespace, name, tierName, targetCluster)
	err := hostAwait.Client.Create(context.TODO(), space)
	require.NoError(r.T, err)

	_, err = hostAwait.WaitForSpace(space.Name,
		wait.UntilSpaceHasConditions(test.Provisioned()))
	require.NoError(r.T, err)
	if r.WithCleanup {
		cleanup.AddCleanTasks(r.T, r.Awaitilities.Host().Client, space)
	}
}

func (r *SetupMigrationRunner) prepareProvisionedUser() {
	r.prepareUser(ProvisionedUser, r.Awaitilities.Member1())
}

func (r *SetupMigrationRunner) prepareSecondMemberProvisionedUser() {
	r.prepareUser(SecondMemberProvisionedUser, r.Awaitilities.Member2())
}

func (r *SetupMigrationRunner) prepareDeactivatedUser() {
	userSignup := r.prepareUser(DeactivatedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// deactivate the UserSignup
	userSignup, err := hostAwait.UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
		states.SetDeactivated(us, true)
	})
	require.NoError(r.T, err)
	r.T.Logf("user signup '%s' set to deactivated", userSignup.Name)

	// verify that MUR is deleted
	err = hostAwait.WaitUntilMasterUserRecordDeleted(userSignup.Status.CompliantUsername)
	require.NoError(r.T, err)
}

func (r *SetupMigrationRunner) prepareBannedUser() {
	userSignup := r.prepareUser(BannedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// Create the BannedUser
	bannedUser := test.NewBannedUser(hostAwait, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])
	err := hostAwait.Client.Create(context.TODO(), bannedUser)
	require.NoError(r.T, err)

	r.T.Logf("BannedUser '%s' created", bannedUser.Spec.Email)

	// Confirm the user is banned
	_, err = hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitForUserSignup(userSignup.Name,
		wait.ContainsCondition(test.Banned()[0]))
	require.NoError(r.T, err)
}

func (r *SetupMigrationRunner) prepareAppStudioProvisionedUser() {
	r.prepareUser(AppStudioProvisionedUser, r.Awaitilities.Member1())
	hostAwait := r.Awaitilities.Host()

	// promote to appstudio
	changeTierRequest := test.NewChangeTierRequest(hostAwait.Namespace, AppStudioProvisionedUser, "appstudio")
	err := hostAwait.CreateWithCleanup(context.TODO(), changeTierRequest)
	require.NoError(r.T, err)

	r.T.Logf("user %s was promoted to appstudio tier", AppStudioProvisionedUser)

	// verify that it's promoted
	_, err = r.Awaitilities.Host().WaitForMasterUserRecord(AppStudioProvisionedUser,
		wait.UntilMasterUserRecordHasTierName("appstudio"),
		wait.UntilMasterUserRecordHasConditions(test.Provisioned(), test.ProvisionedNotificationCRCreated()))
	require.NoError(r.T, err)
}

func (r *SetupMigrationRunner) prepareUser(name string, targetCluster *wait.MemberAwaitility) *v1alpha1.UserSignup {
	requestBuilder := test.NewSignupRequest(r.T, r.Awaitilities).
		Username(name).
		ManuallyApprove().
		TargetCluster(targetCluster)
	if !r.WithCleanup {
		requestBuilder = requestBuilder.DisableCleanup()
	}

	signup, _ := requestBuilder.
		RequireConditions(test.ConditionSet(test.Default(), test.ApprovedByAdmin())...).
		Execute().
		Resources()
	_, err := r.Awaitilities.Host().WaitForMasterUserRecord(signup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasTierName("base"),
		wait.UntilMasterUserRecordHasConditions(test.Provisioned(), test.ProvisionedNotificationCRCreated()))
	require.NoError(r.T, err)
	return signup
}
