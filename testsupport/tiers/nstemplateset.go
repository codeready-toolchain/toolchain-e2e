package tiers

import (
	"fmt"
	"sync"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/davecgh/go-spew/spew"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyNsTemplateSet(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, space *toolchainv1alpha1.Space, tier *toolchainv1alpha1.NSTemplateTier) {
	// Verify provisioned NSTemplateSet
	nsTemplateSet, err := memberAwait.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasTier(tier.Name))
	require.NoError(t, err)

	tierChecks, err := NewChecks(tier)
	require.NoError(t, err)

	VerifyNSTemplateSet(t, memberAwait, nsTemplateSet, tierChecks, tierChecks.GetExpectedTemplateRefs(hostAwait))

}

func VerifyNSTemplateSet(t *testing.T, memberAwait *wait.MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet, checks TierChecks, expectedTemplateRefs TemplateRefs) {

	_, err := memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasTemplateRefs(expectedTemplateRefs))
	assert.NoError(t, err)

	// Verify all namespaces and objects within
	namespaceObjectChecks := sync.WaitGroup{}
	for _, templateRef := range expectedTemplateRefs.Namespaces {
		ns, err := memberAwait.WaitForNamespace(nsTmplSet.Name, templateRef, nsTmplSet.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(t, err)
		_, nsType, _, err := wait.Split(templateRef)
		require.NoError(t, err)
		for _, check := range checks.GetNamespaceObjectChecks(nsType) {
			namespaceObjectChecks.Add(1)
			go func(checkNamespaceObjects namespaceObjectsCheck) {
				defer namespaceObjectChecks.Done()
				checkNamespaceObjects(t, ns, memberAwait, nsTmplSet.Name)
			}(check)
		}
	}

	clusterObjectChecks := sync.WaitGroup{}
	if expectedTemplateRefs.ClusterResources != nil {
		for _, check := range checks.GetClusterObjectChecks() {
			clusterObjectChecks.Add(1)
			go func(check clusterObjectsCheck) {
				defer clusterObjectChecks.Done()
				check(t, memberAwait, nsTmplSet.Name, nsTmplSet.Spec.TierName)
			}(check)
		}
	}
	namespaceObjectChecks.Wait()
	clusterObjectChecks.Wait()
}

// UntilNSTemplateSetHasTemplateRefs checks if the NSTemplateTier has the expected template refs
func UntilNSTemplateSetHasTemplateRefs(expectedRevisions TemplateRefs) wait.NSTemplateSetWaitCriterion {
	return wait.NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			actualNamespaces := actual.Spec.Namespaces
			if len(actualNamespaces) != len(expectedRevisions.Namespaces) ||
				expectedRevisions.ClusterResources == nil ||
				actual.Spec.ClusterResources == nil ||
				*expectedRevisions.ClusterResources != actual.Spec.ClusterResources.TemplateRef {
				return false
			}

		ExpectedNamespaces:
			for _, expectedNsRef := range expectedRevisions.Namespaces {
				for _, ns := range actualNamespaces {
					if expectedNsRef == ns.TemplateRef {
						continue ExpectedNamespaces
					}
				}
				return false
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected NSTemplateSet '%s' to have the following cluster and namespace revisions: %s\nbut it contained: %s", actual.Name, spew.Sdump(expectedRevisions), spew.Sdump(actual.Spec.Namespaces))
		},
	}
}
