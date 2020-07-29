package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	. "github.com/codeready-toolchain/toolchain-e2e/wait"

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
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	hostAwaitility := NewHostAwaitility(awaitility)

	// Create and approve "testingtiers" signups
	testingTiersName := "testingtiers"
	testingtiers := createAndApproveSignup(t, awaitility, testingTiersName)

	// all tiers to check - keep the basic as the last one, it will verify downgrade back to the default tier at the end of the test
	tiersToCheck := []string{"advanced", "team", "basic"}

	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.NSTemplateTierList{}
	err := awaitility.Client.List(context.TODO(), allTiers, client.InNamespace(awaitility.HostNs))
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, len(tiersToCheck))

	for _, tier := range allTiers.Items {
		assert.Contains(t, tiersToCheck, tier.Name)
	}
	var changeTierRequestNames []string

	// wait for the user to be provisioned for the first time
	verifyResourcesProvisionedForSignup(t, awaitility, testingtiers, "basic")
	for _, tierToCheck := range tiersToCheck {

		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for basic tier)
		_, err := hostAwaitility.WaitForNSTemplateTier(tierToCheck,
			UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-code-000000a"))),
			UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-dev-000000a"))),
			UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-stage-000000a"))),
			UntilNSTemplateTierSpec(Not(HasClusterResourcesTemplateRef("basic-clusterresources-000000a"))),
		)
		require.NoError(t, err)

		t.Run(fmt.Sprintf("promote to %s tier", tierToCheck), func(t *testing.T) {
			// given
			changeTierRequest := newChangeTierRequest(hostAwaitility.Ns, tierToCheck, testingTiersName)

			// when
			err = awaitility.Client.Create(context.TODO(), changeTierRequest, &test.CleanupOptions{})

			// then
			require.NoError(t, err)
			_, err := hostAwaitility.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
			require.NoError(t, err)
			verifyResourcesProvisionedForSignup(t, awaitility, testingtiers, tierToCheck)
			changeTierRequestNames = append(changeTierRequestNames, changeTierRequest.Name)
		})
	}

	// then - wait until all ChangeTierRequests are deleted by our automatic GC
	for _, name := range changeTierRequestNames {
		err := hostAwaitility.WaitUntilChangeTierRequestDeleted(name)
		assert.NoError(t, err)
	}
}

func TestUpdateNSTemplateTier(t *testing.T) {
	// in this test, we have 2 groups of users, configured with their own tier (both using the "basic" tier templates)
	// then, the first tier is updated with the "advanced" templates, whereas the second one is updated using the "team" templates
	// finally, all user namespaces are verified.
	// So, in this test, we verify that namespace resources and cluster resources are updated, on 2 groups of users with different tiers ;)

	count := 2*MaxPoolSize + 1
	ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.NSTemplateTier{})
	defer ctx.Cleanup()

	// first group of users: the "cheesecake lovers"
	cheesecakeSyncIndexes := setupAccounts(t, ctx, awaitility, "cheesecake", "cheesecakelover%02d", count)
	// second group of users: the "cookie lovers"
	cookieSyncIndexes := setupAccounts(t, ctx, awaitility, "cookie", "cookielover%02d", count)

	// when updating the "cheesecakeTier" tier with the "advanced" template refs for namespaces (ie, same number of namespaces) but keep the ClusterResources refs
	updateTemplateTier(t, awaitility, "cheesecake", "advanced", "")
	// and when updating the "cookie" tier with the "team" template refs (ie, different number of namespaces)
	updateTemplateTier(t, awaitility, "cookie", "team", "team")

	// then
	verifyResourceUpdates(t, awaitility, cheesecakeSyncIndexes, "cheesecake", "advanced", "basic")
	verifyResourceUpdates(t, awaitility, cookieSyncIndexes, "cookie", "team", "team")

	// when updating the "cheesecakeTier" tier with the "advanced" template refs for ClusterResources but keep the Namespaces refs
	updateTemplateTier(t, awaitility, "cheesecake", "", "advanced")
	// and when updating the "cookie" tier back to the "basic" template refs (ie, different number of namespaces)
	updateTemplateTier(t, awaitility, "cookie", "basic", "basic")

	// then
	verifyResourceUpdates(t, awaitility, cheesecakeSyncIndexes, "cheesecake", "advanced", "advanced")
	verifyResourceUpdates(t, awaitility, cookieSyncIndexes, "cookie", "basic", "basic")

	// finally, verify the counters in the status.history for both 'cheesecake' and 'cookie' tiers
	// cheesecake tier
	// there should be 3 entries in the status.history (1 create + 2 update)
	verifyStatus(t, awaitility, "cheesecake", 3)

	// cookie tier
	// there should be 3 entries in the status.history (1 create + 2 updates)
	verifyStatus(t, awaitility, "cookie", 3)
}

