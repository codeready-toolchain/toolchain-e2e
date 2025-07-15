package tiers

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

// TemplateRefs the templateRefs in a given NSTemplateTier or NSTemplateSet
type TemplateRefs struct {
	Namespaces       []string
	ClusterResources *string
	SpaceRoles       map[string]string
}

// GetTemplateRefs returns the expected templateRefs for all the namespace templates and the optional cluster resources template for the given tier
func GetTemplateRefs(t *testing.T, hostAwait *wait.HostAwaitility, tierName string) TemplateRefs { // nolint:unparam // false positive on unused return param `0`??
	templateTier, err := hostAwait.WaitForNSTemplateTier(t, tierName, wait.UntilNSTemplateTierSpec(wait.HasNoTemplateRefWithSuffix("-000000a")))
	require.NoError(t, err)
	nsRefs := make([]string, 0, len(templateTier.Spec.Namespaces))
	spaceRoleRefs := make(map[string]string, len(templateTier.Spec.SpaceRoles))
	if tierName == "ttr-go-template" {
		for key, ns := range templateTier.Status.Revisions {
			if key == "dev-go" {
				nsRefs = append(nsRefs, ns)
			}
		}
		for role, ns := range templateTier.Status.Revisions {
			if role == "spacerole-admin-go" {
				spaceRoleRefs[role] = ns
			}
		}
	} else {
		for _, ns := range templateTier.Spec.Namespaces {
			nsRefs = append(nsRefs, ns.TemplateRef)
		}
		for role, ns := range templateTier.Spec.SpaceRoles {
			spaceRoleRefs[role] = ns.TemplateRef
		}
	}

	return TemplateRefs{
		Namespaces:       nsRefs,
		ClusterResources: clusterResourcesRevision(*templateTier),
		SpaceRoles:       spaceRoleRefs,
	}
}

func (r TemplateRefs) Flatten() []string {
	refs := r.Namespaces
	if r.ClusterResources != nil {
		refs = append(refs, *r.ClusterResources)
	}
	for _, ref := range r.SpaceRoles {
		refs = append(refs, ref)
	}
	return refs
}

func (r TemplateRefs) SpaceRolesFlatten() []string {
	refs := make([]string, 0, len(r.SpaceRoles))
	for _, ref := range r.SpaceRoles {
		refs = append(refs, ref)
	}
	return refs
}

func clusterResourcesRevision(tier toolchainv1alpha1.NSTemplateTier) *string {
	if tier.Name == "ttr-go-template" {
		if rev, ok := tier.Status.Revisions["cluster-resources-go"]; ok {
			return &rev
		}
	} else if tier.Spec.ClusterResources != nil {
		return &(tier.Spec.ClusterResources.TemplateRef)
	}
	return nil
}
