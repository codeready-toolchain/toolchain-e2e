package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testtier "github.com/codeready-toolchain/toolchain-common/pkg/test/tier"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var toBeComplete = toolchainv1alpha1.Condition{
	Type:   toolchainv1alpha1.ChangeTierRequestComplete,
	Status: corev1.ConditionTrue,
	Reason: toolchainv1alpha1.ChangeTierRequestChangedReason,
}

const (
	MaxPoolSize = 5 // same as hard-coded value in host operator
)

func TestNSTemplateTiers(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	// Create and approve "testingtiers" signups
	testingTiersName := "testingtiers"
	testingtiers, _ := NewSignupRequest(t, awaitilities).
		Username(testingTiersName).
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute().
		Resources()

	// all tiers to check - keep the base as the last one, it will verify downgrade back to the default tier at the end of the test
	tiersToCheck := []string{"advanced", "basedeactivationdisabled", "baseextended", "baseextendedidling", "baselarge", "hackathon", "test", "appstudio", "base"}

	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.NSTemplateTierList{}
	err := hostAwait.Client.List(context.TODO(), allTiers, client.InNamespace(hostAwait.Namespace))
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, len(tiersToCheck))

	for _, tier := range allTiers.Items {
		assert.Contains(t, tiersToCheck, tier.Name)
	}
	var changeTierRequestNames []string

	// wait for the user to be provisioned for the first time
	VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers, "base")
	for _, tierToCheck := range tiersToCheck {

		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
		tier, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(tierToCheck,
			UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
		require.NoError(t, err)

		// verify each tier's tier object values, this corresponds to the NSTemplateTier resource that each tier has
		t.Run(fmt.Sprintf("tier object check for %s", tierToCheck), func(t *testing.T) {
			tierChecks, err := tiers.NewChecksForTier(tier)
			require.NoError(t, err)
			for _, check := range tierChecks.GetTierObjectChecks() {
				check(t, hostAwait)
			}
		})

		t.Run(fmt.Sprintf("promote to %s tier", tierToCheck), func(t *testing.T) {
			// given
			t.Logf("promoting %s user to %s tier", testingTiersName, tierToCheck)
			changeTierRequest := tiers.NewChangeTierRequest(hostAwait.Namespace, testingTiersName, tierToCheck)

			// when
			err = hostAwait.CreateWithCleanup(context.TODO(), changeTierRequest)

			// then
			require.NoError(t, err)
			_, err := hostAwait.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers, tierToCheck)
			changeTierRequestNames = append(changeTierRequestNames, changeTierRequest.Name)
		})
	}

	// then - wait until all ChangeTierRequests are deleted by our automatic GC
	for _, name := range changeTierRequestNames {
		err := hostAwait.WaitUntilChangeTierRequestDeleted(name)
		assert.NoError(t, err)
	}
}

func TestSetDefaultTier(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// check that the tier exists, and all its namespace other cluster-scoped resource revisions
	// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
	err := hostAwait.WaitUntilBaseNSTemplateTierIsUpdated()
	require.NoError(t, err)

	t.Run("original default tier", func(t *testing.T) {
		// Create and approve a new user that should be provisioned to the base tier
		NewSignupRequest(t, awaitilities).
			Username("defaulttier").
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()
	})

	t.Run("changed default tier configuration", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(testconfig.Tiers().DefaultTier("advanced").DefaultSpaceTier("advanced"))
		// Create and approve a new user that should be provisioned to the advanced tier
		NewSignupRequest(t, awaitilities).
			Username("defaulttierchanged").
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()
	})
}