// setupAccounts takes care of:
// 1. creating a new `cheesecake` tier with the TemplateRefs of the "basic" tier.
// 2. creating 10 users (signups, MURs, etc.)
// 3. promoting the users to the `cheesecake` tier
// returns the tier, users and their "syncIndexes"
func setupAccounts(t *testing.T, ctx *test.Context, awaitility *Awaitility, tierName, nameFmt string, count int) map[string]string {
	hostAwaitility := NewHostAwaitility(awaitility)
	// first, let's create the `cheesecake` NSTemplateTier (to avoid messing with other tiers)
	// We'll use the `basic` tier as a source of inspiration.
	_, err := hostAwaitility.WaitForNSTemplateTier("basic",
		UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-code-000000a"))),
		UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-dev-000000a"))),
		UntilNSTemplateTierSpec(Not(HasNamespaceTemplateRefs("basic-stage-000000a"))),
		UntilNSTemplateTierSpec(Not(HasClusterResourcesTemplateRef("basic-clusterresources-000000a"))),
	)
	require.NoError(t, err)
	basicTier := &toolchainv1alpha1.NSTemplateTier{}
	err = hostAwaitility.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwaitility.Ns,
		Name:      "basic",
	}, basicTier)
	require.NoError(t, err)

	// now let's create the new NSTemplateTier with the same templates as the "basic" tier
	tier := &toolchainv1alpha1.NSTemplateTier{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: basicTier.Namespace,
			Name:      tierName,
		},
		Spec: basicTier.Spec,
	}
	err = hostAwaitility.Client.Create(context.TODO(), tier, testsupport.CleanupOptions(ctx))
	require.NoError(t, err)
	// let's create a few users (more than `maxPoolSize`)
	users := make([]toolchainv1alpha1.UserSignup, count)
	for i := 0; i < count; i++ {
		users[i] = createAndApproveSignup(t, awaitility, fmt.Sprintf(nameFmt, i))
	}
	// and wait until there are all provisioned
	for i := range users {
		_, err := hostAwaitility.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i))
		require.NoError(t, err)
	}
	// let's promote to users the `cheesecake` tier and retain the SyncIndexes (indexes by usersignup.Name)
	syncIndexes := make(map[string]string, len(users))
	for i, user := range users {
		changeTierRequest := newChangeTierRequest(hostAwaitility.Ns, tier.Name, fmt.Sprintf(nameFmt, i))
		err = awaitility.Client.Create(context.TODO(), changeTierRequest, &test.CleanupOptions{})
		require.NoError(t, err)
		_, err = hostAwaitility.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
		require.NoError(t, err)
		mur, err := hostAwaitility.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i),
			UntilMasterUserRecordHasCondition(provisioned())) // ignore other conditions, such as notification sent, etc.
		require.NoError(t, err)
		syncIndexes[user.Name] = mur.Spec.UserAccounts[0].SyncIndex
		t.Logf("initial syncIndex for %s: '%s'", mur.Name, syncIndexes[user.Name])
	}
	return syncIndexes
}

// updateTemplateTier updates the given "tier" using the templateRefs of the "aliasTierNamespaces"
// for namespaces and "aliasTierClusterResources" for ClusterResources (basically, we reuse the templates of the other tiers)
func updateTemplateTier(t *testing.T, awaitility *Awaitility, tierName string, aliasTierNamespaces, aliasTierClusterResources string) {
	hostAwaitility := NewHostAwaitility(awaitility)
	// make sure we have the very latest version of the given tier (to avoid the update conflict on the server-side)
	// make sure we have the latest revision before updating
	tier := getTier(t, hostAwaitility, tierName)

	if aliasTierClusterResources != "" {
		tier.Spec.ClusterResources = getTier(t, hostAwaitility, aliasTierClusterResources).Spec.ClusterResources
	}
	if aliasTierNamespaces != "" {
		tier.Spec.Namespaces = getTier(t, hostAwaitility, aliasTierNamespaces).Spec.Namespaces
	}
	err := hostAwaitility.Client.Update(context.TODO(), tier)
	require.NoError(t, err)
}

