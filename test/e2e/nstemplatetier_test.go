package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/require"
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
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, hostAwait, memberAwait, _ := WaitForDeployments(t, tierList)
	defer ctx.Cleanup()

	// Create and approve "testingtiers" signups
	testingTiersName := "testingtiers"
	testingtiers := CreateAndApproveSignup(t, hostAwait, testingTiersName, memberAwait.ClusterName)

	// all tiers to check - keep the base as the last one, it will verify downgrade back to the default tier at the end of the test
	tiersToCheck := []string{"advanced", "team", "basicdeactivationdisabled", "test", "basic", "basedeactivationdisabled", "baseextended", "base"}

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
	VerifyResourcesProvisionedForSignup(t, hostAwait, testingtiers, "base", memberAwait)
	for _, tierToCheck := range tiersToCheck {

		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
		WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

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
			changeTierRequest := NewChangeTierRequest(hostAwait.Namespace, testingTiersName, tierToCheck)

			// when
			err = hostAwait.FrameworkClient.Create(context.TODO(), changeTierRequest, &test.CleanupOptions{})

			// then
			require.NoError(t, err)
			_, err := hostAwait.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
			require.NoError(t, err)
			VerifyResourcesProvisionedForSignup(t, hostAwait, testingtiers, tierToCheck, memberAwait)
			changeTierRequestNames = append(changeTierRequestNames, changeTierRequest.Name)
		})
	}

	// then - wait until all ChangeTierRequests are deleted by our automatic GC
	for _, name := range changeTierRequestNames {
		err := hostAwait.WaitUntilChangeTierRequestDeleted(name)
		assert.NoError(t, err)
	}
}

func TestUpdateNSTemplateTier(t *testing.T) {
	// in this test, we have 2 groups of users, configured with their own tier (both using the "base" tier templates)
	// then, the first tier is updated with the "advanced" templates, whereas the second one is updated using the "team" templates
	// finally, all user namespaces are verified.
	// So, in this test, we verify that namespace resources and cluster resources are updated, on 2 groups of users with different tiers ;)

	count := 2*MaxPoolSize + 1
	ctx, hostAwait, memberAwait, _ := WaitForDeployments(t, &toolchainv1alpha1.NSTemplateTier{})
	defer ctx.Cleanup()

	// first group of users: the "cheesecake lovers"
	cheesecakeSyncIndexes := setupAccounts(t, ctx, hostAwait, "cheesecake", "cheesecakelover%02d", memberAwait.ClusterName, count)
	// second group of users: the "cookie lovers"
	cookieSyncIndexes := setupAccounts(t, ctx, hostAwait, "cookie", "cookielover%02d", memberAwait.ClusterName, count)

	cheesecakeSyncIndexes = verifyResourceUpdates(t, hostAwait, memberAwait, cheesecakeSyncIndexes, "cheesecake", "base", "base")
	cookieSyncIndexes = verifyResourceUpdates(t, hostAwait, memberAwait, cookieSyncIndexes, "cookie", "base", "base")

	// when updating the "cheesecakeTier" tier with the "advanced" template refs for namespaces (ie, same number of namespaces) but keep the ClusterResources refs
	updateTemplateTier(t, hostAwait, "cheesecake", "advanced", "")
	// and when updating the "cookie" tier with the "team" template refs (ie, same number of namespaces)
	updateTemplateTier(t, hostAwait, "cookie", "team", "team")

	// then
	cheesecakeSyncIndexes = verifyResourceUpdates(t, hostAwait, memberAwait, cheesecakeSyncIndexes, "cheesecake", "advanced", "base")
	cookieSyncIndexes = verifyResourceUpdates(t, hostAwait, memberAwait, cookieSyncIndexes, "cookie", "team", "team")

	// when updating the "cheesecakeTier" tier with the "advanced" template refs for ClusterResources but keep the Namespaces refs
	updateTemplateTier(t, hostAwait, "cheesecake", "", "advanced")
	// and when updating the "cookie" tier to the "basic" template refs (ie, different number of namespaces)
	updateTemplateTier(t, hostAwait, "cookie", "basic", "basic")

	// then
	verifyResourceUpdates(t, hostAwait, memberAwait, cheesecakeSyncIndexes, "cheesecake", "advanced", "advanced")
	verifyResourceUpdates(t, hostAwait, memberAwait, cookieSyncIndexes, "cookie", "basic", "basic")

	// finally, verify the counters in the status.history for both 'cheesecake' and 'cookie' tiers
	// cheesecake tier
	// there should be 3 entries in the status.history (1 create + 2 update)
	verifyStatus(t, hostAwait, "cheesecake", 3)

	// cookie tier
	// there should be 3 entries in the status.history (1 create + 2 updates)
	verifyStatus(t, hostAwait, "cookie", 3)
}

