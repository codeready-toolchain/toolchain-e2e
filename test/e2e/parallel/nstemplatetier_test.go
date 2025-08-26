package parallel

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"testing"
	"time"

	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/gofrs/uuid"
	"k8s.io/client-go/kubernetes/scheme"

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
	tiersToCheck := wait.AllE2eNSTemplateTiers

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
	VerifyResourcesProvisionedForSignup(t, awaitilities, testingtiers)

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
			VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, testingtiers, "deactivate30", tierToCheck) // deactivate30 is the default UserTier
		})
	}
}

func TestUpdateNSTemplateTier(t *testing.T) {
	t.Parallel()
	// in this test, we have 2 groups of users, configured with their own tier (both using the "base1ns" tier templates)
	// then, the first tier is updated with the "base1nsnoidling" templates, whereas the second one is updated using the "base1ns6didler" templates
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

	baseTier, err := hostAwait.WaitForNSTemplateTier(t, "base1ns")
	require.NoError(t, err)
	base1nsnoidlingTier, err := hostAwait.WaitForNSTemplateTier(t, "base1nsnoidling")
	require.NoError(t, err)
	base1ns6dilderTier, err := hostAwait.WaitForNSTemplateTier(t, "base1ns6didler")
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
	// when updating the "cheesecakeTier" tier with the "base1nsnoidling" template refs for namespace resources and spaceroles
	cheesecakeTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, cheesecakeTier, tiers.WithNamespaceResources(t, base1nsnoidlingTier), tiers.WithSpaceRoles(t, base1nsnoidlingTier))
	// and when updating the "cookie" tier with the "base1ns6didler" template refs for both namespace resources and cluster-wide resources
	cookieTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, cookieTier, tiers.WithNamespaceResources(t, base1ns6dilderTier), tiers.WithClusterResources(t, base1ns6dilderTier))
	// and when updating the "chocolate" tier to the "base1nsnoidling" template refs for namespace resources
	chocolateTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, chocolateTier, tiers.WithNamespaceResources(t, base1nsnoidlingTier))

	// then
	t.Log("verifying users and spaces after tier updates")
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cheesecakeUsers, cheesecakeTier)
	verifyResourceUpdatesForUserSignups(t, hostAwait, memberAwait, cookieUsers, cookieTier)
	verifyResourceUpdatesForSpaces(t, hostAwait, memberAwait, spaces, chocolateTier)
}

// TestGoTemplate verifies that a go-template NSTemplateTier can be used to provision a user successfully.
// This test validates the complete workflow of using Go templates in NSTemplateTier configurations
// and verifies that all expected resources are correctly provisioned using the custom tier configuration
func TestGoTemplate(t *testing.T) {
	t.Parallel()

	//given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	base1Ns, err := hostAwait.WaitForNSTemplateTier(t, "base1ns")
	require.NoError(t, err)
	base1nsGoTemplateTier, err := hostAwait.WaitForNSTemplateTier(t, "base1ns-gotemplate")
	require.NoError(t, err)

	//when
	user := NewSignupRequest(awaitilities).
		Username("gotemplateuser").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		WaitForMUR().
		UserID(uuid.Must(uuid.NewV4()).String()).
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	VerifyResourcesProvisionedForSignup(t, awaitilities, user.UserSignup)
	tiers.MoveSpaceToTier(t, hostAwait, user.UserSignup.Name, base1nsGoTemplateTier.Name)

	//then
	verifyResourceUpdatesForUserSignups(t, hostAwait, awaitilities.Member1(), []*toolchainv1alpha1.UserSignup{user.UserSignup}, &tiers.CustomNSTemplateTier{
		NSTemplateTier:         base1nsGoTemplateTier,
		ClusterResourcesTier:   base1Ns,
		NamespaceResourcesTier: base1Ns,
		SpaceRolesTier:         base1Ns,
	})
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
		updatedUserSignup, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(user.UserSignup.Name, hostAwait.Namespace,
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
		VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, promotedUserSignup, "deactivate90", "base1ns")
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
// 1. creating a new tier with the TemplateRefs of the "base1ns" tier.
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
		VerifyResourcesProvisionedForSignup(t, awaitilities, userSignups[i])
		username := fmt.Sprintf(nameFmt, i)
		tiers.MoveSpaceToTier(t, hostAwait, username, tier.Name)
	}
	return userSignups
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
	// We have 19 tier templates (base: 3, base1ns: 2, base1nsnoidling: 2, base1ns6didler: 3, appstudio: 3, appstudiolarge: 3, appstudio-env: 3)
	// But we cannot verify the exact number of tiers, because during the operator update it may happen that more TierTemplates are created
	assert.GreaterOrEqual(t, len(allTiers.Items), 19)
}

