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

	expectedRevisions := tierChecks.GetExpectedRevisions(awaitility)
	assert.Len(t, nsTemplateSet.Spec.Namespaces, len(expectedRevisions.Namespaces))

Revisions:
	for nsType, revision := range expectedRevisions.Namespaces {
		for _, ns := range nsTemplateSet.Spec.Namespaces {
			if ns.Type == nsType {
				assert.Equal(t, revision, ns.Revision)
				assert.Empty(t, ns.Template)
				continue Revisions
			}
		}
		assert.FailNowf(t, "the namespace type '%s' wasn't found in '%v'", nsType, nsTemplateSet)
	}

	if expectedRevisions.ClusterResources == "" {
		assert.Nil(t, nsTemplateSet.Spec.ClusterResources)
	} else {
		require.NotNil(t, nsTemplateSet.Spec.ClusterResources)
		assert.Equal(t, expectedRevisions.ClusterResources, nsTemplateSet.Spec.ClusterResources.Revision)
		assert.Empty(t, nsTemplateSet.Spec.ClusterResources.Template)
	}

	// Verify all namespaces and RoleBindings in these namespaces
	for key, revision := range expectedRevisions.Namespaces {
		ns, err := memberAwait.WaitForNamespace(userAccount.Name, key, revision, tier)
		require.NoError(t, err)
		for _, check := range tierChecks.GetInnerObjectChecks(key) {
			check(t, ns, memberAwait, userAccount.Name)
		}
	}
}
