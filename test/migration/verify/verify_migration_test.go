package verify

import (
	"context"
	"sync"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	"github.com/codeready-toolchain/toolchain-e2e/test/migration"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAfterMigration(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)

	// get Signups for the users provisioned in the setup part
	provisionedSignup := getSignupFromMUR(t, awaitilities.Host(), migration.ProvisionedUser)
	secondMemberProvisionedSignup := getSignupFromMUR(t, awaitilities.Host(), migration.SecondMemberProvisionedUser)
	appstudioProvisionedSignup := getSignupFromMUR(t, awaitilities.Host(), migration.AppStudioProvisionedUser)
	// note: listing banned/deactivated UserSignups should be done as part of setup because the tests are run in parallel and there can be multiple banned/deactivated UserSignups at that point which could lead to test flakiness
	deactivatedSignup := listAndGetSignupWithState(t, awaitilities.Host(), toolchainv1alpha1.UserSignupStateLabelValueDeactivated)
	bannedSignup := listAndGetSignupWithState(t, awaitilities.Host(), toolchainv1alpha1.UserSignupStateLabelValueBanned)

	var wg sync.WaitGroup

	// prepare all functions to verify the state of the Signups and Spaces
	toRun := []func(){
		// Spaces
		func() { verifyAppStudioProvisionedSpace(t, awaitilities) },
		func() { verifySecondMemberProvisionedSpace(t, awaitilities) },
		// UserSignups
		func() { verifyProvisionedSignup(t, awaitilities, provisionedSignup) },
		func() { verifySecondMemberProvisionedSignup(t, awaitilities, secondMemberProvisionedSignup) },
		func() { verifyAppStudioProvisionedSignup(t, awaitilities, appstudioProvisionedSignup) },
		func() { verifyDeactivatedSignup(t, awaitilities, deactivatedSignup) },
		func() { verifyBannedSignup(t, awaitilities, bannedSignup) },
	}

	// when & then - run all functions in parallel
	for _, funcToRun := range toRun {
		wg.Add(1)
		go func(run func()) {
			defer wg.Done()
			run()
		}(funcToRun)
	}

	wg.Wait()

	cleanup.ExecuteAllCleanTasks()

	t.Run("run migration setup with new operator versions for compatibility", func(t *testing.T) {
		// We need to run the migration setup part to ensure the compatibility with both versions of the sandbox (the old one as well as the new one)
		// Consider this:
		//    1. there is a running version of host/member-operator that populates the `UserAccount.Spec.NSTemplateSet` field
		//    2. the `migration setup logic` checks and expects that this field is populated
		//    3. there is a migration PR that drops usage of that `UserAccount.Spec.NSTemplateSet` field so it won't be populated any more
		//    4. there is also paired e2e PR that modifies the `verify migration test` so it doesn't expect the field to be populated
		//    5. the tests are green - so far so good.
		//    6. now, when the PRs are merged, we need to make sure that the setup will pass also for all the following PRs. If the logic wasn't compatible
		//       then the next PRs would fail in the `migration setup logic` because it would still contain the logic that would check and expect
		//       `UserAccount.Spec.NSTemplateSet` to be populated.
		//
		// That use-case could fit to any situation where the setup part relies on a functionality/feature that is present in the current version of operators
		// but won't be present in the next one.
		//
		// This is based on the assumption that everything what is merged in master works as expected. This means that we can "just" create either UserSignup
		// or Space and wait for the provisioned status (or any other status/state that signs that the process is done). We don't have to verify the actual
		// content of the resources, nor labels, etc... because it was already verified when the PR that was merged. If we agree on such a generic setup logic,
		// then we can easily make sure that it's fully compatible with both versions of Dev Sandbox.
		runner := migration.SetupMigrationRunner{
			T:            t,
			Awaitilities: awaitilities,
			WithCleanup:  true,
		}

		runner.Run()
	})
}

func verifyAppStudioProvisionedSpace(t *testing.T, awaitilities wait.Awaitilities) {
	space := VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, awaitilities.Member1(), migration.ProvisionedAppStudioSpace, "appstudio")
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, space)
}

func verifySecondMemberProvisionedSpace(t *testing.T, awaitilities wait.Awaitilities) {
	space := VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, awaitilities.Member2(), migration.SecondMemberProvisionedSpace, "base")
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, space)
}

func verifyProvisionedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "base")
	DeactivateAndCheckUser(t, awaitilities, signup)
	ReactivateAndCheckUser(t, awaitilities, signup)
}

func verifySecondMemberProvisionedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "base")
	CreateBannedUser(t, awaitilities.Host(), signup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])
}

func verifyAppStudioProvisionedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "appstudio")
}

func verifyDeactivatedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)

	_, err := awaitilities.Host().WaitForUserSignup(signup.Name,
		wait.UntilUserSignupContainsConditions(ConditionSet(Default(), DeactivatedWithoutPreDeactivation())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
	require.NoError(t, err)
	require.True(t, states.Deactivated(signup), "usersignup should be deactivated")

	err = awaitilities.Host().WaitUntilMasterUserRecordDeleted(migration.DeactivatedUser)
	require.NoError(t, err)

	ReactivateAndCheckUser(t, awaitilities, signup)
}

func verifyBannedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	hostAwait := awaitilities.Host()
	cleanup.AddCleanTasks(t, hostAwait.Client, signup)

	// verify that it's still banned
	_, err := hostAwait.WaitForUserSignup(signup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), Banned())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
	require.NoError(t, err)
	err = hostAwait.WaitUntilMasterUserRecordDeleted(migration.BannedUser)
	require.NoError(t, err)

	// get the BannedUser resource
	matchEmailHash := client.MatchingLabels{
		toolchainv1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(signup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]),
	}
	bannedUsers := &toolchainv1alpha1.BannedUserList{}
	err = hostAwait.Client.List(context.TODO(), bannedUsers, client.InNamespace(hostAwait.Namespace), matchEmailHash)
	require.NoError(t, err)
	require.Len(t, bannedUsers.Items, 1)

	// Unban the user by deleting the BannedUser resource
	err = hostAwait.Client.Delete(context.TODO(), &bannedUsers.Items[0])
	require.NoError(t, err)

	// verify that it's unbanned
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "base")
}

func getSignupFromMUR(t *testing.T, hostAwait *wait.HostAwaitility, murName string) *toolchainv1alpha1.UserSignup {
	provisionedMur, err := hostAwait.WaitForMasterUserRecord(murName)
	require.NoError(t, err)
	signup, err := hostAwait.WaitForUserSignup(provisionedMur.Labels[toolchainv1alpha1.OwnerLabelKey])
	require.NoError(t, err)

	return signup
}

func listAndGetSignupWithState(t *testing.T, hostAwait *wait.HostAwaitility, state string) *toolchainv1alpha1.UserSignup {
	userSignups := &toolchainv1alpha1.UserSignupList{}
	err := hostAwait.Client.List(context.TODO(), userSignups, client.InNamespace(hostAwait.Namespace), client.MatchingLabels{toolchainv1alpha1.UserSignupStateLabelKey: state})
	require.NoError(t, err)

	require.Len(t, userSignups.Items, 1)
	return &userSignups.Items[0]
}