func TestFeatureToggles(t *testing.T) {
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	base1nsTier, err := hostAwait.WaitForNSTemplateTier(t, "base1ns")
	require.NoError(t, err)

	t.Run("provision space with enabled feature", func(t *testing.T) {
		// given

		// Create a new tier which is a copy of base1ns but with an additional ClusterRoleBinding object with "test-feature" annotation.
		// "feature-test" feature is defined in the ToolchainConfig and has 100 weight
		tier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "ftier", base1nsTier,
			withClusterRoleBindings(t, base1nsTier, "test-feature"),
			tiers.WithNamespaceResources(t, base1nsTier),
			tiers.WithSpaceRoles(t, base1nsTier))
		_, err := hostAwait.WaitForNSTemplateTier(t, tier.Name)
		require.NoError(t, err)
		// when

		// Now let's create a Space
		user := NewSignupRequest(awaitilities).
			Username("featured-user").
			Email("featured@domain.com").
			ManuallyApprove().
			EnsureMUR().
			SpaceTier("base1ns").
			TargetCluster(memberAwait).
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)
		// and promote that space to the ftier tier
		tiers.MoveSpaceToTier(t, hostAwait, "featured-user", tier.Name)
		VerifyResourcesProvisionedForSpaceWithCustomTier(t, hostAwait, memberAwait, "featured-user", tier)

		// then

		// Verify that the space has the feature annotation - the weight is set to 100, so it should be added to all Spaces in all tiers
		space, err := hostAwait.WaitForSpace(t, user.Space.Name)
		require.NoError(t, err)
		require.NotEmpty(t, space.Annotations)
		assert.Equal(t, "test-feature", space.Annotations[toolchainv1alpha1.FeatureToggleNameAnnotationKey])
		// and CRB for the that feature has been created
		crbName := fmt.Sprintf("%s-%s", user.Space.Name, "test-feature")
		_, err = wait.For(t, memberAwait.Awaitility, &v1.ClusterRoleBinding{}).WithNameThat(crbName)
		require.NoError(t, err)
		// the noise CRB for unknown/disabled feature is not created
		noiseCrbName := fmt.Sprintf("%s-%s", user.Space.Name, unknownFeature)
		err = wait.For(t, memberAwait.Awaitility, &v1.ClusterRoleBinding{}).WithNameDeleted(noiseCrbName)
		require.NoError(t, err)

		t.Run("disable feature", func(t *testing.T) {
			// when

			// Now let's disable the feature for the Space by removing the feature annotation
			_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
				Update(user.Space.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
					delete(s.Annotations, toolchainv1alpha1.FeatureToggleNameAnnotationKey)
				})

			require.NoError(t, err)

			// then
			err = wait.For(t, memberAwait.Awaitility, &v1.ClusterRoleBinding{}).WithNameDeleted(crbName)
			require.NoError(t, err)
			err = wait.For(t, memberAwait.Awaitility, &v1.ClusterRoleBinding{}).WithNameDeleted(noiseCrbName)
			require.NoError(t, err)

			t.Run("re-enable feature", func(t *testing.T) {
				// when

				// Now let's re-enable the feature for the Space by restoring the feature annotation
				_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.Space{}).
					Update(user.Space.Name, hostAwait.Namespace, func(s *toolchainv1alpha1.Space) {
						if s.Annotations == nil {
							s.Annotations = make(map[string]string)
						}
						s.Annotations[toolchainv1alpha1.FeatureToggleNameAnnotationKey] = "test-feature"
					})

				require.NoError(t, err)

				// then
				// Verify that the CRB is back
				_, err = wait.For(t, memberAwait.Awaitility, &v1.ClusterRoleBinding{}).WithNameThat(crbName)
				require.NoError(t, err)
				// the noise CRB for unknown/disabled feature is still not created
				err = wait.For(t, memberAwait.Awaitility, &v1.ClusterRoleBinding{}).WithNameDeleted(noiseCrbName)
				require.NoError(t, err)
			})
		})
	})
}