// setupAccounts takes care of:
// 1. creating a new tier with the TemplateRefs of the "base" tier.
// 2. creating 10 users (signups, MURs, etc.)
// 3. promoting the users to the new tier
// returns the tier, users and their "syncIndexes"
func setupAccounts(t *testing.T, ctx *test.Context, hostAwait *HostAwaitility, tierName, nameFmt, targetCluster string, count int) map[string]string {
	// first, let's create the a new NSTemplateTier (to avoid messing with other tiers)
	tier := CreateNSTemplateTier(t, ctx, hostAwait, tierName)

	// let's create a few users (more than `maxPoolSize`)
	users := make([]*toolchainv1alpha1.UserSignup, count)
	for i := 0; i < count; i++ {
		users[i] = CreateAndApproveSignup(t, hostAwait, fmt.Sprintf(nameFmt, i), targetCluster)
	}
	// and wait until there are all provisioned
	for i := range users {
		_, err := hostAwait.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i))
		require.NoError(t, err)
	}
	// let's promote to users the new tier and retain the SyncIndexes (indexes by usersignup.Name)
	syncIndexes := make(map[string]string, len(users))
	for i, user := range users {
		mur := MoveUserToTier(t, hostAwait, fmt.Sprintf(nameFmt, i), *tier)
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
		tier.Spec.ClusterResources = getTier(t, hostAwait, aliasTierClusterResources).Spec.ClusterResources
	}
	if aliasTierNamespaces != "" {
		tier.Spec.Namespaces = getTier(t, hostAwait, aliasTierNamespaces).Spec.Namespaces
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
	tier, err := hostAwait.WaitForNSTemplateTier(tierName, UntilNSTemplateTierStatusUpdates(expectedCount))
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

func verifyResourceUpdates(t *testing.T, hostAwait *HostAwaitility, memberAwaitility *MemberAwaitility, syncIndexes map[string]string, tierName, aliasTierNamespaces, aliasTierClusterResources string) map[string]string {
	//
	tierClusterResources, err := hostAwait.WaitForNSTemplateTier(aliasTierClusterResources)
	require.NoError(t, err)

	// let's wait until all MasterUserRecords have been updated
	tier, err := hostAwait.WaitForNSTemplateTier(tierName,
		UntilNSTemplateTierSpec(HasClusterResourcesTemplateRef(tierClusterResources.Spec.ClusterResources.TemplateRef)))
	require.NoError(t, err)

	templateRefs := tiers.GetTemplateRefs(hostAwait, tier.Name)
	require.NoError(t, err)
	namespacesChecks, err := tiers.NewChecks(aliasTierNamespaces)
	require.NoError(t, err)
	clusterResourcesChecks, err := tiers.NewChecks(aliasTierClusterResources)
	require.NoError(t, err)

	// verify that all TemplateUpdateRequests were deleted
	err = hostAwait.WaitForTemplateUpdateRequests(hostAwait.Namespace, 0)
	require.NoError(t, err)

	// verify individual user updates
	updatedSyncIndexes := make(map[string]string, len(syncIndexes))
	for userID, syncIndex := range syncIndexes {
		usersignup, err := hostAwait.WaitForUserSignup(userID)
		require.NoError(t, err)
		userAccount, err := memberAwaitility.WaitForUserAccount(usersignup.Status.CompliantUsername,
			UntilUserAccountHasConditions(Provisioned()),
			UntilUserAccountHasSpec(ExpectedUserAccount(usersignup.Name, tier.Name, templateRefs)),
			UntilUserAccountMatchesMur(hostAwait))
		require.NoError(t, err, "Failing UserSignup: %+v", usersignup)
		mur, err := hostAwait.WaitForMasterUserRecord(usersignup.Status.CompliantUsername,
			UntilMasterUserRecordHasCondition(Provisioned()), // ignore other conditions, such as notification sent, etc.
			UntilMasterUserRecordHasNotSyncIndex(syncIndex),
		)
		updatedSyncIndexes[userID] = mur.Spec.UserAccounts[0].SyncIndex

		require.NoError(t, err)
		require.NotNil(t, userAccount)
		nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(usersignup.Status.CompliantUsername)
		require.NoError(t, err)
		tiers.VerifyGivenNsTemplateSet(t, memberAwaitility, nsTemplateSet, namespacesChecks, clusterResourcesChecks, templateRefs)
	}

	return updatedSyncIndexes
}

func TestTierTemplates(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, hostAwait, _, _ := WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.TierTemplateList{}
	err := hostAwait.Client.List(context.TODO(), allTiers, client.InNamespace(hostAwait.Namespace))
	// verify that we have 26 tier templates (base: 3, baseextended: 3, basedeactivationdisabled 3, basic: 4, advanced: 3, basicdeactivationdisabled 4, team 3, test 3)
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, 26)
}

func TestUpdateOfNamespacesWithLegacyLabels(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, hostAwait, memberAwait, _ := WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	for _, nsType := range []string{"code", "dev", "stage"} {
		err := memberAwait.Client.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "legacy-" + nsType,
				Labels: map[string]string{
					"toolchain.dev.openshift.com/provider": "codeready-toolchain",
					"toolchain.dev.openshift.com/owner":    "legacy",
					"toolchain.dev.openshift.com/tier":     "base",
					"toolchain.dev.openshift.com/type":     nsType,
				},
			},
		})
		require.NoError(t, err)
	}

	// when
	legacySignup := CreateAndApproveSignup(t, hostAwait, "legacy", memberAwait.ClusterName)

	// then
	VerifyResourcesProvisionedForSignup(t, hostAwait, legacySignup, "base", memberAwait)
}
