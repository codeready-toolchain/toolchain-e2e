package testsupport

import (
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
func NewSpace(t *testing.T, awaitilities wait.Awaitilities, opts ...SpaceOption) *toolchainv1alpha1.Space {
	namePrefix := strings.ToLower(t.Name())
	// Remove all invalid characters
	namePrefix = notAllowedChars.ReplaceAllString(namePrefix, "")

	// Trim if the length exceeds 40 chars (63 is the max)
	if len(namePrefix) > 40 {
		namePrefix = namePrefix[0:40]
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

func WithTargetCluster(clusterName string) SpaceOption {
	return func(s *toolchainv1alpha1.Space) {
		s.Spec.TargetCluster = clusterName
	}
}

func WithParentSpace(name string) SpaceOption {
	return func(s *toolchainv1alpha1.Space) {
		s.Spec.ParentSpace = name
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
	space := NewSpace(t, awaitilities, opts...)
	err := awaitilities.Host().CreateWithCleanup(t, space)
	require.NoError(t, err)
	space, err = awaitilities.Host().WaitForSpace(t, space.Name, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)
	// we also need to create a MUR & SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	signup, mur, spaceBinding := CreateMurWithAdminSpaceBindingForSpace(t, awaitilities, space, true)
	// make sure that the NSTemplateSet associated with the Space was updated after the space binding was created (new entry in the `spec.SpaceRoles`)
	// before we can check the resources (roles and rolebindings)
	tier, err := awaitilities.Host().WaitForNSTemplateTier(t, space.Spec.TierName)
	require.NoError(t, err)
	if memberAwait, err := awaitilities.Member(space.Status.TargetCluster); err == nil {
		// if member is `unknown` or invalid (depending on the test case), then don't try to check the associated NSTemplateSet
		_, err = memberAwait.WaitForNSTmplSet(t, space.Name,
			wait.UntilNSTemplateSetHasSpaceRoles(
				wait.SpaceRole(tier.Spec.SpaceRoles["admin"].TemplateRef, mur.Name)))
		require.NoError(t, err)
	}

	return space, signup, spaceBinding
}

// CreateSpaceWithBinding initializes a new Space object using the NewSpace function, and then creates it in the cluster
// It also automatically creates SpaceBinding for it and for the given MasterUserRecord
func CreateSpaceWithBinding(t *testing.T, awaitilities wait.Awaitilities, mur *toolchainv1alpha1.MasterUserRecord, opts ...SpaceOption) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBinding) {
	space := NewSpace(t, awaitilities, opts...)

	err := awaitilities.Host().CreateWithCleanup(t, space)
	require.NoError(t, err)

	// we need to  create the SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")

	return space, spaceBinding
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
func VerifyResourcesProvisionedForSpace(t *testing.T, awaitilities wait.Awaitilities, spaceName string, additionalCriteria ...wait.SpaceWaitCriterion) (*toolchainv1alpha1.Space, *toolchainv1alpha1.NSTemplateSet) {
	space, err := awaitilities.Host().WaitForSpace(t, spaceName,
		append(additionalCriteria,
			wait.UntilSpaceHasAnyTargetClusterSet(),
			wait.UntilSpaceHasAnyTierNameSet())...)
	require.NoError(t, err)
	targetCluster := getSpaceTargetMember(t, awaitilities, space)
	tier, err := awaitilities.Host().WaitForNSTemplateTier(t, space.Spec.TierName)
	require.NoError(t, err)
	checks, err := tiers.NewChecksForTier(tier)
	require.NoError(t, err)

	t.Logf("verifying resources provisioned for space '%s' with tier '%s'", space.Name, space.Spec.TierName)
	return verifyResourcesProvisionedForSpace(t, awaitilities.Host(), targetCluster, spaceName, tier, checks)
}

func VerifyResourcesProvisionedForSpaceWithCustomTier(t *testing.T, hostAwait *wait.HostAwaitility, targetCluster *wait.MemberAwaitility, spaceName string, tier *tiers.CustomNSTemplateTier) (*toolchainv1alpha1.Space, *toolchainv1alpha1.NSTemplateSet) {
	checks := tiers.NewChecksForCustomTier(t, tier)
	return verifyResourcesProvisionedForSpace(t, hostAwait, targetCluster, spaceName, tier.NSTemplateTier, checks)
}

func verifyResourcesProvisionedForSpace(t *testing.T, hostAwait *wait.HostAwaitility, targetCluster *wait.MemberAwaitility, spaceName string, tier *toolchainv1alpha1.NSTemplateTier, checks tiers.TierChecks) (*toolchainv1alpha1.Space, *toolchainv1alpha1.NSTemplateSet) {
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	// wait for space to be fully provisioned
	space, err := hostAwait.WaitForSpace(t, spaceName,
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

	// get NSTemplateSet
	nsTmplSet, err := targetCluster.WaitForNSTmplSet(t, spaceName, wait.UntilNSTemplateSetHasTier(tier.Name), wait.UntilNSTemplateSetHasConditions(Provisioned()))
	require.NoError(t, err)

	// verify NSTemplateSet with namespace & cluster scoped resources
	tiers.VerifyNSTemplateSet(t, hostAwait, targetCluster, nsTmplSet, checks)

	if tier.Name == "appstudio" {
		// checks that namespace exists and has the expected label(s)
		ns, err := targetCluster.WaitForNamespace(t, space.Name, tier.Spec.Namespaces[0].TemplateRef, space.Spec.TierName, wait.UntilNamespaceIsActive())
		require.NoError(t, err)
		require.Contains(t, ns.Labels, toolchainv1alpha1.WorkspaceLabelKey)
		assert.Equal(t, space.Name, ns.Labels[toolchainv1alpha1.WorkspaceLabelKey])
	}

	return space, nsTmplSet
}

func CreateMurWithAdminSpaceBindingForSpace(t *testing.T, awaitilities wait.Awaitilities, space *toolchainv1alpha1.Space, cleanup bool) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord, *toolchainv1alpha1.SpaceBinding) {
	username := "for-space-" + space.Name
	builder := NewSignupRequest(awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		NoSpace().
		WaitForMUR()
	if !cleanup {
		builder.DisableCleanup()
	}
	signup, mur := builder.Execute(t).Resources()
	t.Logf("The UserSignup %s and MUR %s were created", signup.Name, mur.Name)
	var binding *toolchainv1alpha1.SpaceBinding
	if cleanup {
		binding = CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")
	} else {
		binding = CreateSpaceBindingWithoutCleanup(t, awaitilities.Host(), mur, space, "admin")
	}
	t.Logf("The SpaceBinding %s was created", binding.Name)
	return signup, mur, binding
}
