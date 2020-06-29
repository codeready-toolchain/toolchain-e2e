package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
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
	// this test verifies that `maxPoolSize` TemplateUpdateRequests are created when the `cheesecake` NSTemplateTier is updated
	// with new templates and there are MUR accounts associated with this tier.
	ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.NSTemplateTier{})
	// defer ctx.Cleanup()
	hostAwaitility := NewHostAwaitility(awaitility)
	memberAwaitility := NewMemberAwaitility(awaitility)

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
	cheesecakeTier := &toolchainv1alpha1.NSTemplateTier{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: basicTier.Namespace,
			Name:      "cheesecake",
		},
		Spec: basicTier.Spec,
	}
	err = hostAwaitility.Client.Create(context.TODO(), cheesecakeTier, testsupport.CleanupOptions(ctx))
	require.NoError(t, err)
	// let's create a few users (more than `maxPoolSize`)
	count := 2*MaxPoolSize + 1
	users := make([]toolchainv1alpha1.UserSignup, count)
	nameFmt := "cheesecakelover%02d"
	for i := 0; i < count; i++ {
		users[i] = createAndApproveSignup(t, awaitility, fmt.Sprintf(nameFmt, i))
	}
	// and wait until there are all provisioned
	for i := range users {
		_, err := hostAwaitility.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i))
		require.NoError(t, err)
	}
	// let's promote to users the `cheesecake` tier and retain the SyncIndexes
	syncIndexes := make([]string, len(users))
	for i := range users {
		changeTierRequest := newChangeTierRequest(hostAwaitility.Ns, cheesecakeTier.Name, fmt.Sprintf(nameFmt, i))
		err = awaitility.Client.Create(context.TODO(), changeTierRequest, &test.CleanupOptions{})
		require.NoError(t, err)
		_, err = hostAwaitility.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
		require.NoError(t, err)
		mur, err := hostAwaitility.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i),
			UntilMasterUserRecordHasCondition(provisioned())) // ignore other conditions, such as notification sent, etc.
		require.NoError(t, err)
		syncIndexes[i] = mur.Spec.UserAccounts[0].SyncIndex
		t.Logf("'%s' initial syncIndex: '%s'", mur.Name, syncIndexes[i])
	}
	// finally, let's retrieve the `advanced` NSTemplateTier
	advancedTier := &toolchainv1alpha1.NSTemplateTier{}
	err = hostAwaitility.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwaitility.Ns,
		Name:      "advanced",
	}, advancedTier)
	require.NoError(t, err)

	// when the `cheesecake` tier is updated using the `advanced` templates
	cheesecakeTier.Spec.ClusterResources = advancedTier.Spec.ClusterResources
	cheesecakeTier.Spec.Namespaces = advancedTier.Spec.Namespaces
	err = hostAwaitility.Client.Update(context.TODO(), cheesecakeTier)

	// then
	require.NoError(t, err)

	// now, let's wait until all MasterUserRecords have been updated
	cheesecakeTier, err = hostAwaitility.WaitForNSTemplateTier(cheesecakeTier.Name,
		UntilNSTemplateTierSpec(HasClusterResourcesTemplateRef(advancedTier.Spec.ClusterResources.TemplateRef)))
	require.NoError(t, err)

	templateRefs := tiers.GetTemplateRefs(hostAwaitility, cheesecakeTier.Name) // here we need to use the `advanced` tier name so we can init the tier checks :|
	require.NoError(t, err)
	checks, err := tiers.NewChecks("advanced")
	require.NoError(t, err)

	for i, user := range users {
		mur, err := hostAwaitility.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i),
			UntilMasterUserRecordHasCondition(provisioned()), // ignore other conditions, such as notification sent, etc.
			UntilMasterUserRecordHasNotSyncIndex(syncIndexes[i]),
		)
		require.NoError(t, err)
		userAccount, err := memberAwaitility.WaitForUserAccount(fmt.Sprintf(nameFmt, i),
			wait.UntilUserAccountHasConditions(provisioned()),
			wait.UntilUserAccountHasSpec(expectedUserAccount(user.Name, cheesecakeTier.Name, templateRefs)),
			wait.UntilUserAccountMatchesMur(mur.Spec, mur.Spec.UserAccounts[0].Spec))
		require.NoError(t, err)
		require.NotNil(t, userAccount)
		nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(fmt.Sprintf(nameFmt, i))
		require.NoError(t, err)
		tiers.VerifyGivenNsTemplateSet(t, memberAwaitility, nsTemplateSet, checks, templateRefs)
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
