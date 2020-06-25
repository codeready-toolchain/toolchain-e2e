package e2e

import (
	"context"
	"fmt"
	"strings"
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

func TestUpdateToOtherRevisionOfTierTemplates(t *testing.T) {
	// given
	tierList := &toolchainv1alpha1.NSTemplateTierList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, tierList)
	defer ctx.Cleanup()

	// Create and approve "testing-template-updates" user
	murName := "testingtemplateupdates"
	userSignup := createAndApproveSignup(t, awaitility, murName)

	tierTemplatesByTier := getTierTemplatesMappedByTier(t, awaitility)
	templatesFromAdvanced := createBasicTierTemplate(t, ctx, awaitility, tierTemplatesByTier["advanced"], "123abcd")
	templatesFromTeam := createBasicTierTemplate(t, ctx, awaitility, tierTemplatesByTier["team"], "456efgh")
	templatesFromBasic := createBasicTierTemplate(t, ctx, awaitility, tierTemplatesByTier["basic"], "789ijkl")

	// wait for the user to be provisioned for the first time
	verifyResourcesProvisionedForSignup(t, awaitility, userSignup, "basic")

	t.Run("update to refs basic-*-123abcd made out of the advanced TierTemplates", func(t *testing.T) {
		// when update to basic-*-123abcd
		replaceMurRefs(t, awaitility, murName, templatesFromAdvanced)

		// then
		verifyMurIsProvisionedForRevisionMadeOutOfOtherTier(t, awaitility, murName, "advanced", "123abcd")
	})

	t.Run("update to refs basic-*-456efgh made out of the team TierTemplates", func(t *testing.T) {
		// when update to basic-*-456efgh
		replaceMurRefs(t, awaitility, murName, templatesFromTeam)

		// then
		verifyMurIsProvisionedForRevisionMadeOutOfOtherTier(t, awaitility, murName, "team", "456efgh")
	})

	t.Run("update to refs basic-*-789ijkl made out of the basic TierTemplates", func(t *testing.T) {
		// when update to basic-*-789ijkl
		replaceMurRefs(t, awaitility, murName, templatesFromBasic)

		// then
		verifyMurIsProvisionedForRevisionMadeOutOfOtherTier(t, awaitility, murName, "basic", "789ijkl")
	})
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

func verifyMurIsProvisionedForRevisionMadeOutOfOtherTier(t *testing.T, awaitility *Awaitility, murName, originalTier, revision string) {
	_, err := awaitility.Host().WaitForMasterUserRecord(murName, UntilMasterUserRecordHasConditions(provisioned(), provisionedNotificationCRCreated()))
	require.NoError(t, err)

	memberAwait := awaitility.Member()
	userAccount, err := memberAwait.WaitForUserAccount(murName)
	require.NoError(t, err)
	templateRefs := tiers.GetTemplateRefsForTierAndRevision(awaitility.Host(), "basic", revision)

	nsTemplateSet, err := memberAwait.WaitForNSTmplSet(userAccount.Name, UntilNSTemplateSetHasTier("basic"))
	assert.NoError(t, err)

	tierChecks, err := tiers.NewChecks(originalTier)
	require.NoError(t, err)

	tiers.VerifyGivenNsTemplateSet(t, memberAwait, nsTemplateSet, tierChecks, templateRefs)
}

func getTierTemplatesMappedByTier(t *testing.T, awaitility *Awaitility) map[string][]*toolchainv1alpha1.TierTemplate {
	allTiers := &toolchainv1alpha1.NSTemplateTierList{}
	err := awaitility.Client.List(context.TODO(), allTiers, client.InNamespace(awaitility.HostNs))
	require.NoError(t, err)
	mappedTierTemplates := map[string][]*toolchainv1alpha1.TierTemplate{}
	for _, tier := range allTiers.Items {
		for _, ns := range tier.Spec.Namespaces {
			tierTemplate, err := awaitility.Host().WaitForTierTemplate(ns.TemplateRef)
			require.NoError(t, err)
			mappedTierTemplates[tier.Name] = append(mappedTierTemplates[tier.Name], tierTemplate)
		}
		if tier.Spec.ClusterResources != nil {
			tierTemplate, err := awaitility.Host().WaitForTierTemplate(tier.Spec.ClusterResources.TemplateRef)
			require.NoError(t, err)
			mappedTierTemplates[tier.Name] = append(mappedTierTemplates[tier.Name], tierTemplate)
		}
	}
	return mappedTierTemplates
}

func createBasicTierTemplate(t *testing.T, ctx *test.Context, awaitility *Awaitility, tierTemplates []*toolchainv1alpha1.TierTemplate, revision string) []string {
	var refs []string
	for _, tierTemplate := range tierTemplates {
		_, typeName, _, err := Split(tierTemplate.Name)
		require.NoError(t, err)
		ref := strings.ToLower(fmt.Sprintf("%s-%s-%s", "basic", typeName, revision))
		newTierTemplate := &toolchainv1alpha1.TierTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ref,
				Namespace: awaitility.HostNs,
			},
			Spec: toolchainv1alpha1.TierTemplateSpec{
				Type:     typeName,
				TierName: "basic",
				Revision: revision,
				Template: tierTemplate.Spec.Template,
			},
		}

		newTierTemplate.Name = ref
		refs = append(refs, ref)
		err = awaitility.Client.Create(context.TODO(), newTierTemplate, testsupport.CleanupOptions(ctx))
		require.NoError(t, err)
	}
	return refs
}

