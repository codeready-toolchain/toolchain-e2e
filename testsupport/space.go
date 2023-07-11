package testsupport

import (
	"fmt"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	testtier "github.com/codeready-toolchain/toolchain-common/pkg/test/tier"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CreateSpace initializes a new Space object using the NewSpace function, and then creates it in the cluster
// It also automatically provisions MasterUserRecord and creates SpaceBinding for it
func CreateSpace(t *testing.T, awaitilities wait.Awaitilities, opts ...testspace.Option) (*toolchainv1alpha1.Space, *toolchainv1alpha1.UserSignup, *toolchainv1alpha1.SpaceBinding) {
	// we need to create a MUR & SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	username := uuid.Must(uuid.NewV4()).String()
	signup, mur := NewSignupRequest(awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		NoSpace().
		WaitForMUR().Execute(t).Resources()
	t.Logf("The UserSignup %s and MUR %s were created", signup.Name, mur.Name)

	// create the actual space
	space := testspace.NewSpaceWithGeneratedName(awaitilities.Host().Namespace, NewObjectNamePrefix(t), opts...)
	space, _, err := awaitilities.Host().CreateSpaceAndSpaceBinding(t, mur, space, "admin")
	require.NoError(t, err)
	space, err = awaitilities.Host().WaitForSpace(t, space.Name,
		wait.UntilSpaceHasAnyTargetClusterSet(),
		wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)
	// let's see if spacebinding was provisioned as expected
	spaceBinding, err := awaitilities.Host().WaitForSpaceBinding(t, mur.Name, space.Name,
		wait.UntilSpaceBindingHasMurName(mur.Name),
		wait.UntilSpaceBindingHasSpaceName(space.Name),
		wait.UntilSpaceBindingHasSpaceRole("admin"),
	)
	require.NoError(t, err)
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
func CreateSpaceWithBinding(t *testing.T, awaitilities wait.Awaitilities, mur *toolchainv1alpha1.MasterUserRecord, opts ...testspace.Option) (*toolchainv1alpha1.Space, *toolchainv1alpha1.SpaceBinding) {
	space := testspace.NewSpaceWithGeneratedName(awaitilities.Host().Namespace, NewObjectNamePrefix(t), opts...)

	err := awaitilities.Host().CreateWithCleanup(t, space)
	require.NoError(t, err)

	// we need to  create the SpaceBinding, otherwise, the Space could be automatically deleted by the SpaceCleanup controller
	spaceBinding := CreateSpaceBinding(t, awaitilities.Host(), mur, space, "admin")

	return space, spaceBinding
}

// CreateSubSpace initializes a new Space object using the NewSpace function, and sets the parentSpace field value accordingly.
func CreateSubSpace(t *testing.T, awaitilities wait.Awaitilities, opts ...testspace.Option) *toolchainv1alpha1.Space {
	space := testspace.NewSpaceWithGeneratedName(awaitilities.Host().Namespace, NewObjectNamePrefix(t), opts...)

	err := awaitilities.Host().CreateWithCleanup(t, space)
	require.NoError(t, err)

	return space
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
		wait.UntilSpaceHasConditions(wait.Provisioned()),
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
	nsTmplSet, err := targetCluster.WaitForNSTmplSet(t, spaceName,
		wait.UntilNSTemplateSetHasTier(tier.Name),
		wait.UntilNSTemplateSetHasConditions(wait.Provisioned()),
	)
	require.NoError(t, err)

	// verify NSTemplateSet with namespace & cluster scoped resources
	tiers.VerifyNSTemplateSet(t, hostAwait, targetCluster, nsTmplSet, checks)

	// Wait for space to have list of provisioned namespaces in Space status.
	// the expected namespaces for `nsTmplSet.Status.ProvisionedNamespaces` are checked as part of VerifyNSTemplateSet function above.
	_, err = hostAwait.WaitForSpace(t, spaceName,
		wait.UntilSpaceHasProvisionedNamespaces(nsTmplSet.Status.ProvisionedNamespaces))
	require.NoError(t, err)

	return space, nsTmplSet
}

func CreateMurWithAdminSpaceBindingForSpace(t *testing.T, awaitilities wait.Awaitilities, space *toolchainv1alpha1.Space, cleanup bool) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord, *toolchainv1alpha1.SpaceBinding) {
	username := space.Name
	builder := NewSignupRequest(awaitilities).
		Username(username).
		Email(username + "@acme.com").
		ManuallyApprove().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
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

func GetDefaultNamespace(provisionedNamespaces []toolchainv1alpha1.SpaceNamespace) string {
	for _, namespaceObj := range provisionedNamespaces {
		if namespaceObj.Type == "default" {
			return namespaceObj.Name
		}
	}
	return ""
}