func TestTierTemplateRevision(t *testing.T) {
	t.Parallel()

	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	// we create new NSTemplateTiers (derived from `base`)
	baseTier, err := hostAwait.WaitForNSTemplateTier(t, "base1ns")
	require.NoError(t, err)
	// for the tiertemplaterevisions to be created the tiertemplates need to have template objects populated
	// we add the RawExtension objects to the TemplateObjects field
	crq := getTestCRQ("600")
	rawTemplateObjects := []runtime.RawExtension{{Object: &crq}}
	updateTierTemplateObjects := func(template *toolchainv1alpha1.TierTemplate) error {
		template.Spec.TemplateObjects = rawTemplateObjects
		return nil
	}
	// for simplicity, we add the CRQ to all types of templates (both cluster scope and namespace scoped),
	// even if the CRQ is cluster scoped.
	// WARNING: thus THIS NSTemplateTier should NOT be used to provision a user!!!
	customTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "ttr", baseTier,
		tiers.WithNamespaceResources(t, baseTier, updateTierTemplateObjects),
		tiers.WithClusterResources(t, baseTier, updateTierTemplateObjects),
		tiers.WithSpaceRoles(t, baseTier, updateTierTemplateObjects),
		tiers.WithParameter("DEPLOYMENT_QUOTA", "60"))
	// when
	// we verify that TierTemplateRevision CRs were created, since all the tiertemplates now have templateObjects field populated
	tier, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, "ttr",
		wait.HasStatusTierTemplateRevisionKeys())
	require.NoError(t, err)
	customTier.NSTemplateTier = tier

	// then
	// check the expected total number of ttr matches,
	// we IDEALLY expect one TTR per each tiertemplate to be created (clusterresource, namespace and spacerole), thus a total of 3 TTRs ideally.
	// But since the creation of a TTR could be very quick and could trigger another reconcile of the NSTemplateTier before the status is actually updated with the reference,
	// this might generate some copies of the TTRs. This is not a problem in production since the cleanup mechanism of TTRs will remove the extra ones but could cause some flakiness with the test,
	// thus we assert the number of TTRs doesn't exceed the double of the expected number.
	// TODO check for exact match or remove the *2 and check for not empty revisions list, once we implement the cleanup controller
	ttrs, err := hostAwait.WaitForTTRs(t, customTier.Name, wait.LessOrEqual(len(tiers.GetTemplateRefs(t, hostAwait, "ttr").Flatten())*2))
	require.NoError(t, err)

	t.Run("update of tiertemplate should trigger creation of new TTR", func(t *testing.T) {
		// given
		// that the tiertemplates and nstemlpatetier are provisioned from the parent test
		ttrToBeModified, found := customTier.Status.Revisions[customTier.Spec.ClusterResources.TemplateRef]
		require.True(t, found)
		// check that it has the crq before updating it
		checkThatTTRContainsCRQ(t, ttrToBeModified, ttrs, crq)

		// when
		// we update one tiertemplate
		// let's reduce the pod count
		updatedCRQ := getTestCRQ("100")
		_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.TierTemplate{}).
			Update(customTier.Spec.ClusterResources.TemplateRef, hostAwait.Namespace, func(tiertemplate *toolchainv1alpha1.TierTemplate) {
				tiertemplate.Spec.TemplateObjects = []runtime.RawExtension{{Object: &updatedCRQ}}
			})
		require.NoError(t, err)

		// then
		// get the updated nstemplatetier
		updatedCustomTier, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.NSTemplateTier{}).
			WithNameMatching(customTier.Name, func(actual *toolchainv1alpha1.NSTemplateTier) bool {
				newTTR, found := actual.Status.Revisions[actual.Spec.ClusterResources.TemplateRef]
				return found && newTTR != "" && newTTR != ttrToBeModified
			})
		newTTR := updatedCustomTier.Status.Revisions[updatedCustomTier.Spec.ClusterResources.TemplateRef]

		// check that it has the updated crq
		// a new TTR was created
		updatedTTRs, err := hostAwait.WaitForTTRs(t, customTier.Name, wait.GreaterOrEqual(len(ttrs))) //there is already a cleanup controller
		require.NoError(t, err)
		checkThatTTRContainsCRQ(t, newTTR, updatedTTRs, updatedCRQ)

		t.Run("update of the NSTemplateTier parameters should trigger creation of new TTR", func(t *testing.T) {
			// given
			// that the TierTemplates and NSTemplateTier are provisioned from the parent test
			// and they have the initial parameter value for deployment quota
			checkThatTTRsHaveParameter(t, customTier, updatedTTRs, toolchainv1alpha1.Parameter{
				Name:  "DEPLOYMENT_QUOTA",
				Value: "60",
			})

			// when
			// we increase the parameter for the deployment quota
			customTier = tiers.UpdateCustomNSTemplateTier(t, hostAwait, customTier, tiers.WithParameter("DEPLOYMENT_QUOTA", "100"))
			require.NoError(t, err)

			// then
			// retrieve new tier once the ttrs were created and the revision field updated
			customTier.NSTemplateTier, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.NSTemplateTier{}).
				WithNameMatching(customTier.Name, func(actual *toolchainv1alpha1.NSTemplateTier) bool {
					for _, oldValue := range updatedCustomTier.Status.Revisions {
						for _, actualValue := range actual.Status.Revisions {
							if oldValue == actualValue {
								return false
							}
						}
					}
					return true
				})
			require.NoError(t, err)
			// an additional TTRs will be created
			ttrsWithNewParams, err := hostAwait.WaitForTTRs(t, customTier.Name, wait.GreaterOrEqual(len(ttrs))) //there is already a cleanup controller
			require.NoError(t, err)
			// and the parameter is updated in all the ttrs
			checkThatTTRsHaveParameter(t, customTier, ttrsWithNewParams, toolchainv1alpha1.Parameter{
				Name:  "DEPLOYMENT_QUOTA",
				Value: "100",
			})
		})
	})

	t.Run("when updating one tiertemplate the revisions field should be cleaned up from old entries", func(t *testing.T) {
		// given
		// we create new NSTemplateTiers (derived from `base`)
		updatingTier := tiers.CreateCustomNSTemplateTier(t, hostAwait, "updatingtier", baseTier)
		// we use the base1nsnoidling tier only for copying the namespace and space role resources
		base1nsnoidlingTier, err := hostAwait.WaitForNSTemplateTier(t, "base1nsnoidling")
		require.NoError(t, err)

		// when
		// we verify that the new tier exists and the revisions field was populated
		tier, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, "updatingtier",
			wait.HasStatusTierTemplateRevisionKeys())
		require.NoError(t, err)
		updatingTier.NSTemplateTier = tier
		revisionsBeforeUpdate := updatingTier.Status.Revisions
		// and we update the tier with the "base1nsnoidling" template refs for namespace and space role resources
		tiers.UpdateCustomNSTemplateTier(t, hostAwait, updatingTier, tiers.WithNamespaceResources(t, base1nsnoidlingTier), tiers.WithSpaceRoles(t, base1nsnoidlingTier))

		// then
		// we ensure the new revisions are made by namespace and spaceroles from advanced tier + clusterResources from the updating tier
		advancedRefs := tiers.GetTemplateRefs(t, hostAwait, base1nsnoidlingTier.Name)
		expectedRefs := []string{updatingTier.Spec.ClusterResources.TemplateRef}
		// the duplicated tiertemplates have a different prefix
		for _, tierTemplateName := range advancedRefs.SpaceRolesFlatten() {
			expectedRefs = append(expectedRefs, tiers.DuplicatedTierName(updatingTier.Name, tierTemplateName))
		}
		for _, tierTemplateName := range advancedRefs.Namespaces {
			expectedRefs = append(expectedRefs, tiers.DuplicatedTierName(updatingTier.Name, tierTemplateName))
		}
		updatedTier, err := hostAwait.WaitForNSTemplateTierAndCheckTemplates(t, "updatingtier",
			wait.HasStatusTierTemplateRevisionKeys())
		require.NoError(t, err)
		// revisions values should be different compared to the previous ones
		assert.NotEqual(t, revisionsBeforeUpdate, updatedTier.Status.Revisions)
	})
}

