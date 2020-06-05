package tiers

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyNsTemplateSet(t *testing.T, awaitility *wait.Awaitility, userAccount *toolchainv1alpha1.UserAccount, tier string) {
	// Verify provisioned NSTemplateSet
	memberAwait := awaitility.Member()
	nsTemplateSet, err := memberAwait.WaitForNSTmplSet(userAccount.Name, wait.UntilNSTemplateSetHasTier(tier))
	assert.NoError(t, err)

	tierChecks, err := NewChecks(tier)
	require.NoError(t, err)

	expectedRevisions := tierChecks.GetExpectedTemplateRefs(awaitility)
	assert.Len(t, nsTemplateSet.Spec.Namespaces, len(expectedRevisions.Namespaces))

	actualTemplateRefs := []string{}
	for _, ns := range nsTemplateSet.Spec.Namespaces {
		actualTemplateRefs = append(actualTemplateRefs, ns.TemplateRef)
	}
	assert.ElementsMatch(t, expectedRevisions.Namespaces, actualTemplateRefs)

	// Verify all namespaces and objects within
	for _, templateRef := range expectedRevisions.Namespaces {
		ns, err := memberAwait.WaitForNamespace(userAccount.Name, templateRef)
		require.NoError(t, err)
		_, nsType, _, err := wait.Split(templateRef)
		require.NoError(t, err)
		for _, check := range tierChecks.GetNamespaceObjectChecks(nsType) {
			check(t, ns, memberAwait, userAccount.Name)
		}
	}
	if expectedRevisions.ClusterResources != nil {
		for _, check := range tierChecks.GetClusterObjectChecks() {
			check(t, memberAwait, userAccount.Name)
		}
	}

}
