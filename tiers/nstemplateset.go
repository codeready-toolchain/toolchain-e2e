package tiers

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyNsTemplateSet(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, userAccount *toolchainv1alpha1.UserAccount, tier string) {
	// Verify provisioned NSTemplateSet
	nsTemplateSet, err := memberAwait.WaitForNSTmplSet(userAccount.Name, wait.UntilNSTemplateSetHasTier(tier))
	assert.NoError(t, err)

	tierChecks, err := NewChecks(tier)
	require.NoError(t, err)

	VerifyGivenNsTemplateSet(t, memberAwait, nsTemplateSet, tierChecks, tierChecks, tierChecks.GetExpectedTemplateRefs(hostAwait))

}

func VerifyGivenNsTemplateSet(t *testing.T, memberAwait *wait.MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet,
	tierChecksNamespaces, tierChecksClusterResources TierChecks, expectedRevisions TemplateRefs) {

	assert.Len(t, nsTmplSet.Spec.Namespaces, len(expectedRevisions.Namespaces))

	var actualTemplateRefs []string
	for _, ns := range nsTmplSet.Spec.Namespaces {
		actualTemplateRefs = append(actualTemplateRefs, ns.TemplateRef)
	}
	assert.ElementsMatch(t, expectedRevisions.Namespaces, actualTemplateRefs)
	assert.Equal(t, expectedRevisions.ClusterResources, expectedRevisions.ClusterResources)

	// Verify all namespaces and objects within
	for _, templateRef := range expectedRevisions.Namespaces {
		ns, err := memberAwait.WaitForNamespace(nsTmplSet.Name, templateRef)
		require.NoError(t, err)
		_, nsType, _, err := wait.Split(templateRef)
		require.NoError(t, err)
		for _, check := range tierChecksNamespaces.GetNamespaceObjectChecks(nsType) {
			check(t, ns, memberAwait, nsTmplSet.Name)
		}
	}
	if expectedRevisions.ClusterResources != nil {
		for _, check := range tierChecksClusterResources.GetClusterObjectChecks() {
			check(t, memberAwait, nsTmplSet.Name)
		}
	}

}
