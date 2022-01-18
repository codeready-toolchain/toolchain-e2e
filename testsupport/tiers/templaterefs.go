package tiers

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

// TemplateRefs the templateRefs in a given NSTemplateTier or NSTemplateSet
type TemplateRefs struct {
	Namespaces       []string
	ClusterResources *string
}

// GetTemplateRefs returns the expected templateRefs for all the namespace templates and the optional cluster resources template for the given tier
func GetTemplateRefs(hostAwait *wait.HostAwaitility, tier string) TemplateRefs {
	templateTier, err := hostAwait.WaitForNSTemplateTier(tier, wait.UntilNSTemplateTierSpec(wait.HasNoTemplateRefWithSuffix("-000000a")))
	require.NoError(hostAwait.T, err)
	nsRefs := make([]string, 0, len(templateTier.Spec.Namespaces))
	for _, ns := range templateTier.Spec.Namespaces {
		nsRefs = append(nsRefs, ns.TemplateRef)
	}
	return TemplateRefs{
		Namespaces:       nsRefs,
		ClusterResources: clusterResourcesRevision(*templateTier),
	}
}

func clusterResourcesRevision(tier toolchainv1alpha1.NSTemplateTier) *string {
	if tier.Spec.ClusterResources != nil {
		return &(tier.Spec.ClusterResources.TemplateRef)
	}
	return nil
}
