package e2e

import (
	"context"
	"fmt"
	"github.com/gofrs/uuid"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

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
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()

	// Create and approve "testingtiers" signups
	testingTiersName := "testingtiers"
	testingtiers, _ := NewSignupRequest(awaitilities).
		Username(testingTiersName).
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t).
		Resources()

	// all tiers to check - keep the base as the last one, it will verify downgrade back to the default tier at the end of the test
	tiersToCheck := []string{"advanced", "baseextendedidling", "baselarge", "test", "appstudio", "appstudio-env", "base1ns", "base1nsnoidling", "base1ns6didler", "base"}

	// when the tiers are created during the startup then we can verify them
	allTiers := &toolchainv1alpha1.NSTemplateTierList{}
	err := hostAwait.Client.List(context.TODO(), allTiers, client.InNamespace(hostAwait.Namespace))
	require.NoError(t, err)
	assert.Len(t, allTiers.Items, len(tiersToCheck)) // temporarily remove this check because migration tests create basedeactivationdisabled, baseextended and hackathon tiers

	for _, tier := range allTiers.Items {
		assert.Contains(t, tiersToCheck, tier.Name)
	}

	// wait for the user to be provisioned for the first time
	VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers, "deactivate30", "base") // deactivate30 is the default UserTier and base is the default SpaceTier
	for _, tierToCheck := range tiersToCheck {

		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
		_, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, tierToCheck,
			UntilNSTemplateTierSpec(HasNoTemplateRefWithSuffix("-000000a")))
		require.NoError(t, err)

		t.Run(fmt.Sprintf("promote %s space to %s tier", testingTiersName, tierToCheck), func(t *testing.T) {
			// when
			tiers.MoveSpaceToTier(t, hostAwait, testingTiersName, tierToCheck)

			// then
			VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers, "deactivate30", tierToCheck) // deactivate30 is the default UserTier
		})
	}
}

func TestSetDefaultTier(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("original default tier", func(t *testing.T) {
		// Create and approve a new user that should be provisioned to the base tier
		NewSignupRequest(awaitilities).
			Username("defaulttier").
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t).
			Resources()
	})

	t.Run("changed default tier configuration", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultUserTier("deactivate30").DefaultSpaceTier("advanced"))
		// Create and approve a new user that should be provisioned to the advanced tier
		NewSignupRequest(awaitilities).
			Username("defaulttierchanged").
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t).
			Resources()
	})
}

