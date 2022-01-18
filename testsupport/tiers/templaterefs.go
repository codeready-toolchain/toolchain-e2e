package tiers

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"sort"

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

// ComputeTemplateRefsHash computes the hash of the `.spec.namespaces[].templateRef` + `.spec.clusteResource.TemplateRef`
func ComputeTemplateRefsHash(tier *toolchainv1alpha1.NSTemplateTier) (string, error) {
	refs := []string{}
	for _, ns := range tier.Spec.Namespaces {
		refs = append(refs, ns.TemplateRef)
	}
	if tier.Spec.ClusterResources != nil {
		refs = append(refs, tier.Spec.ClusterResources.TemplateRef)
	}
	sort.Strings(refs)
	m, err := json.Marshal(templateRefs{Refs: refs})
	if err != nil {
		return "", err
	}
	md5hash := md5.New()
	// Ignore the error, as this implementation cannot return one
	_, _ = md5hash.Write(m)
	hash := hex.EncodeToString(md5hash.Sum(nil))
	return hash, nil
}

// TemplateTierHashLabel returns the label key to specify the version of the templates of the given tier
func TemplateTierHashLabelKey(tierName string) string {
	return toolchainv1alpha1.LabelKeyPrefix + tierName + "-tier-hash"
}

type templateRefs struct {
	Refs []string `json:"refs"`
}
