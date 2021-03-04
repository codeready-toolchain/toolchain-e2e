package tiers

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

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

	_, err := memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasTemplateRefs(expectedRevisions))
	assert.NoError(t, err)

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

// UntilNSTemplateSetHasTemplateRefs checks if the NSTemplateTier has the expected template refs
func UntilNSTemplateSetHasTemplateRefs(expectedRevisions TemplateRefs) wait.NSTemplateSetWaitCriterion {
	return func(a *wait.MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet) bool {
		actualNamespaces := nsTmplSet.Spec.Namespaces
		if len(actualNamespaces) != len(expectedRevisions.Namespaces) {
			a.T.Logf("waiting for NSTemplateSet '%s' to have the expected namespace template refs. Actual: '%s'; Expected: '%s'",
				nsTmplSet.Name, actualNamespaces, expectedRevisions.Namespaces)
			return false
		}
		if expectedRevisions.ClusterResources == nil && nsTmplSet.Spec.ClusterResources != nil {
			a.T.Logf("waiting for NSTemplateSet '%s' to have the expected cluster resource template ref - it should be nil but it is not. Actual: '%v'",
				nsTmplSet.Name, nsTmplSet.Spec.ClusterResources)
			return false
		}
		if expectedRevisions.ClusterResources != nil && nsTmplSet.Spec.ClusterResources == nil {
			a.T.Logf("waiting for NSTemplateSet '%s' to have the expected cluster resource template ref - it should not be nil but it is. Expected: '%v'",
				nsTmplSet.Name, expectedRevisions.Namespaces)
			return false
		}
		if (expectedRevisions.ClusterResources != nil && nsTmplSet.Spec.ClusterResources != nil) &&
			*expectedRevisions.ClusterResources != nsTmplSet.Spec.ClusterResources.TemplateRef {
			a.T.Logf("waiting for NSTemplateSet '%s' to have the expected cluster resource template ref. Actual: '%v'; Expected: '%v'",
				nsTmplSet.Name, nsTmplSet.Spec.ClusterResources, expectedRevisions.ClusterResources)
			return false
		}

	ExpectedNamespaces:
		for _, expectedNsRef := range expectedRevisions.Namespaces {
			for _, ns := range actualNamespaces {
				if expectedNsRef == ns.TemplateRef {
					continue ExpectedNamespaces
				}
			}
			a.T.Logf("waiting for NSTemplateSet '%s' to have the expected namespace template refs. Actual: '%s'; Expected: '%s'; Missing: '%s'",
				nsTmplSet.Name, actualNamespaces, expectedRevisions.Namespaces, expectedNsRef)
			return false
		}
		return true
	}
}
