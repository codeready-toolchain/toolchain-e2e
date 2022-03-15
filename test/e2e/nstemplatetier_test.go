package e2e

import (
	"context"
	"fmt"
	"testing"

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
	"k8s.io/apimachinery/pkg/types"
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
	tiersToCheck := []string{"advanced", "basedeactivationdisabled", "baseextended", "baseextendedidling", "baselarge", "hackathon", "test", "appstudio", "base1ns", "base"}

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
		_, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(tierToCheck,
			UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
		require.NoError(t, err)

		// verify each tier's tier object values, this corresponds to the NSTemplateTier resource that each tier has
		t.Run(fmt.Sprintf("tier object check for %s", tierToCheck), func(t *testing.T) {
			tierChecks, err := tiers.NewChecks(tierToCheck)
			require.NoError(t, err)
			for _, check := range tierChecks.GetTierObjectChecks() {
				check(t, hostAwait)
			}
		})

		t.Run(fmt.Sprintf("promote to %s tier", tierToCheck), func(t *testing.T) {
			// given
			t.Logf("promoting %s user to %s tier", testingTiersName, tierToCheck)
			changeTierRequest := NewChangeTierRequest(hostAwait.Namespace, testingTiersName, tierToCheck)

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
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

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

	// first group of users: the "cheesecake lovers"
	cheesecakeSyncIndexes := setupAccounts(t, awaitilities, "cheesecake", "cheesecakelover%02d", memberAwait, count)
	// second group of users: the "cookie lovers"
	cookieSyncIndexes := setupAccounts(t, awaitilities, "cookie", "cookielover%02d", memberAwait, count)

	// setup chocolate tier to be used for creating spaces without usersignups
	spaces := setupSpaces(t, awaitilities, "chocolate", "chocolatelover%02d", memberAwait, count)

	cheesecakeSyncIndexes = verifyResourceUpdatesForUserSignups(t, awaitilities, memberAwait, cheesecakeSyncIndexes, "cheesecake", "base", "base", true)
	cookieSyncIndexes = verifyResourceUpdatesForUserSignups(t, awaitilities, memberAwait, cookieSyncIndexes, "cookie", "base", "base", true)
	verifyResourceUpdatesForSpaces(t, awaitilities, memberAwait, spaces, "chocolate", "base", "base")

	// when updating the "cheesecakeTier" tier with the "advanced" template refs for namespaces (ie, same number of namespaces) but keep the ClusterResources refs
	updateTemplateTier(t, hostAwait, "cheesecake", "advanced", "")
	// and when updating the "cookie" tier with the "baseextendedidling" template refs (ie, same number of namespaces)
	updateTemplateTier(t, hostAwait, "cookie", "baseextendedidling", "baseextendedidling")
	// and when updating the "chocolate" tier to the "advanced" template for Namespace refs
	updateTemplateTier(t, hostAwait, "chocolate", "advanced", "")

	// then
	cheesecakeSyncIndexes = verifyResourceUpdatesForUserSignups(t, awaitilities, memberAwait, cheesecakeSyncIndexes, "cheesecake", "advanced", "base", false)
	cookieSyncIndexes = verifyResourceUpdatesForUserSignups(t, awaitilities, memberAwait, cookieSyncIndexes, "cookie", "baseextendedidling", "baseextendedidling", false)
	verifyResourceUpdatesForSpaces(t, awaitilities, memberAwait, spaces, "chocolate", "advanced", "base")

	// when updating the "cheesecakeTier" tier with the "advanced" template refs for ClusterResources but keep the Namespaces refs
	updateTemplateTier(t, hostAwait, "cheesecake", "", "advanced")
	// and when updating the "cookie" tier to the "base" template refs
	updateTemplateTier(t, hostAwait, "cookie", "base", "base")
	// and when updating the "chocolate" tier to the "advanced" template for Namespace refs
	updateTemplateTier(t, hostAwait, "chocolate", "", "advanced")

	// then
	verifyResourceUpdatesForUserSignups(t, awaitilities, memberAwait, cheesecakeSyncIndexes, "cheesecake", "advanced", "advanced", false)
	verifyResourceUpdatesForUserSignups(t, awaitilities, memberAwait, cookieSyncIndexes, "cookie", "base", "base", false)
	verifyResourceUpdatesForSpaces(t, awaitilities, memberAwait, spaces, "chocolate", "advanced", "advanced")

	// finally, verify the counters in the status.history for both 'cheesecake' and 'cookie' tiers
	// cheesecake tier
	// there should be 3 entries in the status.history (1 create + 2 update)
	verifyStatus(t, hostAwait, "cheesecake", 3)

	// cookie tier
	// there should be 3 entries in the status.history (1 create + 2 updates)
	verifyStatus(t, hostAwait, "cookie", 3)

	// chocolate tier
	// there should be 3 entries in the status.history (1 create + 2 updates)
	verifyStatus(t, hostAwait, "chocolate", 3)
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
		MoveUserToTier(t, hostAwait, updatedUserSignup.Spec.Username, "advanced")

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
func setupSpaces(t *testing.T, awaitilities Awaitilities, tierName, nameFmt string, targetCluster *MemberAwaitility, count int) []string {
	// first, let's create the a new NSTemplateTier (to avoid messing with other tiers)
	hostAwait := awaitilities.Host()
	_ = CreateNSTemplateTier(t, hostAwait, tierName)
	// verify ice cream tier created successfully
	tier, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(tierName)
	require.NoError(t, err)
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
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
func setupAccounts(t *testing.T, awaitilities Awaitilities, tierName, nameFmt string, targetCluster *MemberAwaitility, count int) map[string]string {
	// first, let's create the a new NSTemplateTier (to avoid messing with other tiers)
	hostAwait := awaitilities.Host()
	tier := CreateNSTemplateTier(t, hostAwait, tierName)

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
		MoveUserToTier(t, hostAwait, username, tier.Name)
		syncIndexes[user.Name] = mur.Spec.UserAccounts[0].SyncIndex
		t.Logf("initial syncIndex for %s: '%s'", mur.Name, syncIndexes[user.Name])
	}
	return syncIndexes
}

// updateTemplateTier updates the given "tier" using the templateRefs of the "aliasTierNamespaces"
// for namespaces and "aliasTierClusterResources" for ClusterResources (basically, we reuse the templates of the other tiers)
func updateTemplateTier(t *testing.T, hostAwait *HostAwaitility, tierName string, aliasTierNamespaces, aliasTierClusterResources string) {
	// make sure we have the very latest version of the given tier (to avoid the update conflict on the server-side)
	// make sure we have the latest revision before updating
	tier := getTier(t, hostAwait, tierName)

	if aliasTierClusterResources != "" {
		baseTier := getTier(t, hostAwait, aliasTierClusterResources)
		SetClusterTierTemplateFromTier(t, hostAwait, tier, baseTier)
	}
	if aliasTierNamespaces != "" {
		baseTier := getTier(t, hostAwait, aliasTierNamespaces)
		SetNamespaceTierTemplatesFromTier(t, hostAwait, tier, baseTier)
	}
	err := hostAwait.Client.Update(context.TODO(), tier)
	require.NoError(t, err)
}

func getTier(t *testing.T, hostAwait *HostAwaitility, tierName string) *toolchainv1alpha1.NSTemplateTier {
	tier := &toolchainv1alpha1.NSTemplateTier{}
	err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwait.Namespace,
		Name:      tierName,
	}, tier)
	require.NoError(t, err)
	return tier
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

func verifyResourceUpdatesForUserSignups(t *testing.T, awaitilities Awaitilities, memberAwaitility *MemberAwaitility, syncIndexes map[string]string, tierName, aliasTierNamespaces, aliasTierClusterResources string, tierNameChanged bool) map[string]string {

	// verify that all TemplateUpdateRequests were deleted
	hostAwait := awaitilities.Host()
	err := hostAwait.WaitForTemplateUpdateRequests(hostAwait.Namespace, 0)
	require.NoError(t, err)

	// verify individual user updates
	updatedSyncIndexes := make(map[string]string, len(syncIndexes))
	for userID, syncIndex := range syncIndexes {
		usersignup, err := hostAwait.WaitForUserSignup(userID)
		require.NoError(t, err)
		userAccount, err := memberAwaitility.WaitForUserAccount(usersignup.Status.CompliantUsername,
			UntilUserAccountHasConditions(Provisioned()),
			UntilUserAccountHasSpec(ExpectedUserAccount(usersignup.Name, usersignup.Spec.OriginalSub)),
			UntilUserAccountMatchesMur(hostAwait))
		if err != nil {
			nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(usersignup.Status.CompliantUsername)
			if err != nil {
				t.Logf("getting NSTemplateSet '%s' failed with: %s", usersignup.Status.CompliantUsername, err)
			}
			require.NoError(t, err, "Failing \nUserSignup: %+v \nUserAccount: %+v \nNSTemplateSet: %+v", usersignup, userAccount, nsTemplateSet)
		}

		// the syncIndex should be different if the tier changed. if only the template refs changed then the sync index is expected to be the same because UserAccounts no longer have references to the NSTemplateSet
		mur, err := hostAwait.WaitForMasterUserRecord(usersignup.Status.CompliantUsername,
			UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
			UntilMasterUserRecordHasSyncIndex(syncIndex, tierNameChanged),
		)
		require.NoError(t, err)
		updatedSyncIndexes[userID] = mur.Spec.UserAccounts[0].SyncIndex
		require.NotNil(t, userAccount)

		// verify space and tier resources are correctly updated
		VerifyResourcesProvisionedForSpaceWithTiers(t, awaitilities, memberAwaitility, mur.Name, tierName, aliasTierNamespaces, aliasTierClusterResources)
	}

	return updatedSyncIndexes
}

func verifyResourceUpdatesForSpaces(t *testing.T, awaitilities Awaitilities, targetCluster *MemberAwaitility, spaces []string, tierName, aliasTierNamespaces, aliasTierClusterResources string) {
	// verify that all TemplateUpdateRequests were deleted
	hostAwait := awaitilities.Host()
	err := hostAwait.WaitForTemplateUpdateRequests(hostAwait.Namespace, 0)
	require.NoError(t, err)

	// verify individual space updates
	for _, spaceName := range spaces {
		VerifyResourcesProvisionedForSpaceWithTiers(t, awaitilities, targetCluster, spaceName, tierName, aliasTierNamespaces, aliasTierClusterResources)
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
	// verify that we have 28 tier templates (base: 3, base1ns: 2, baselarge: 3, baseextended: 3, baseextendedidling: 3, basedeactivationdisabled: 3, advanced: 3, test: 3, hackathon: 3, appstudio: 2)
	require.NoError(t, err)
	// we cannot verify the exact number of tiers, because during the operator update it may happen that more TierTemplates are created
	assert.True(t, len(allTiers.Items) >= 28)
}