func TestUpdateNSTemplateTier(t *testing.T) {
	// in this test, we have 2 groups of users, configured with their own tier (both using the "base" tier templates)
	// then, the first tier is updated with the "advanced" templates, whereas the second one is updated using the "baseextendedidling" templates
	// finally, all user namespaces are verified.
	// So, in this test, we verify that namespace resources and cluster resources are updated, on 2 groups of users with different tiers ;)

	count := 2*MaxPoolSize + 1
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// we will have a lot of usersignups who are affected by the tier updates, so
	// we need to increase the timeouts on assertions/awaitilities to allow for all resources to be updated
	hostAwait = hostAwait.WithRetryOptions(TimeoutOption(hostAwait.Timeout + time.Second*time.Duration(3*count*2)))       // 3 batches of `count` accounts, with 2s of interval between each update
	memberAwait = memberAwait.WithRetryOptions(TimeoutOption(memberAwait.Timeout + time.Second*time.Duration(3*count*2))) // 3 batches of `count` accounts, with 2s of interval between each update

	err := hostAwait.WaitUntilBaseNSTemplateTierIsUpdated()
	require.NoError(t, err)
	baseTier, err := hostAwait.WaitForNSTemplateTier("base")
	require.NoError(t, err)
	advancedTier, err := hostAwait.WaitForNSTemplateTier("advanced")
	require.NoError(t, err)
	baseextendedidlingTier, err := hostAwait.WaitForNSTemplateTier("baseextendedidling")
	require.NoError(t, err)

	// create new NSTemplateTiers (derived from `base`)
	cheesecakeTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "cheesecake", baseTier)
	cookieTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "cookie", baseTier)
	chocolateTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "chocolate", baseTier)

	// first group of users: the "cheesecake users"
	cheesecakeSyncIndexes := setupAccounts(t, awaitilities, cheesecakeTier, "cheesecakeuser%02d", memberAwait, count)
	// second group of users: the "cookie users"
	cookieSyncIndexes := setupAccounts(t, awaitilities, cookieTier, "cookieuser%02d", memberAwait, count)
	// setup chocolate tier to be used for creating spaces
	spaces := setupSpaces(t, awaitilities, chocolateTier, "chocolateuser%02d", memberAwait, count)

	t.Log("verifying new users and spaces")
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cheesecakeSyncIndexes, cheesecakeTier, true)
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cookieSyncIndexes, cookieTier, true)
	verifyResourceUpdatesForSpaces(t, awaitilities, memberAwait, spaces, chocolateTier)

	t.Log("updating tiers")
	// when updating the "cheesecakeTier" tier with the "advanced" template refs for namespace resources
	cheesecakeTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, cheesecakeTier, tiers.WithNamespaceResources(advancedTier))
	// and when updating the "cookie" tier with the "baseextendedidling" template refs for both namespace resources and cluster-wide resources
	cookieTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, cookieTier, tiers.WithNamespaceResources(baseextendedidlingTier), tiers.WithClusterResources(baseextendedidlingTier))
	// and when updating the "chocolate" tier to the "advanced" template refs for namespace resources
	chocolateTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, chocolateTier, tiers.WithNamespaceResources(advancedTier))

	// then
	t.Log("verifying users and spaces after tier updates")
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cheesecakeSyncIndexes, cheesecakeTier, false)
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cookieSyncIndexes, cookieTier, false)
	verifyResourceUpdatesForSpaces(t, awaitilities, memberAwait, spaces, chocolateTier)

	// finally, verify the counters in the status.history for both 'cheesecake' and 'cookie' tiers
	// cheesecake tier
	// there should be 2 entries in the status.history (1 create + 1 update)
	verifyStatus(t, hostAwait, "cheesecake", 2)

	// cookie tier
	// there should be 2 entries in the status.history (1 create + 1 update)
	verifyStatus(t, hostAwait, "cookie", 2)

	// chocolate tier
	// there should be 2 entries in the status.history (1 create + 1 update)
	verifyStatus(t, hostAwait, "chocolate", 2)
}

