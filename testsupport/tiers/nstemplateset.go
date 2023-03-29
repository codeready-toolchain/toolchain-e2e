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

	_, err := memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name, UntilNSTemplateSetHasTemplateRefs(expectedTemplateRefs))
	require.NoError(t, err)

	// save the names of the namespaces provisioned by the NSTemplateSet,
	// so that we can check they are correctly reflected in the NSTemplateSet.Status.ProvisionedNamespace and Space.Status.ProvisionedNamespace.
	var actualNamespaces []string
	// Verify all namespaces and objects within
	namespaceObjectChecks := sync.WaitGroup{}
	for _, templateRef := range expectedTemplateRefs.Namespaces {
		ns, err := memberAwait.WaitForNamespace(t, nsTmplSet.Name, templateRef, nsTmplSet.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(t, err)
		_, nsType, err := wait.TierAndType(templateRef)
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
			tmpl, err := hostAwait.WaitForTierTemplate(t, r.TemplateRef)
			require.NoError(t, err)
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
		actualNamespaces = append(actualNamespaces, ns.GetName())
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

	// Once all concurrent checks are done, and the expected list of namespaces for the NSTemplateSet is generated,
	// let's verify NSTemplateSet.Status.ProvisionedNamespaces is populated as expected.
	expectedProvisionedNamespaces := getExpectedProvisionedNamespaces(actualNamespaces)
	_, err = memberAwait.WaitForNSTmplSet(t, nsTmplSet.Name, wait.UntilNSTemplateSetHasProvisionedNamespaces(expectedProvisionedNamespaces))
	require.NoError(t, err)
}

// getExpectedProvisionedNamespaces returns a list of provisioned namespaces from the given slice containing namespaces of the template tier.
// todo this is just temporary logic, since now only first namespace in alphabetical order has the `default` type,
// as soon as we introduce logic with multiple types, we may need to have one of those functions per tier (e.g. as for the other checks).
func getExpectedProvisionedNamespaces(namespaces []string) []toolchainv1alpha1.SpaceNamespace {
	expectedProvisionedNamespaces := make([]toolchainv1alpha1.SpaceNamespace, len(namespaces))
	if len(namespaces) == 0 {
		return expectedProvisionedNamespaces // no provisioned namespaces expected
	}

	// sort namespaces by name
	sort.Slice(namespaces, func(i, j int) bool {
		return namespaces[i] < namespaces[j]
	})
	// set first namespace as default for now...
	expectedProvisionedNamespaces[0] = toolchainv1alpha1.SpaceNamespace{
		Name: namespaces[0],
		Type: "default",
	}

	// skip first one since already added with `default` type,
	// but add all other namespaces without any specific type for now.
	for i := 1; i < len(namespaces); i++ {
		expectedProvisionedNamespaces[i] = toolchainv1alpha1.SpaceNamespace{
			Name: namespaces[i],
		}
	}

	return expectedProvisionedNamespaces
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
