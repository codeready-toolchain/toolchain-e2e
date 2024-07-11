package parallel

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	MaxPoolSize = 5 // same as hard-coded value in host operator
)

func TestNSTemplateTiers(t *testing.T) {
	t.Parallel()
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	user := NewSignupRequest(awaitilities).
		Username("testnstemplatetiers").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	testingtiers := user.UserSignup
	space := user.Space

	// all tiers to check - keep the base as the last one, it will verify downgrade back to the default tier at the end of the test
	tiersToCheck := []string{"advanced", "baseextendedidling", "baselarge", "test", "appstudio", "appstudiolarge", "appstudio-env", "base1ns", "base1nsnoidling", "base1ns6didler", "base"}

	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.NSTemplateTierList{}
	e2eProducer, err := labels.NewRequirement("producer", selection.NotEquals, []string{"toolchain-e2e"})
	require.NoError(t, err)
	notCreatedByE2e := client.MatchingLabelsSelector{
		Selector: labels.NewSelector().Add(*e2eProducer),
	}
	err = hostAwait.Client.List(context.TODO(), allTiers, client.InNamespace(hostAwait.Namespace), notCreatedByE2e)
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, len(tiersToCheck))

	for _, tier := range allTiers.Items {
		assert.Contains(t, tiersToCheck, tier.Name)
	}

	// wait for the user to be provisioned for the first time
	VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers, "deactivate30", "base") // deactivate30 is the default UserTier and base is the default SpaceTier

	for _, tierToCheck := range tiersToCheck {
		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
		_, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, tierToCheck,
			wait.UntilNSTemplateTierSpec(wait.HasNoTemplateRefWithSuffix("-000000a")))
		require.NoError(t, err)

		t.Run(fmt.Sprintf("promote %s space to %s tier", space.Name, tierToCheck), func(t *testing.T) {
			// when
			tiers.MoveSpaceToTier(t, hostAwait, space.Name, tierToCheck)

			// then
			VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers, "deactivate30", tierToCheck) // deactivate30 is the default UserTier
		})
	}
}

func TestUpdateNSTemplateTier(t *testing.T) {
	t.Parallel()
	// in this test, we have 2 groups of users, configured with their own tier (both using the "base" tier templates)
	// then, the first tier is updated with the "advanced" templates, whereas the second one is updated using the "baseextendedidling" templates
	// finally, all user namespaces are verified.
	// So, in this test, we verify that namespace resources and cluster resources are updated, on 2 groups of users with different tiers ;)

	count := 2*MaxPoolSize + 1
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// we will have a lot of usersignups who are affected by the tier updates, so
	// we need to increase the timeouts on assertions/awaitilities to allow for all resources to be updated
	hostAwait = hostAwait.WithRetryOptions(wait.TimeoutOption(hostAwait.Timeout + time.Second*time.Duration(3*count*2)))       // 3 batches of `count` accounts, with 2s of interval between each update
	memberAwait = memberAwait.WithRetryOptions(wait.TimeoutOption(memberAwait.Timeout + time.Second*time.Duration(3*count*2))) // 3 batches of `count` accounts, with 2s of interval between each update

	baseTier, err := hostAwait.WaitForNSTemplateTier(t, "base")
	require.NoError(t, err)
	advancedTier, err := hostAwait.WaitForNSTemplateTier(t, "advanced")
	require.NoError(t, err)
	baseextendedidlingTier, err := hostAwait.WaitForNSTemplateTier(t, "baseextendedidling")
	require.NoError(t, err)

	// create new NSTemplateTiers (derived from `base`)
	cheesecakeTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "cheesecake", baseTier)
	cookieTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "cookie", baseTier)
	chocolateTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "chocolate", baseTier)

	// first group of users: the "cheesecake users"
	cheesecakeUsers := setupAccounts(t, awaitilities, cheesecakeTier, "cheesecakeuser%02d", memberAwait, count)
	// second group of users: the "cookie users"
	cookieUsers := setupAccounts(t, awaitilities, cookieTier, "cookieuser%02d", memberAwait, count)
	// setup chocolate tier to be used for creating spaces
	spaces := setupSpaces(t, awaitilities, chocolateTier, "chocolateuser%02d", memberAwait, count)

	t.Log("verifying new users and spaces")
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cheesecakeUsers, cheesecakeTier)
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cookieUsers, cookieTier)
	verifyResourceUpdatesForSpaces(t, hostAwait, memberAwait, spaces, chocolateTier)

	t.Log("updating tiers")
	// when updating the "cheesecakeTier" tier with the "advanced" template refs for namespace resources
	cheesecakeTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, cheesecakeTier, tiers.WithNamespaceResources(t, advancedTier), tiers.WithSpaceRoles(t, advancedTier))
	// and when updating the "cookie" tier with the "baseextendedidling" template refs for both namespace resources and cluster-wide resources
	cookieTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, cookieTier, tiers.WithNamespaceResources(t, baseextendedidlingTier), tiers.WithClusterResources(t, baseextendedidlingTier))
	// and when updating the "chocolate" tier to the "advanced" template refs for namespace resources
	chocolateTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, chocolateTier, tiers.WithNamespaceResources(t, advancedTier))

	// then
	t.Log("verifying users and spaces after tier updates")
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cheesecakeUsers, cheesecakeTier)
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cookieUsers, cookieTier)
	verifyResourceUpdatesForSpaces(t, hostAwait, memberAwait, spaces, chocolateTier)

	// finally, verify the counters in the status.history for both 'cheesecake' and 'cookie' tiers
	// cheesecake tier
	// there should be 2 entries in the status.history (1 create + 1 update)
	verifyStatus(t, hostAwait, "cheesecake", 2)

	// cookie tier
	// there should be 2 entries in the status.history (1 create + 1 update)
	verifyStatus(t, hostAwait, "cookie", 2)

	// chocolate tier
	// there should be 2 entries in the status.history (1 create + 1 update)
	verifyStatus(t, hostAwait, "chocolate", 2)
}

