package tiers

import (
	"testing"

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
	templateTier, err := hostAwait.WaitForNSTemplateTier(t, tierName, wait.UntilNSTemplateTierSpec(wait.HasNoTemplateRefWithSuffix("-000000a")), wait.HasStatusTierTemplateRevisionKeys())
	require.NoError(t, err)

	nsRefs := make([]string, 0, len(templateTier.Spec.Namespaces))
	spaceRoleRefs := make(map[string]string, len(templateTier.Spec.SpaceRoles))
	var clusterResources *string

	for _, ns := range templateTier.Spec.Namespaces {
		if revision, ok := templateTier.Status.Revisions[ns.TemplateRef]; ok {
			nsRefs = append(nsRefs, revision)
		}
	}

	for role, spaceRole := range templateTier.Spec.SpaceRoles {
		if revision, ok := templateTier.Status.Revisions[spaceRole.TemplateRef]; ok {
			spaceRoleRefs[role] = revision
		}
	}

	if templateTier.Spec.ClusterResources != nil {
		if rev, ok := templateTier.Status.Revisions[templateTier.Spec.ClusterResources.TemplateRef]; ok {
			clusterResources = &rev
		}
	}
	return TemplateRefs{
		Namespaces:       nsRefs,
		ClusterResources: clusterResources,
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