func getTestCRQ(podsCount string) unstructured.Unstructured {
	crq := unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "ClusterResourceQuota",
		"metadata": map[string]interface{}{
			"name": "for-{{.SPACE_NAME}}-deployments",
		},
		"spec": map[string]interface{}{
			"quota": map[string]interface{}{
				"hard": map[string]interface{}{
					"count/deploymentconfigs.apps": "{{.DEPLOYMENT_QUOTA}}",
					"count/deployments.apps":       "{{.DEPLOYMENT_QUOTA}}",
					"count/pods":                   podsCount,
				},
			},
			"selector": map[string]interface{}{
				"annotations": map[string]interface{}{},
				"labels": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"toolchain.dev.openshift.com/space": "{{.SPACE_NAME}}",
					},
				},
			},
		},
	}}
	return crq
}

func withClusterRoleBindings(t *testing.T, otherTier *toolchainv1alpha1.NSTemplateTier, feature string) tiers.CustomNSTemplateTierModifier {
	clusterRB := getCRBforFeature(t, feature)       // This is the ClusterRoleBinding for the desired feature
	noiseCRB := getCRBforFeature(t, unknownFeature) // This is a noise CRB for unknown/disabled feature. To be used to check that this CRB is never created.

	return tiers.WithClusterResources(t, otherTier, func(template *toolchainv1alpha1.TierTemplate) error {
		template.Spec.Template.Objects = append(template.Spec.Template.Objects, clusterRB, noiseCRB)
		return nil
	})
}