func TestUpdateNSTemplateTier(t *testing.T) {
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
	hostAwait = hostAwait.WithRetryOptions(TimeoutOption(hostAwait.Timeout + time.Second*time.Duration(3*count*2)))       // 3 batches of `count` accounts, with 2s of interval between each update
	memberAwait = memberAwait.WithRetryOptions(TimeoutOption(memberAwait.Timeout + time.Second*time.Duration(3*count*2))) // 3 batches of `count` accounts, with 2s of interval between each update

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
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	t.Run("test reset deactivating state when promoting user", func(t *testing.T) {
		userSignup, _ := NewSignupRequest(awaitilities).
			Username("promoteuser").
			Email("promoteuser@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t).
			Resources()

		// Set the deactivating state on the UserSignup
		updatedUserSignup, err := hostAwait.UpdateUserSignup(t, userSignup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivating(us, true)
			})
		require.NoError(t, err)

		// Move the MUR to the user tier with longer deactivation time
		tiers.MoveMURToTier(t, hostAwait, updatedUserSignup.Spec.Username, "deactivate90")

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
func setupSpaces(t *testing.T, awaitilities Awaitilities, tier *tiers.CustomNSTemplateTier, nameFmt string, targetCluster *MemberAwaitility, count int) []string {
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
func setupAccounts(t *testing.T, awaitilities Awaitilities, tier *tiers.CustomNSTemplateTier, nameFmt string, targetCluster *MemberAwaitility, count int) []*toolchainv1alpha1.UserSignup {
	// first, let's create the a new NSTemplateTier (to avoid messing with other tiers)
	hostAwait := awaitilities.Host()

	// let's create a few users (more than `maxPoolSize`)
	// and wait until they are all provisioned by calling EnsureMUR()
	userSignups := make([]*toolchainv1alpha1.UserSignup, count)
	for i := 0; i < count; i++ {
		userSignups[i], _ = NewSignupRequest(awaitilities).
			Username(fmt.Sprintf(nameFmt, i)).
			ManuallyApprove().
			WaitForMUR().
			UserID(uuid.Must(uuid.NewV4()).String()).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			TargetCluster(targetCluster).
			Execute(t).
			Resources()
	}

	// let's promote to users the new tier
	for i := range userSignups {
		VerifyResourcesProvisionedForSignup(t, awaitilities, userSignups[i], "deactivate30", "base")
		username := fmt.Sprintf(nameFmt, i)
		tiers.MoveSpaceToTier(t, hostAwait, username, tier.Name)
	}
	return userSignups
}

func verifyStatus(t *testing.T, hostAwait *HostAwaitility, tierName string, expectedCount int) {
	_, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, tierName, UntilNSTemplateTierStatusUpdates(expectedCount))
	require.NoError(t, err)
}

func verifyResourceUpdatesForUserSignups(t *testing.T, hostAwait *HostAwaitility, memberAwaitility *MemberAwaitility, userSignups []*toolchainv1alpha1.UserSignup, tier *tiers.CustomNSTemplateTier) {
	// if there's an annotation that describes on which other tier this one is based (for e2e tests only)
	for _, usersignup := range userSignups {
		userAccount, err := memberAwaitility.WaitForUserAccount(t, usersignup.Status.CompliantUsername,
			UntilUserAccountHasConditions(wait.Provisioned()),
			UntilUserAccountHasSpec(ExpectedUserAccount(usersignup.Spec.IdentityClaims.Sub, usersignup.Spec.IdentityClaims.UserID,
				usersignup.Spec.IdentityClaims.AccountID,
				usersignup.Spec.OriginalSub, usersignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])),
			UntilUserAccountMatchesMur(hostAwait))
		require.NoError(t, err)
		require.NotNil(t, userAccount)

		nsTemplateSet, err := memberAwaitility.WaitForNSTmplSet(t, usersignup.Status.CompliantUsername, UntilNSTemplateSetHasTier(tier.Name))
		if err != nil {
			t.Logf("getting NSTemplateSet '%s' failed with: %s", usersignup.Status.CompliantUsername, err)
		}
		require.NoError(t, err, "Failing \nUserSignup: %+v \nUserAccount: %+v \nNSTemplateSet: %+v", usersignup, userAccount, nsTemplateSet)

		// verify space and tier resources are correctly updated
		VerifyResourcesProvisionedForSpaceWithCustomTier(t, hostAwait, memberAwaitility, usersignup.Status.CompliantUsername, tier)
	}
}

func verifyResourceUpdatesForSpaces(t *testing.T, hostAwait *HostAwaitility, targetCluster *MemberAwaitility, spaces []string, tier *tiers.CustomNSTemplateTier) {
	// verify individual space updates
	for _, spaceName := range spaces {
		VerifyResourcesProvisionedForSpaceWithCustomTier(t, hostAwait, targetCluster, spaceName, tier)
	}
}

func TestTierTemplates(t *testing.T) {
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
	// verify that we have 24 tier templates (base: 3, base1ns: 2, base1nsnoidling: 2, base1ns6didler: 3, baselarge: 3, baseextendedidling: 3, advanced: 3, test: 3, appstudio: 3, appstudio-env: 3)
	require.NoError(t, err)
	// we cannot verify the exact number of tiers, because during the operator update it may happen that more TierTemplates are created
	assert.True(t, len(allTiers.Items) >= 28)
}
