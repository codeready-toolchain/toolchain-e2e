package testsupport

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testtier "github.com/codeready-toolchain/toolchain-common/pkg/test/tier"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var notAllowedChars = regexp.MustCompile("[^-a-z0-9]")

// NewSpace initializes a new Space object with the given options. By default it sets tierName to "base".
func NewSpace(awaitilities wait.Awaitilities, opts ...SpaceOption) *toolchainv1alpha1.Space {
	namePrefix := strings.ToLower(awaitilities.Host().T.Name())
	// Remove all invalid characters
	namePrefix = notAllowedChars.ReplaceAllString(namePrefix, "")

	// Trim if the length exceeds 50 chars (63 is the max)
	if len(namePrefix) > 50 {
		namePrefix = namePrefix[0:50]
	}

	space := &toolchainv1alpha1.Space{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    awaitilities.Host().Namespace,
			GenerateName: namePrefix + "-",
		},
		Spec: toolchainv1alpha1.SpaceSpec{
			TierName: "base",
		},
	}
	for _, apply := range opts {
		apply(space)
	}
	return space
}

type SpaceOption func(*toolchainv1alpha1.Space)

func WithTargetCluster(memberCluster *wait.MemberAwaitility) SpaceOption {
	return func(s *toolchainv1alpha1.Space) {
		s.Spec.TargetCluster = memberCluster.ClusterName
	}
}

func WithTierName(tierName string) SpaceOption {
	return func(s *toolchainv1alpha1.Space) {
		s.Spec.TierName = tierName
	}
}

func WithName(name string) SpaceOption {
	return func(s *toolchainv1alpha1.Space) {
		s.Name = name
		s.GenerateName = ""
	}
}

func WithTierNameAndHashLabel(tierName, hash string) SpaceOption {
	return func(s *toolchainv1alpha1.Space) {
		s.Spec.TierName = tierName
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		s.Labels[testtier.TemplateTierHashLabelKey(tierName)] = hash
	}
}

// CreateSpace initializes a new Space object using the NewSpace function, and then creates it in the cluster
func CreateSpace(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceOption) *toolchainv1alpha1.Space {
	space := NewSpace(awaitilities, opts...)

	err := awaitilities.Host().CreateWithCleanup(context.TODO(), space)
	require.NoError(t, err)
	return space
}

// CreateAndVerifySpace does the same as CreateSpace plus it also verifies the provisioned resources in the cluster using function VerifyResourcesProvisionedForSpace
func CreateAndVerifySpace(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceOption) *toolchainv1alpha1.Space {
	space := CreateSpace(t, awaitilities, opts...)
	return VerifyResourcesProvisionedForSpace(t, awaitilities, space)
}

func getSpaceTargetMember(t *testing.T, awaitilities wait.Awaitilities, space *toolchainv1alpha1.Space) *wait.MemberAwaitility {
	for _, member := range awaitilities.AllMembers() {
		if space.Spec.TargetCluster == member.ClusterName {
			return member
		}
	}

	require.FailNowf(t, "Unable to find a target member cluster", "Space: %+v", space)
	return nil
}

// VerifyResourcesProvisionedForSpace waits until the space has some target cluster and a tier name set together with the additional criteria (if provided)
// then it gets the target member cluster specified for the space and verifies all resources provisioned for the Space
func VerifyResourcesProvisionedForSpace(t *testing.T, awaitilities wait.Awaitilities, space *toolchainv1alpha1.Space, additionalCriteria ...wait.SpaceWaitCriterion) *toolchainv1alpha1.Space {

	space, err := awaitilities.Host().WaitForSpace(space.Name,
		append(additionalCriteria,
			wait.UntilSpaceHasAnyTargetClusterSet(),
			wait.UntilSpaceHasAnyTierNameSet())...)
	require.NoError(t, err)
	targetMember := getSpaceTargetMember(t, awaitilities, space)

	return VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, targetMember, space.Name, space.Spec.TierName)
}

// Same as VerifyResourcesProvisionedForSpaceWithTiers but reuses the provided tier name for the namespace and cluster resources names
func VerifyResourcesProvisionedForSpaceWithTier(t *testing.T, awaitilities wait.Awaitilities, targetCluster *wait.MemberAwaitility, spaceName, tierName string) *toolchainv1alpha1.Space {
	return VerifyResourcesProvisionedForSpaceWithTiers(t, awaitilities, targetCluster, spaceName, tierName, tierName, tierName)
}

// VerifyResourcesProvisionedForSpaceWithTiers verifies that the Space for the given name is provisioned with all needed labels and conditions.
// It also checks the NSTemplateSet and that all related namespace-scoped resources are provisoned for the given aliasTierNamespaces, and cluster scoped resources for aliasTierClusterResources.
func VerifyResourcesProvisionedForSpaceWithTiers(t *testing.T, awaitilities wait.Awaitilities, targetCluster *wait.MemberAwaitility, spaceName, tierName, aliasTierNamespaces, aliasTierClusterResources string) *toolchainv1alpha1.Space {
	hostAwait := awaitilities.Host()

	tier, err := hostAwait.WaitForNSTemplateTier(tierName)
	require.NoError(t, err)
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	// wait for space to be fully provisioned
	space, err := hostAwait.WaitForSpace(spaceName,
		wait.UntilSpaceHasTier(tierName),
		wait.UntilSpaceHasLabelWithValue(fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", tierName), hash),
		wait.UntilSpaceHasConditions(Provisioned()),
		wait.UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValueClusterAssigned),
		wait.UntilSpaceHasStatusTargetCluster(targetCluster.ClusterName))
	require.NoError(t, err)

	// verify that there is only one toolchain.dev.openshift.com/<>-tier-hash label
	found := false
	for key := range space.GetLabels() {
		if strings.HasPrefix(key, "toolchain.dev.openshift.com/") && strings.HasSuffix(key, "-tier-hash") {
			if !found {
				found = true
			} else {
				assert.FailNowf(t, "the space %s should have only one -tier-hash label, but has more of them: %v", space.Name, space.GetLabels())
			}
		}
	}

	// get refs & checks
	templateRefs, namespacesChecks, clusterResourcesChecks := tiers.GetRefsAndChecksForTiers(t, hostAwait, tierName, aliasTierNamespaces, aliasTierClusterResources)

	// get NSTemplateSet
	nsTemplateSet, err := targetCluster.WaitForNSTmplSet(spaceName, wait.UntilNSTemplateSetHasTier(tierName), wait.UntilNSTemplateSetHasConditions(Provisioned()))
	require.NoError(t, err)

	// verify NSTemplateSet with namespace & cluster scoped resources
	tiers.VerifyGivenNsTemplateSet(t, targetCluster, nsTemplateSet, namespacesChecks, clusterResourcesChecks, templateRefs)

	if aliasTierNamespaces == "appstudio" {
		// checks that namespace exists and has the expected label(s)
		ns, err := targetCluster.WaitForNamespace(space.Name, tier.Spec.Namespaces[0].TemplateRef, space.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(t, err)
		require.Contains(t, ns.Labels, toolchainv1alpha1.WorkspaceLabelKey)
		assert.Equal(t, space.Name, ns.Labels[toolchainv1alpha1.WorkspaceLabelKey])
	}

	return space
}
