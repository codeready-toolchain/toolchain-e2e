package tiers

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/require"
)

type Revisions struct {
	Namespaces       map[string]string
	ClusterResources *string
}

const (
	// ClusterResources the key to retrieve the cluster resources template
	ClusterResources string = "clusterResources"
)

// GetRevisions returns the expected revisions for all the namespace templates and the optional cluster resources template for the given tier
func GetRevisions(hostAwait *wait.HostAwaitility, tier string) Revisions {
	templateTier, err := hostAwait.WaitForNSTemplateTier(tier, wait.UntilNSTemplateTierSpec(wait.Not(wait.HasNamespaceRevisions("000000a"))))
	require.NoError(hostAwait.T, err)
	nsRevisions := make(map[string]string, len(templateTier.Spec.Namespaces))
	for _, ns := range templateTier.Spec.Namespaces {
		nsRevisions[ns.Type] = ns.Revision
	}
	return Revisions{
		Namespaces:       nsRevisions,
		ClusterResources: clusterResourcesRevision(*templateTier),
	}
}

func clusterResourcesRevision(tier toolchainv1alpha1.NSTemplateTier) *string {
	if tier.Spec.ClusterResources != nil {
		return &(tier.Spec.ClusterResources.Revision)
	}
	return nil
}
