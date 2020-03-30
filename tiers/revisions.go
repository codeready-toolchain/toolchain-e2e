package tiers

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/require"
)

type Revisions struct {
	Namespaces       map[string]string
	ClusterResources string
}

func GetRevisions(awaitility *wait.Awaitility, tier string, nsTypes ...string) Revisions {
	templateTier, err := awaitility.Host().WaitForNSTemplateTier(tier, wait.UntilNSTemplateTierSpec(wait.Not(wait.HasNamespaceRevisions("000000a"))))
	require.NoError(awaitility.T, err)

	require.Len(awaitility.T, templateTier.Spec.Namespaces, len(nsTypes))
	revisions := Revisions{
		Namespaces: make(map[string]string, len(nsTypes)),
	}
	for _, typ := range nsTypes {
		if r, found := namespaceRevision(*templateTier, typ); found {
			revisions.Namespaces[typ] = r
			continue
		}
		require.FailNowf(awaitility.T, "unable to find revision for '%s' namespace in the 'basic' NSTemplateTier", typ)
		return Revisions{}
	}
	if templateTier.Spec.ClusterResources != nil {
		revisions.ClusterResources = templateTier.Spec.ClusterResources.Revision
	}
	require.Len(awaitility.T, revisions.Namespaces, len(nsTypes))
	return revisions
}

func namespaceRevision(tier toolchainv1alpha1.NSTemplateTier, typ string) (string, bool) {
	for _, ns := range tier.Spec.Namespaces {
		if ns.Type == typ {
			return ns.Revision, true
		}
	}
	return "", false
}