func replaceMurRefs(t *testing.T, awaitility *Awaitility, murName string, refs []string) {
	masterUserRecord, err := awaitility.Host().WaitForMasterUserRecord(murName)
	require.NoError(t, err)
	var namespaces []toolchainv1alpha1.NSTemplateSetNamespace
	masterUserRecord.Spec.UserAccounts[0].Spec.NSTemplateSet.ClusterResources = nil
	for _, ref := range refs {
		if strings.Contains(ref, "clusterresources") {
			masterUserRecord.Spec.UserAccounts[0].Spec.NSTemplateSet.ClusterResources = &toolchainv1alpha1.NSTemplateSetClusterResources{
				TemplateRef: ref,
			}
		} else {
			namespaces = append(namespaces, toolchainv1alpha1.NSTemplateSetNamespace{
				TemplateRef: ref,
			})
		}
	}
	masterUserRecord.Spec.UserAccounts[0].Spec.NSTemplateSet.Namespaces = namespaces

	err = awaitility.Client.Update(context.TODO(), masterUserRecord)
	require.NoError(t, err)
}

func TestUpdateNSTemplateTier(t *testing.T) {
	// this test verifies that `maxPoolSize` TemplateUpdateRequests are created when the `cheesecake` NSTemplateTier is updated
	// with new templates and there are MUR accounts associated with this tier.
	ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.NSTemplateTier{})
	defer ctx.Cleanup()
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
	basic := &toolchainv1alpha1.NSTemplateTier{}
	err = hostAwaitility.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwaitility.Ns,
		Name:      "basic",
	}, basic)
	// now let's create the new NSTemplateTier with the same templates as the "basic" tier
	cheesecake := &toolchainv1alpha1.NSTemplateTier{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: basic.Namespace,
			Name:      "cheesecake",
		},
		Spec: basic.Spec,
	}
	err = hostAwaitility.Client.Create(context.TODO(), cheesecake, testsupport.CleanupOptions(ctx))
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
		changeTierRequest := newChangeTierRequest(hostAwaitility.Ns, cheesecake.Name, fmt.Sprintf(nameFmt, i))
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
	// finally, let's update the NSTemplateTier using the namespace templates of the `advanced` tier
	advanced := &toolchainv1alpha1.NSTemplateTier{}
	err = hostAwaitility.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: hostAwaitility.Ns,
		Name:      "advanced",
	}, advanced)
	require.NoError(t, err)

	// when the `cheesecake` tier is updated
	cheesecake.Spec.ClusterResources = advanced.Spec.ClusterResources
	cheesecake.Spec.Namespaces = advanced.Spec.Namespaces
	err = hostAwaitility.Client.Update(context.TODO(), cheesecake)

	// then
	require.NoError(t, err)
	// now, let's wait until all MasterUserRecords have been updated
	for i := range users {
		_, err := hostAwaitility.WaitForMasterUserRecord(fmt.Sprintf(nameFmt, i),
			UntilMasterUserRecordHasCondition(provisioned()), // ignore other conditions, such as notification sent, etc.
			UntilMasterUserRecordHasNotSyncIndex(syncIndexes[i]),
		)
		require.NoError(t, err)
	}

	// and verify that all TemplateUpdateRequests were deleted
	hostAwaitility.WaitForTemplateUpdateRequests(hostAwaitility.Ns, 0)
}