func TestResetDeactivatingStateWhenPromotingUser(t *testing.T) {
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	t.Run("test reset deactivating state when promoting user", func(t *testing.T) {
		userSignup, _ := NewSignupRequest(t, awaitilities).
			Username("promoteuser").
			Email("promoteuser@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			Execute().
			Resources()

		// Set the deactivating state on the UserSignup
		updatedUserSignup, err := hostAwait.UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivating(us, true)
		})
		require.NoError(t, err)

		// Move the user to the new tier
		tiers.MoveUserToTier(t, hostAwait, updatedUserSignup.Spec.Username, "advanced")

		// Ensure the deactivating state is reset after promotion
		promotedUserSignup, err := hostAwait.WaitForUserSignup(updatedUserSignup.Name)
		require.NoError(t, err)
		require.False(t, states.Deactivating(promotedUserSignup), "usersignup should not be deactivating")
		VerifyResourcesProvisionedForSignup(t, awaitilities, promotedUserSignup, "advanced")
	})
}

// setupSpaces takes care of:
// 1. creating a new tier with the provided tierName and using the TemplateRefs of the "base" tier.
// 2. creating `count` number of spaces
func setupSpaces(t *testing.T, awaitilities Awaitilities, tier *tiers.CustomNSTemplateTier, nameFmt string, targetCluster *MemberAwaitility, count int) []string {
	hash, err := testtier.ComputeTemplateRefsHash(&tier.NSTemplateTier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)
	t.Logf("NSTemplateTier hash is: %s", hash)

	var spaces []string
	for i := 0; i < count; i++ {
		name := fmt.Sprintf(nameFmt, i)
		s, _, _ := CreateSpace(t, awaitilities, WithName(name), WithTierNameAndHashLabel(tier.Name, hash), WithTargetCluster(targetCluster))
		spaces = append(spaces, s.Name)
	}
	return spaces
}

// setupAccounts takes care of:
// 1. creating a new tier with the TemplateRefs of the "base" tier.
// 2. creating 10 users (signups, MURs, etc.)
// 3. promoting the users to the new tier
// returns the tier, users and their "syncIndexes"
func setupAccounts(t *testing.T, awaitilities Awaitilities, tier *tiers.CustomNSTemplateTier, nameFmt string, targetCluster *MemberAwaitility, count int) map[string]string {
	// first, let's create the a new NSTemplateTier (to avoid messing with other tiers)
	hostAwait := awaitilities.Host()

	// let's create a few users (more than `maxPoolSize`)
	// and wait until they are all provisioned by calling EnsureMUR()
	users := make([]*toolchainv1alpha1.UserSignup, count)
	for i := 0; i < count; i++ {
		users[i], _ = NewSignupRequest(t, awaitilities).
			Username(fmt.Sprintf(nameFmt, i)).
			ManuallyApprove().
			EnsureMUR().
			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
			TargetCluster(targetCluster).
			Execute().
			Resources()
	}

	// let's promote to users the new tier and retain the SyncIndexes (indexes by usersignup.Name)
	syncIndexes := make(map[string]string, len(users))
	for i, user := range users {
		username := fmt.Sprintf(nameFmt, i)
		mur, err := hostAwait.WaitForMasterUserRecord(username,
			UntilMasterUserRecordHasCondition(Provisioned())) // ignore other conditions, such as notification sent, etc.
		require.NoError(t, err)
		tiers.MoveUserToTier(t, hostAwait, username, tier.Name)
		syncIndexes[user.Name] = mur.Spec.UserAccounts[0].SyncIndex
		t.Logf("initial syncIndex for %s: '%s'", mur.Name, syncIndexes[user.Name])
	}
	return syncIndexes
}

func verifyStatus(t *testing.T, hostAwait *HostAwaitility, tierName string, expectedCount int) {
	var tier *toolchainv1alpha1.NSTemplateTier
	tier, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(tierName, UntilNSTemplateTierStatusUpdates(expectedCount))
	require.NoError(t, err)
	// first update: creation -> 0 MasterUserRecords affected
	assert.Equal(t, 0, tier.Status.Updates[0].Failures)
	assert.NotNil(t, tier.Status.Updates[0].CompletionTime)
	// other updates
	for i := range tier.Status.Updates[1:] {
		assert.Equal(t, 0, tier.Status.Updates[i+1].Failures)
		assert.NotNil(t, tier.Status.Updates[i+1].CompletionTime)
	}
}