func getTier(t *testing.T, hostAwaitility *HostAwaitility, tierName string) *toolchainv1alpha1.NSTemplateTier {
	tier := &toolchainv1alpha1.NSTemplateTier{}
	err := hostAwaitility.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwaitility.Ns,
		Name:      tierName,
	}, tier)
	require.NoError(t, err)
	return tier
}

func verifyStatus(t *testing.T, awaitility *Awaitility, tierName string, expectedCount int) {
	var tier *toolchainv1alpha1.NSTemplateTier
	tier, err := awaitility.Host().WaitForNSTemplateTier(tierName, UntilNSTemplateTierStatusUpdates(expectedCount))
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

func verifyResourceUpdates(t *testing.T, awaitility *Awaitility, syncIndexes map[string]string, tierName, aliasTierNamespaces, aliasTierClusterResources string) {
	hostAwaitility := NewHostAwaitility(awaitility)
	//
	tierClusterResources, err := hostAwaitility.WaitForNSTemplateTier(aliasTierClusterResources)
	require.NoError(t, err)

	// let's wait until all MasterUserRecords have been updated
	tier, err := hostAwaitility.WaitForNSTemplateTier(tierName,
		UntilNSTemplateTierSpec(HasClusterResourcesTemplateRef(tierClusterResources.Spec.ClusterResources.TemplateRef)))
	require.NoError(t, err)

	templateRefs := tiers.GetTemplateRefs(hostAwaitility, tier.Name)
	require.NoError(t, err)
	namespacesChecks, err := tiers.NewChecks(aliasTierNamespaces)
	require.NoError(t, err)
	clusterResourcesChecks, err := tiers.NewChecks(aliasTierClusterResources)
	require.NoError(t, err)

	memberAwaitility := NewMemberAwaitility(awaitility)
	for userID, syncIndex := range syncIndexes {
		usersignup, err := hostAwaitility.WaitForUserSignup(userID)
		require.NoError(t, err)
		userAccount, err := memberAwaitility.WaitForUserAccount(usersignup.Status.CompliantUsername,
			UntilUserAccountHasConditions(provisioned()),
			UntilUserAccountHasSpec(expectedUserAccount(usersignup.Name, tier.Name, templateRefs)),
			UntilUserAccountMatchesMur(hostAwaitility))
		require.NoError(t, err)
		_, err = hostAwaitility.WaitForMasterUserRecord(usersignup.Status.CompliantUsername,
			UntilMasterUserRecordHasCondition(provisioned()), // ignore other conditions, such as notification sent, etc.
			UntilMasterUserRecordHasNotSyncIndex(syncIndex),
		)
		require.NoError(t, err)
		require.NotNil(t, userAccount)
		nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(usersignup.Status.CompliantUsername)
		require.NoError(t, err)
		tiers.VerifyGivenNsTemplateSet(t, memberAwaitility, nsTemplateSet, namespacesChecks, clusterResourcesChecks, templateRefs)
	}

	// and verify that all TemplateUpdateRequests were deleted
	err = hostAwaitility.WaitForTemplateUpdateRequests(hostAwaitility.Ns, 0)
	require.NoError(t, err)
}

func TestTierTemplates(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.TierTemplateList{}
	err := awaitility.Client.List(context.TODO(), allTiers, client.InNamespace(awaitility.HostNs))
	// verify that we have 11 tier templates (4+4+3)
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, 11)
}

func TestUpdateOfNamespacesWithLegacyLabels(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()
	for _, nsType := range []string{"code", "dev", "stage"} {
		err := awaitility.Client.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "legacy-" + nsType,
				Labels: map[string]string{
					"toolchain.dev.openshift.com/provider": "codeready-toolchain",
					"toolchain.dev.openshift.com/owner":    "legacy",
					"toolchain.dev.openshift.com/tier":     "basic",
					"toolchain.dev.openshift.com/type":     nsType,
				},
			},
		}, &test.CleanupOptions{})
		require.NoError(t, err)
	}

	// when
	legacySignup := createAndApproveSignup(t, awaitility, "legacy")

	// then
	verifyResourcesProvisionedForSignup(t, awaitility, legacySignup, "basic")
}

func newChangeTierRequest(namespace, tier, murName string) *toolchainv1alpha1.ChangeTierRequest {
	return &toolchainv1alpha1.ChangeTierRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "changetierrequest-",
		},
		Spec: toolchainv1alpha1.ChangeTierRequestSpec{
			TierName: tier,
			MurName:  murName,
		},
	}
}
