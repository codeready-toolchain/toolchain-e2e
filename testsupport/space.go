package testsupport

import (
	"fmt"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

// NewSpace initializes a new Space object with the given value. If the targetCluster is nil, then the Space.Spec.TargetCluster is not set.
func NewSpace(namespace, name, tierName string, targetCluster *wait.MemberAwaitility) *toolchainv1alpha1.Space {
	space := &toolchainv1alpha1.Space{
		ObjectMeta: v1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: toolchainv1alpha1.SpaceSpec{
			TierName: tierName,
		},
	}
	if targetCluster != nil {
		space.Spec.TargetCluster = targetCluster.ClusterName
	}
	return space
}

// VerifyResourcesProvisionedForSpaceWithTier verifies that the Space for the given name is provisioned with all needed labels and conditions.
// It also checks the NSTemplateSet and all related namespace & cluster scoped resources
func VerifyResourcesProvisionedForSpaceWithTier(t *testing.T, awaitilities wait.Awaitilities, targetCluster *wait.MemberAwaitility, spaceName, tierName string) *toolchainv1alpha1.Space {
	return VerifyResourcesProvisionedForSpaceWithTiers(t, awaitilities, targetCluster, spaceName, tierName, tierName, tierName)
}

// VerifyResourcesProvisionedForSpaceWithTiers verifies that the Space for the given name is provisioned with all needed labels and conditions.
// It also checks the NSTemplateSet and that all related namespace-scoped resources are provisoned for the given aliasTierNamespaces, and cluster scoped resources for aliasTierClusterResources.
func VerifyResourcesProvisionedForSpaceWithTiers(t *testing.T, awaitilities wait.Awaitilities, targetCluster *wait.MemberAwaitility, spaceName, tierName, aliasTierNamespaces, aliasTierClusterResources string) *toolchainv1alpha1.Space {
	hostAwait := awaitilities.Host()

	tier, err := hostAwait.WaitForNSTemplateTier(tierName)
	require.NoError(t, err)
	hash, err := tiers.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	// wait for space to be fully provisioned
	space, err := hostAwait.WaitForSpace(spaceName,
		wait.UntilSpaceHasTier(tierName),
		wait.UntilSpaceHasLabelWithValue(fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", tierName), hash),
		wait.UntilSpaceHasConditions(Provisioned()),
		wait.UntilSpaceHasStatusTargetCluster(targetCluster.ClusterName))
	require.NoError(t, err)

	// verify that there is only one toolchain.dev.openshift.com/<>-tier-hash label
	found := false
	for key := range space.GetLabels() {
		if strings.HasPrefix(key, "toolchain.dev.openshift.com/") && strings.HasSuffix(key, "-tier-hash") {
			if !found {
				found = true
			} else {
				assert.FailNowf(t, "the space %s should have only one -tier-hash label, but has more of them: %v", space.Name, space.GetLabels())
			}
		}
	}

	// get refs & checks
	templateRefs, namespacesChecks, clusterResourcesChecks := tiers.GetRefsAndChecksForTiers(t, hostAwait, tierName, aliasTierNamespaces, aliasTierClusterResources)

	// get NSTemplateSet
	nsTemplateSet, err := targetCluster.WaitForNSTmplSet(spaceName, wait.UntilNSTemplateSetHasTier(tierName), wait.UntilNSTemplateSetHasConditions(Provisioned()))
	require.NoError(t, err)

	// verify NSTemplateSet with namespace & cluster scoped resources
	tiers.VerifyGivenNsTemplateSet(t, targetCluster, nsTemplateSet, namespacesChecks, clusterResourcesChecks, templateRefs)

	if aliasTierNamespaces == "appstudio" {
		// checks that namespace exists and has the expected label(s)
		ns, err := targetCluster.WaitForNamespace(space.Name, tier.Spec.Namespaces[0].TemplateRef, space.Spec.TierName)
		require.NoError(t, err)
		require.Contains(t, ns.Labels, toolchainv1alpha1.WorkspaceLabelKey)
		assert.Equal(t, space.Name, ns.Labels[toolchainv1alpha1.WorkspaceLabelKey])
	}

	return space
}