func verifyResourceUpdatesForUserSignups(t *testing.T, hostAwait *HostAwaitility, memberAwaitility *MemberAwaitility, syncIndexes map[string]string, tier *tiers.CustomNSTemplateTier, tierNameChanged bool) {
	templateRefs := tiers.GetTemplateRefs(hostAwait, tier.Name)
	// if there's an annotation that describes on which other tier this one is based (for e2e tests only)
	checks, err := tiers.NewChecksForCustomTier(tier)
	require.NoError(t, err)

	// verify that all TemplateUpdateRequests were deleted
	err = hostAwait.WaitForTemplateUpdateRequests(hostAwait.Namespace, 0)
	require.NoError(t, err)

	for userID, syncIndex := range syncIndexes {
		usersignup, err := hostAwait.WaitForUserSignup(userID)
		require.NoError(t, err)
		userAccount, err := memberAwaitility.WaitForUserAccount(usersignup.Status.CompliantUsername,
			UntilUserAccountHasConditions(Provisioned()),
			UntilUserAccountHasSpec(ExpectedUserAccount(usersignup.Name, usersignup.Spec.OriginalSub)),
			UntilUserAccountMatchesMur(hostAwait))
		require.NoError(t, err)
		require.NotNil(t, userAccount)

		nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(usersignup.Status.CompliantUsername, UntilNSTemplateSetHasTier(tier.Name))
		if err != nil {
			t.Logf("getting NSTemplateSet '%s' failed with: %s", usersignup.Status.CompliantUsername, err)
		}
		require.NoError(t, err, "Failing \nUserSignup: %+v \nUserAccount: %+v \nNSTemplateSet: %+v", usersignup, userAccount, nsTemplateSet)
		tiers.VerifyNSTemplateSet(t, memberAwaitility, nsTemplateSet, checks, templateRefs)

		// the syncIndex should be different if the tier changed. if only the template refs changed then the sync index is expected to be the same because UserAccounts no longer have references to the NSTemplateSet
		mur, err := hostAwait.WaitForMasterUserRecord(usersignup.Status.CompliantUsername,
			UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
			UntilMasterUserRecordHasSyncIndex(syncIndex, tierNameChanged),
		)
		require.NoError(t, err)
		syncIndexes[userID] = mur.Spec.UserAccounts[0].SyncIndex

	}

}

func verifyResourceUpdatesForSpaces(t *testing.T, awaitilities Awaitilities, targetCluster *MemberAwaitility, spaces []string, tier *tiers.CustomNSTemplateTier) {
	hostAwait := awaitilities.Host()

	// verify that all TemplateUpdateRequests were deleted
	err := hostAwait.WaitForTemplateUpdateRequests(hostAwait.Namespace, 0)
	require.NoError(t, err)

	// verify individual space updates
	for _, spaceName := range spaces {
		VerifyResourcesProvisionedForSpaceWithCustomTier(t, awaitilities, targetCluster, spaceName, tier)
	}
}

func TestTierTemplates(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	selector := labels.NewSelector()
	e2eProducer, err := labels.NewRequirement("producer", selection.NotEquals, []string{"toolchain-e2e"})
	require.NoError(t, err)
	notCreatedByE2e := client.MatchingLabelsSelector{
		Selector: selector.Add(*e2eProducer),
	}
	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.TierTemplateList{}
	err = hostAwait.Client.List(context.TODO(), allTiers, client.InNamespace(hostAwait.Namespace), notCreatedByE2e)
	// verify that we have 26 tier templates (base: 3, baselarge: 3, baseextended: 3, baseextendedidling: 3, basedeactivationdisabled: 3, advanced: 3, test: 3, hackathon: 3, appstudio: 2)
	require.NoError(t, err)
	// we cannot verify the exact number of tiers, because during the operator update it may happen that more TierTemplates are created
	assert.True(t, len(allTiers.Items) >= 26)
}
