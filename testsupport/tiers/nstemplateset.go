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
	templatev1 "github.com/openshift/api/template/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

func VerifyNSTemplateSet(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, nsTmplSet *toolchainv1alpha1.NSTemplateSet, checks TierChecks) {
	t.Logf("verifying NSTemplateSet '%s' and its resources", nsTmplSet.Name)
	expectedTemplateRefs := checks.GetExpectedTemplateRefs(hostAwait)

	_, err := memberAwait.WaitForNSTmplSet(nsTmplSet.Name, UntilNSTemplateSetHasTemplateRefs(expectedTemplateRefs))
	require.NoError(t, err)

	// Verify all namespaces and objects within
	namespaceObjectChecks := sync.WaitGroup{}
	for _, templateRef := range expectedTemplateRefs.Namespaces {
		ns, err := memberAwait.WaitForNamespace(nsTmplSet.Name, templateRef, nsTmplSet.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(t, err)
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
		spaceRoles := map[*templatev1.Template][]string{}
		decoder := serializer.NewCodecFactory(hostAwait.Client.Scheme()).UniversalDeserializer()
		for _, r := range nsTmplSet.Spec.SpaceRoles {
			tmpl, err := hostAwait.WaitForTierTemplate(r.TemplateRef)
			require.NoError(t, err)
			t.Logf("space role template: %s / %s", tmpl.GetName(), tmpl.Spec.Template.GetName())
			for i, rawObj := range tmpl.Spec.Template.Objects {
				var obj unstructured.Unstructured
				_, _, err := decoder.Decode(rawObj.Raw, nil, &obj)
				require.NoError(t, err)
				tmpl.Spec.Template.Objects[i].Object = &obj
			}

			spaceRoles[&tmpl.Spec.Template] = r.Usernames
		}
		spaceRoleChecks, err := checks.GetSpaceRoleChecks(spaceRoles)
		require.NoError(t, err)
		for _, check := range spaceRoleChecks {
			namespaceObjectChecks.Add(1)
			go func(checkNamespaceObjects spaceRoleObjectsCheck) {
				defer namespaceObjectChecks.Done()
				checkNamespaceObjects(t, ns, memberAwait, nsTmplSet.Name)
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
			actualNamespaces := actual.Spec.Namespaces
			if expectedRevisions.ClusterResources == nil ||
				actual.Spec.ClusterResources == nil ||
				*expectedRevisions.ClusterResources != actual.Spec.ClusterResources.TemplateRef {
				return false
			}
			actualNamespaceTmplRefs := make([]string, len(actualNamespaces))
			for i, r := range actualNamespaces {
				actualNamespaceTmplRefs[i] = r.TemplateRef
			}
			sort.Strings(actualNamespaceTmplRefs)
			sort.Strings(expectedRevisions.Namespaces)
			return reflect.DeepEqual(actualNamespaceTmplRefs, expectedRevisions.Namespaces)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected NSTemplateSet '%s' to have the following cluster and namespace revisions: %s\nbut it contained: %s", actual.Name, spew.Sdump(expectedRevisions), spew.Sdump(actual.Spec.Namespaces))
		},
	}
}