func getCRBforFeature(t *testing.T, featureName string) runtime.RawExtension {
	var crb bytes.Buffer
	err := template.Must(template.New("crb").Parse(viewCRB)).Execute(&crb, map[string]interface{}{
		"featureName": featureName,
	})
	require.NoError(t, err)
	clusterRB := runtime.RawExtension{
		Raw: crb.Bytes(),
	}
	return clusterRB
}

const (
	unknownFeature = "unknown-feature"

	viewCRB = `{
  "apiVersion": "rbac.authorization.k8s.io/v1",
  "kind": "ClusterRoleBinding",
  "metadata": {
    "name": "${SPACE_NAME}-{{ .featureName }}",
    "annotations": {
      "toolchain.dev.openshift.com/feature": "{{ .featureName }}"
    }
  },
  "roleRef": {
    "apiGroup": "rbac.authorization.k8s.io",
    "kind": "ClusterRole",
    "name": "view"
  },
  "subjects": [
    {
      "kind": "User",
      "name": "${USERNAME}"
    }
  ]
}
`
)

// checkThatTTRContainsCRQ verifies if a given ttr from the list contains the CRQ in the templateObjects field
func checkThatTTRContainsCRQ(t *testing.T, ttrName string, ttrs []toolchainv1alpha1.TierTemplateRevision, crq unstructured.Unstructured) {
	for _, ttr := range ttrs {
		if ttr.Name == ttrName {
			assert.NotEmpty(t, ttr.Spec.TemplateObjects)
			unstructuredObj := &unstructured.Unstructured{}
			_, _, err := scheme.Codecs.UniversalDeserializer().Decode(ttr.Spec.TemplateObjects[0].Raw, nil, unstructuredObj)
			require.NoError(t, err)
			assert.Equal(t, &crq, unstructuredObj)
			return
		}
	}
	require.FailNowf(t, "Unable to find a TTR with required crq", "ttr:%s CRQ:%+v", ttrName, crq)
}

// checkThatTTRsHaveParameter verifies that ttrs from the list have the required parameter
func checkThatTTRsHaveParameter(t *testing.T, tier *tiers.CustomNSTemplateTier, ttrs []toolchainv1alpha1.TierTemplateRevision, parameters toolchainv1alpha1.Parameter) {
	for _, ttr := range ttrs {
		// if the ttr is still in the revisions field we check that it contains the required parameters
		if ttrNameInRev, ttrFound := tier.Status.Revisions[ttr.GetLabels()[toolchainv1alpha1.TemplateRefLabelKey]]; ttrFound && ttrNameInRev == ttr.Name {
			assert.Contains(t, ttr.Spec.Parameters, parameters, "Unable to find required parameters:%+v in the TTR:%s", parameters, ttr.Name)
			return
		}
	}
}
