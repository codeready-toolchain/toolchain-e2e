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

// NewSpace initializes a new Space object with the given options. By default, it doesn't set anything in the spec.
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
// It also automatically provisions MasterUserRecord and creates SpaceBinding for it
func CreateSpace(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceOption) (*toolchainv1alpha1.Space, *toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBinding) {
	space := NewSpace(awaitilities, opts...)

	err := awaitilities.Host().CreateWithCleanup(context.TODO(), space)
	require.NoError(t, err)

	// we need to  create the MUR & SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	signup, spaceBinding := CreateMurWithAdminSpaceBindingForSpace(t, awaitilities, space, true)

	return space, signup, spaceBinding
}

// CreateSpaceWithBinding initializes a new Space object using the NewSpace function, and then creates it in the cluster
// It also automatically creates SpaceBinding for it and for the given MasterUserRecord
func CreateSpaceWithBinding(t *testing.T, awaitilities wait.Awaitilities, mur *toolchainv1alpha1.MasterUserRecord, opts ...SpaceOption) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBinding) {
	space := NewSpace(awaitilities, opts...)

	err := awaitilities.Host().CreateWithCleanup(context.TODO(), space)
	require.NoError(t, err)

	// we need to  create the SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")

	return space, spaceBinding
}

// CreateAndVerifySpace does the same as CreateSpace plus it also verifies the provisioned resources in the cluster using function VerifyResourcesProvisionedForSpace
func CreateAndVerifySpace(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceOption) *toolchainv1alpha1.Space {
	space, _, _ := CreateSpace(t, awaitilities, opts...)
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
	hostAwait := awaitilities.Host()
	space, err := awaitilities.Host().WaitForSpace(space.Name,
		append(additionalCriteria,
			wait.UntilSpaceHasAnyTargetClusterSet(),
			wait.UntilSpaceHasAnyTierNameSet())...)
	require.NoError(t, err)
	targetCluster := getSpaceTargetMember(t, awaitilities, space)
	tier, err := hostAwait.WaitForNSTemplateTier(space.Spec.TierName)
	require.NoError(t, err)

	return VerifyResourcesProvisionedForSpaceWithTier(t, awaitilities, targetCluster, space.Name, tier)
}

// VerifyResourcesProvisionedForSpaceWithTier verifies that the Space for the given name is provisioned with all needed labels and conditions.
// It also checks the NSTemplateSet and that all related namespace-scoped resources are provisoned for the given aliasTierNamespaces, and cluster scoped resources for aliasTierClusterResources.
func VerifyResourcesProvisionedForSpaceWithTier(t *testing.T, awaitilities wait.Awaitilities, targetCluster *wait.MemberAwaitility, spaceName string, tier *toolchainv1alpha1.NSTemplateTier) *toolchainv1alpha1.Space {
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	hostAwait := awaitilities.Host()
	// wait for space to be fully provisioned
	space, err := hostAwait.WaitForSpace(spaceName,
		wait.UntilSpaceHasTier(tier.Name),
		wait.UntilSpaceHasLabelWithValue(fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", tier.Name), hash),
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
	templateRefs, checks := tiers.GetRefsAndChecksForTiers(t, hostAwait, tier)

	// get NSTemplateSet
	nsTemplateSet, err := targetCluster.WaitForNSTmplSet(spaceName, wait.UntilNSTemplateSetHasTier(tier.Name), wait.UntilNSTemplateSetHasConditions(Provisioned()))
	require.NoError(t, err)

	// verify NSTemplateSet with namespace & cluster scoped resources
	tiers.VerifyNSTemplateSet(t, targetCluster, nsTemplateSet, checks, templateRefs)

	if tier.Name == "appstudio" || tier.Annotations[tiers.BaseTierKey] == "appstudio" {
		// checks that namespace exists and has the expected label(s)
		ns, err := targetCluster.WaitForNamespace(space.Name, tier.Spec.Namespaces[0].TemplateRef, space.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(t, err)
		require.Contains(t, ns.Labels, toolchainv1alpha1.WorkspaceLabelKey)
		assert.Equal(t, space.Name, ns.Labels[toolchainv1alpha1.WorkspaceLabelKey])
	}

	return space
}

func CreateMurWithAdminSpaceBindingForSpace(t *testing.T, awaitilities wait.Awaitilities, space *toolchainv1alpha1.Space, cleanup bool) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBinding) {
	username := "for-space-" + space.Name
	builder := NewSignupRequest(t, awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		WaitForMUR()
	if !cleanup {
		builder.DisableCleanup()
	}
	signup, mur := builder.Execute().Resources()

	var binding *toolchainv1alpha1.SpaceBinding
	if cleanup {
		binding = CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")
	} else {
		binding = CreateSpaceBindingWithoutCleanup(t, awaitilities.Host(), mur, space, "admin")
	}

	return signup, binding
}
