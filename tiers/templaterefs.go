package tiers

import (
	"context"
	"strings"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TemplateRefs the templateRefs in a given NSTemplateTier or NSTemplateSet
type TemplateRefs struct {
	Namespaces       []string
	ClusterResources *string
}

// GetTemplateRefs returns the expected templateRefs for all the namespace templates and the optional cluster resources template for the given tier
func GetTemplateRefs(hostAwait *wait.HostAwaitility, tier string) TemplateRefs {
	templateTier, err := hostAwait.WaitForNSTemplateTier(tier, wait.UntilNSTemplateTierSpec(wait.Not(wait.HasNSTemplateRefs("000000a"))))
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

// GetTemplateRefsForTierAndRevision returns the expected templateRefs for TierTemplates containing the given tier and revisions
func GetTemplateRefsForTierAndRevision(hostAwait *wait.HostAwaitility, tier, revisions string) TemplateRefs {
	templates := &toolchainv1alpha1.TierTemplateList{}
	err := hostAwait.Client.List(context.TODO(), templates, client.InNamespace(hostAwait.Namespace))
	require.NoError(hostAwait.T, err)

	refs := TemplateRefs{}
	for _, template := range templates.Items {
		if strings.HasPrefix(template.Name, tier) && strings.HasSuffix(template.Name, revisions) {
			if strings.Contains(template.Name, "clusterresources") {
				refs.ClusterResources = &template.Name
			} else {
				refs.Namespaces = append(refs.Namespaces, template.Name)
			}
		}
	}
	return refs
}

func clusterResourcesRevision(tier toolchainv1alpha1.NSTemplateTier) *string {
	if tier.Spec.ClusterResources != nil {
		return &(tier.Spec.ClusterResources.TemplateRef)
	}
	return nil
}
