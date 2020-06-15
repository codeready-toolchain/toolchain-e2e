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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMURToOtherRevisionOfTierTemplates(t *testing.T) {
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