func TestResetDeactivatingStateWhenPromotingUser(t *testing.T) {
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	t.Run("test reset deactivating state when promoting user", func(t *testing.T) {
		user := NewSignupRequest(awaitilities).
			Username("promoteuser").
			Email("promoteuser@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)
		// Set the deactivating state on the UserSignup
		updatedUserSignup, err := hostAwait.UpdateUserSignup(t, user.UserSignup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivating(us, true)
			})
		require.NoError(t, err)

		// Move the MUR to the user tier with longer deactivation time
		tiers.MoveMURToTier(t, hostAwait, updatedUserSignup.Spec.IdentityClaims.PreferredUsername, "deactivate90")

		// Ensure the deactivating state is reset after promotion
		promotedUserSignup, err := hostAwait.WaitForUserSignup(t, updatedUserSignup.Name)
		require.NoError(t, err)
		require.False(t, states.Deactivating(promotedUserSignup), "usersignup should not be deactivating")
		VerifyResourcesProvisionedForSignup(t, awaitilities, promotedUserSignup, "deactivate90", "base")
	})
}

// setupSpaces takes care of:
// 1. creating a new tier with the provided tierName and using the TemplateRefs of the provided tier.
// 2. creating `count` number of spaces
func setupSpaces(t *testing.T, awaitilities wait.Awaitilities, tier *tiers.CustomNSTemplateTier, nameFmt string, targetCluster *wait.MemberAwaitility, count int) []string {
	var spaces []string
	for i := 0; i < count; i++ {
		name := fmt.Sprintf(nameFmt, i)
		s, _, _ := CreateSpace(t, awaitilities, testspace.WithName(name), testspace.WithTierNameAndHashLabelFor(tier.NSTemplateTier), testspace.WithSpecTargetCluster(targetCluster.ClusterName))
		spaces = append(spaces, s.Name)
	}
	return spaces
}

// setupAccounts takes care of:
// 1. creating a new tier with the TemplateRefs of the "base" tier.
// 2. creating 10 users (signups, MURs, etc.)
// 3. promoting the users to the new tier
// returns the tier, users and their "syncIndexes"
func setupAccounts(t *testing.T, awaitilities wait.Awaitilities, tier *tiers.CustomNSTemplateTier, nameFmt string, targetCluster *wait.MemberAwaitility, count int) []*toolchainv1alpha1.UserSignup {
	// first, let's create the a new NSTemplateTier (to avoid messing with other tiers)
	hostAwait := awaitilities.Host()

	// let's create a few users (more than `maxPoolSize`)
	// and wait until they are all provisioned by calling EnsureMUR()
	userSignups := make([]*toolchainv1alpha1.UserSignup, count)
	for i := 0; i < count; i++ {
		user := NewSignupRequest(awaitilities).
			Username(fmt.Sprintf(nameFmt, i)).
			ManuallyApprove().
			WaitForMUR().
			UserID(uuid.Must(uuid.NewV4()).String()).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			TargetCluster(targetCluster).
			Execute(t)
		userSignups[i] = user.UserSignup
	}

	// let's promote to users the new tier
	for i := range userSignups {
		VerifyResourcesProvisionedForSignup(t, awaitilities, userSignups[i], "deactivate30", "base")
		username := fmt.Sprintf(nameFmt, i)
		tiers.MoveSpaceToTier(t, hostAwait, username, tier.Name)
	}
	return userSignups
}

