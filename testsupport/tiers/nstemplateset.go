package tiers

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"
)

func VerifyNSTemplateSet(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet, checks TierChecks) {
	t.Logf("verifying NSTemplateSet '%s' and its resources", nsTmplSet.Name)
	expectedTemplateRefs := checks.GetExpectedTemplateRefs(t, hostAwait)

	memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name, UntilNSTemplateSetHasTemplateRefs(expectedTemplateRefs))

	// Verify all namespaces and objects within
	namespaceObjectChecks := sync.WaitGroup{}
	for _, templateRef := range expectedTemplateRefs.Namespaces {
		ns := memberAwait.WaitForNamespace(t, nsTmplSet.Name, templateRef, nsTmplSet.Spec.TierName, wait.UntilNamespaceIsActive())
		_, nsType, _, err := wait.Split(templateRef)
		require.NoError(t, err)
		namespaceChecks := checks.GetNamespaceObjectChecks(nsType)
		for _, check := range namespaceChecks {
			namespaceObjectChecks.Add(1)
			go func(checkNamespaceObjects namespaceObjectsCheck) {
				defer namespaceObjectChecks.Done()
				checkNamespaceObjects(t, ns, memberAwait, nsTmplSet.Name)
			}(check)
		}
		spaceRoles := map[string][]string{}
		for _, r := range nsTmplSet.Spec.SpaceRoles {
			tmpl := hostAwait.WaitForTierTemplate(t, r.TemplateRef)
			t.Logf("space role template: %s", tmpl.GetName())
			spaceRoles[tmpl.Spec.Type] = r.Usernames
		}
		spaceRoleChecks, err := checks.GetSpaceRoleChecks(spaceRoles)
		require.NoError(t, err)
		for _, check := range spaceRoleChecks {
			namespaceObjectChecks.Add(1)
			go func(checkSpaceRoleObjects spaceRoleObjectsCheck) {
				defer namespaceObjectChecks.Done()
				checkSpaceRoleObjects(t, ns, memberAwait, nsTmplSet.Name)
			}(check)
		}
	}

	// Verify the Cluster Resources
	clusterObjectChecks := sync.WaitGroup{}
	if expectedTemplateRefs.ClusterResources != nil {
		clusterChecks := checks.GetClusterObjectChecks()
		for _, check := range clusterChecks {
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
			if expectedRevisions.ClusterResources == nil ||
				actual.Spec.ClusterResources == nil ||
				*expectedRevisions.ClusterResources != actual.Spec.ClusterResources.TemplateRef {
				return false
			}
			actualNamespaceTmplRefs := make([]string, len(actual.Spec.Namespaces))
			for i, r := range actual.Spec.Namespaces {
				actualNamespaceTmplRefs[i] = r.TemplateRef
			}
			sort.Strings(actualNamespaceTmplRefs)
			sort.Strings(expectedRevisions.Namespaces)
			if !reflect.DeepEqual(actualNamespaceTmplRefs, expectedRevisions.Namespaces) {
				return false
			}
			// checks that the actual SpaceRole templates match (ie, they are present in `expectedRevisions.SpaceRoles`)
		spaceroles:
			for _, r := range actual.Spec.SpaceRoles {
				// look-up the templateRef
				for _, ref := range expectedRevisions.SpaceRoles {
					if r.TemplateRef == ref {
						continue spaceroles
					}
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected NSTemplateSet '%s' to match the following cluster/namespace/spacerole revisions: %s\nbut it contained: %s", actual.Name, spew.Sdump(expectedRevisions), spew.Sdump(actual.Spec))
		},
	}
}