func verifyStatus(t *testing.T, hostAwait *wait.HostAwaitility, tierName string, expectedCount int) {
	_, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, tierName, wait.UntilNSTemplateTierStatusUpdates(expectedCount))
	require.NoError(t, err)
}

func verifyResourceUpdatesForUserSignups(t *testing.T, hostAwait *wait.HostAwaitility, memberAwaitility *wait.MemberAwaitility, userSignups []*toolchainv1alpha1.UserSignup, tier *tiers.CustomNSTemplateTier) {
	// if there's an annotation that describes on which other tier this one is based (for e2e tests only)
	for _, usersignup := range userSignups {
		userAccount, err := memberAwaitility.WaitForUserAccount(t, usersignup.Status.CompliantUsername,
			wait.UntilUserAccountHasConditions(wait.Provisioned()),
			wait.UntilUserAccountHasSpec(ExpectedUserAccount(usersignup.Spec.IdentityClaims.PropagatedClaims)),
			wait.UntilUserAccountMatchesMur(hostAwait))
		require.NoError(t, err)
		require.NotNil(t, userAccount)

		nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(t, usersignup.Status.CompliantUsername, wait.UntilNSTemplateSetHasTier(tier.Name))
		if err != nil {
			t.Logf("getting NSTemplateSet '%s' failed with: %s", usersignup.Status.CompliantUsername, err)
		}
		require.NoError(t, err, "Failing \nUserSignup: %+v \nUserAccount: %+v \nNSTemplateSet: %+v", usersignup, userAccount, nsTemplateSet)

		// verify space and tier resources are correctly updated
		VerifyResourcesProvisionedForSpaceWithCustomTier(t, hostAwait, memberAwaitility, usersignup.Status.CompliantUsername, tier)
	}
}

func verifyResourceUpdatesForSpaces(t *testing.T, hostAwait *wait.HostAwaitility, targetCluster *wait.MemberAwaitility, spaces []string, tier *tiers.CustomNSTemplateTier) {
	// verify individual space updates
	for _, spaceName := range spaces {
		VerifyResourcesProvisionedForSpaceWithCustomTier(t, hostAwait, targetCluster, spaceName, tier)
	}
}

func TestTierTemplates(t *testing.T) {
	t.Parallel()
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	selector := labels.NewSelector()
	e2eProducer, err := labels.NewRequirement("producer", selection.NotEquals, []string{"toolchain-e2e"})
	require.NoError(t, err)
	notCreatedByE2e := client.MatchingLabelsSelector{
		Selector: selector.Add(*e2eProducer),
	}
	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.TierTemplateList{}
	err = hostAwait.Client.List(context.TODO(), allTiers, client.InNamespace(hostAwait.Namespace), notCreatedByE2e)
	require.NoError(t, err)
	// We have 27 tier templates (base: 3, base1ns: 2, base1nsnoidling: 2, base1ns6didler: 3, baselarge: 3, baseextendedidling: 3, advanced: 3, test: 3, appstudio: 3, appstudiolarge: 3, appstudio-env: 3)
	// But we cannot verify the exact number of tiers, because during the operator update it may happen that more TierTemplates are created
	assert.GreaterOrEqual(t, len(allTiers.Items), 27)
}

func TestKsctlGeneratedTiers(t *testing.T) {
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	for _, tierName := range []string{"appstudio", "appstudio-env", "appstudiolarge"} {

		t.Run("for tier "+tierName, func(t *testing.T) {
			tier, err := hostAwait.WaitForNSTemplateTier(t, tierName)
			require.NoError(t, err)
			assert.Equal(t, "ksctl", tier.Annotations["generated-by"])

			refs := tiers.GetTemplateRefs(t, hostAwait, tierName).Flatten()

			for _, ref := range refs {
				tierTemplate, err := hostAwait.WaitForTierTemplate(t, ref)
				require.NoError(t, err)
				assert.Equal(t, "ksctl", tierTemplate.Annotations["generated-by"], "templateRef", ref)
			}
		})
	}
}

func TestFeatureToggles(t *testing.T) {
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	baseTier, err := hostAwait.WaitForNSTemplateTier(t, "base1ns")
	require.NoError(t, err)

	t.Run("provision space with enabled feature", func(t *testing.T) {
		// given

		// Create a new tier which is a copy of base1ns but with an additional ClusterRoleBinding object with "test-feature" annotation.
		// "feature-test" feature is defined in the ToolchainConfig and has 100 weight
		tier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "featuredtier", baseTier, withClusterRoleBinding(t, baseTier, "test-feature"))
		_, err := hostAwait.WaitForNSTemplateTier(t, tier.Name)
		require.NoError(t, err)

		// when

		// Now let's create a Space with this tier.
		user := NewSignupRequest(awaitilities).
			Username("featured-user").
			Email("featured@domain.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			SpaceTier(tier.Name).
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)

		// then

		// Verify that the space has the feature annotation
		space, err := hostAwait.WaitForSpace(t, user.Space.Name)
		require.NoError(t, err)
		require.NotEmpty(t, space.Annotations)
		assert.Equal(t, "test-feature", space.Annotations[toolchainv1alpha1.FeatureToggleNameAnnotationKey])
		// and CRB for the that feature has been created
		crb := &v1.ClusterRoleBinding{}
		nsName := fmt.Sprintf("%s-dev", user.Space.Name)
		crbName := fmt.Sprintf("%s-%s", user.Space.Name, "test-feature")
		err = memberAwait.WaitForObject(t, nsName, crbName, crb)
		require.NoError(t, err)

		t.Run("disable feature", func(t *testing.T) {
			// when

			// Now let's disable the feature for the Space by removing the feature annotation
			an := space.Annotations
			delete(an, toolchainv1alpha1.FeatureToggleNameAnnotationKey)
			space, err := hostAwait.UpdateSpace(t, user.Space.Name, func(s *toolchainv1alpha1.Space) {
				s.Annotations = an
			})
			require.NoError(t, err)

			// then
			err = memberAwait.WaitForObjectDeleted(t, nsName, crbName, crb)
			require.NoError(t, err)

			t.Run("re-enable feature", func(t *testing.T) {
				// when

				// Now let's re-enable the feature for the Space by restoring the feature annotation
				an := space.Annotations
				if an == nil {
					an = make(map[string]string)
				}
				an[toolchainv1alpha1.FeatureToggleNameAnnotationKey] = "test-feature"
				_, err := hostAwait.UpdateSpace(t, user.Space.Name, func(s *toolchainv1alpha1.Space) {
					s.Annotations = an
				})
				require.NoError(t, err)

				// then
				// Verify that the CRB is back
				crb := &v1.ClusterRoleBinding{}
				err = memberAwait.WaitForObject(t, nsName, crbName, crb)
				require.NoError(t, err)
			})
		})
	})
}

func withClusterRoleBinding(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier, feature string) tiers.CustomNSTemplateTierModifier {
	var tpl bytes.Buffer
	err := template.Must(template.New("crb").Parse(viewCRB)).Execute(&tpl, map[string]interface{}{
		"featureName": feature,
	})
	require.NoError(t, err)

	modifiers := []tiers.TierTemplateModifier{
		func(awaitility *wait.HostAwaitility, template *toolchainv1alpha1.TierTemplate) error {
			clusterRB := runtime.RawExtension{
				Raw: tpl.Bytes(),
			}
			template.Spec.Template.Objects = append(template.Spec.Template.Objects, clusterRB)
			return nil
		},
	}
	return tiers.WithClusterResources(t, otherTier, modifiers...)
}

var viewCRB = `
- apiVersion: rbac.authorization.k8s.io/v1
  kind: ClusterRoleBinding
  metadata:
    name: ${SPACE_NAME}-{{ .featureName }}
    annotations:
      toolchain.dev.openshift.com/feature: {{ .featureName }}
  roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: view
  subjects:
    - kind: User
      name: ${USERNAME}
`
