package testsupport

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	testtier "github.com/codeready-toolchain/toolchain-common/pkg/test/tier"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyMultipleSignups(t *testing.T, awaitilities wait.Awaitilities, signups []*toolchainv1alpha1.UserSignup) {
	for _, signup := range signups {
		VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "deactivate30", "base")
	}
}

func VerifyResourcesProvisionedForSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, userTierName, spaceTierName string) {
	VerifyUserRelatedResources(t, awaitilities, signup, userTierName)
	VerifySpaceRelatedResources(t, awaitilities, signup, spaceTierName)
}

func VerifyResourcesProvisionedForSignupWithoutSpace(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, userTierName string) {
	VerifyUserRelatedResources(t, awaitilities, signup, userTierName)

	// verify space does not exist
	space, err := awaitilities.Host().WithRetryOptions(wait.TimeoutOption(3*time.Second)).WaitForSpace(t, signup.Status.CompliantUsername)
	require.Error(t, err)
	require.Nil(t, space)
}

func VerifyUserRelatedResources(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, tierName string) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord) {

	hostAwait := awaitilities.Host()
	// Get the latest signup version, wait for usersignup to have the approved label and wait for the complete status to
	// ensure the compliantusername is available
	userSignup, err := hostAwait.WaitForUserSignup(t, signup.Name,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
		wait.ContainsCondition(Complete()))
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasTierName(tierName),
		wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)

	memberAwait := GetMurTargetMember(t, awaitilities, mur)

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(t, mur.Name,
		wait.UntilUserAccountHasConditions(Provisioned()),
		wait.UntilUserAccountHasSpec(ExpectedUserAccount(userSignup.Spec.Userid, userSignup.Spec.OriginalSub)),
		wait.UntilUserAccountHasLabelWithValue(toolchainv1alpha1.TierLabelKey, mur.Spec.TierName),
		wait.UntilUserAccountHasAnnotation(toolchainv1alpha1.UserEmailAnnotationKey, signup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]),
		wait.UntilUserAccountMatchesMur(hostAwait))
	require.NoError(t, err)
	require.NotNil(t, userAccount)

	// Verify last target cluster annotation is set
	lastCluster, foundLastCluster := userSignup.Annotations[toolchainv1alpha1.UserSignupLastTargetClusterAnnotationKey]
	require.True(t, foundLastCluster)
	require.Equal(t, memberAwait.ClusterName, lastCluster)

	// Check the second identity
	originalSubIdentityName := ""
	if userAccount.Spec.OriginalSub != "" {
		originalSubIdentityName = identitypkg.NewIdentityNamingStandard(userAccount.Spec.OriginalSub, "rhd").IdentityName()
	}

	memberConfiguration := memberAwait.GetMemberOperatorConfig(t)

	// Verify User and Identity if SkipUserCreation is not set or it is set to false
	if memberConfiguration.Spec.SkipUserCreation == nil || !*memberConfiguration.Spec.SkipUserCreation {
		// Verify provisioned User
		user, err := memberAwait.WaitForUser(t, userAccount.Name,
			wait.UntilUserHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
			wait.UntilUserHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name),
			wait.UntilUserHasLabel(toolchainv1alpha1.SpaceLabelKey, userAccount.Name),
			wait.UntilUserHasAnnotation(toolchainv1alpha1.UserEmailAnnotationKey, signup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]))
		assert.NoError(t, err, fmt.Sprintf("no user with name '%s' found", userAccount.Name))

		userID, found := userSignup.Annotations[toolchainv1alpha1.SSOUserIDAnnotationKey]
		if found {
			accountID, found := userSignup.Annotations[toolchainv1alpha1.SSOAccountIDAnnotationKey]
			if found {
				require.Equal(t, userID, user.Annotations[toolchainv1alpha1.SSOUserIDAnnotationKey])
				require.Equal(t, accountID, user.Annotations[toolchainv1alpha1.SSOAccountIDAnnotationKey])
			}
		}

		if !found {
			require.NotContains(t, user.Annotations, toolchainv1alpha1.SSOUserIDAnnotationKey)
			require.NotContains(t, user.Annotations, toolchainv1alpha1.SSOAccountIDAnnotationKey)
		}

		// Verify provisioned Identity
		identityName := identitypkg.NewIdentityNamingStandard(userAccount.Spec.UserID, "rhd").IdentityName()

		_, err = memberAwait.WaitForIdentity(t, identityName,
			wait.UntilIdentityHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
			wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name),
			wait.UntilIdentityHasLabel(toolchainv1alpha1.SpaceLabelKey, userAccount.Name))
		assert.NoError(t, err, fmt.Sprintf("no identity with name '%s' found", identityName))

		// Verify the second identity
		if originalSubIdentityName != "" {
			_, err = memberAwait.WaitForIdentity(t, identityName,
				wait.UntilIdentityHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
				wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name),
				wait.UntilIdentityHasLabel(toolchainv1alpha1.SpaceLabelKey, userAccount.Name))
			assert.NoError(t, err, fmt.Sprintf("no encoded identity with name '%s' found", identityName))
		}
	} else {
		// we don't expect User nor Identity resources to be present for AppStudio tier
		// This can be removed as soon as we don't create UserAccounts in AppStudio environment.
		err := memberAwait.WaitUntilUserDeleted(t, userAccount.Name)
		assert.NoError(t, err)
		err = memberAwait.WaitUntilIdentityDeleted(t, identitypkg.NewIdentityNamingStandard(userAccount.Spec.UserID, "rhd").IdentityName())
		assert.NoError(t, err)
		// Verify the second identity
		if originalSubIdentityName != "" {
			err = memberAwait.WaitUntilIdentityDeleted(t, originalSubIdentityName)
			assert.NoError(t, err)
		}
	}

	// Get member cluster to verify that it was used to provision user accounts
	memberCluster, ok, err := hostAwait.GetToolchainCluster(t, cluster.Member, memberAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, ok)

	// Then finally check again the MasterUserRecord with the expected (embedded) UserAccount status, on top of the other criteria
	expectedEmbeddedUaStatus := toolchainv1alpha1.UserAccountStatusEmbedded{
		Cluster: toolchainv1alpha1.Cluster{
			Name:        mur.Spec.UserAccounts[0].TargetCluster,
			APIEndpoint: memberCluster.Spec.APIEndpoint,
			ConsoleURL:  memberAwait.GetConsoleURL(t),
		},
		UserAccountStatus: userAccount.Status,
	}
	mur, err = hostAwait.WaitForMasterUserRecord(t, mur.Name,
		wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()),
		wait.UntilMasterUserRecordHasUserAccountStatuses(expectedEmbeddedUaStatus))
	assert.NoError(t, err)

	return userSignup, mur
}

func VerifySpaceRelatedResources(t *testing.T, awaitilities wait.Awaitilities, userSignup *toolchainv1alpha1.UserSignup, spaceTierName string) {

	hostAwait := awaitilities.Host()

	userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
		wait.ContainsCondition(Complete()))
	require.NoError(t, err)

	mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)

	tier, err := hostAwait.WaitForNSTemplateTier(t, spaceTierName)
	require.NoError(t, err)
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	space, err := hostAwait.WaitForSpace(t, mur.Name,
		wait.UntilSpaceHasTier(spaceTierName),
		wait.UntilSpaceHasLabelWithValue(toolchainv1alpha1.SpaceCreatorLabelKey, userSignup.Name),
		wait.UntilSpaceHasLabelWithValue(fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", spaceTierName), hash),
		wait.UntilSpaceHasConditions(Provisioned()),
		wait.UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValueClusterAssigned),
		wait.UntilSpaceHasStatusTargetCluster(mur.Spec.UserAccounts[0].TargetCluster))
	require.NoError(t, err)

	VerifySpaceBinding(t, hostAwait, mur.Name, space.Name, "admin")

	bindings, err := hostAwait.ListSpaceBindings(space.Name)
	require.NoError(t, err)
	memberAwait := GetMurTargetMember(t, awaitilities, mur)
	// Verify provisioned NSTemplateSet
	nsTemplateSet, err := memberAwait.WaitForNSTmplSet(t, space.Name,
		wait.UntilNSTemplateSetHasTier(tier.Name),
		wait.UntilNSTemplateSetHasSpaceRolesFromBindings(tier, bindings),
	)
	require.NoError(t, err)
	tierChecks, err := tiers.NewChecksForTier(tier)
	require.NoError(t, err)
	tiers.VerifyNSTemplateSet(t, hostAwait, memberAwait, nsTemplateSet, tierChecks)
}

func ExpectedUserAccount(userID string, originalSub string) toolchainv1alpha1.UserAccountSpec {
	return toolchainv1alpha1.UserAccountSpec{
		UserID:      userID,
		Disabled:    false,
		OriginalSub: originalSub,
	}
}

func GetMurTargetMember(t *testing.T, awaitilities wait.Awaitilities, mur *toolchainv1alpha1.MasterUserRecord) *wait.MemberAwaitility {
	for _, member := range awaitilities.AllMembers() {
		for _, ua := range mur.Spec.UserAccounts {
			if ua.TargetCluster == member.ClusterName {
				return member
			}
		}
	}

	require.FailNowf(t, "Unable to find a target member cluster", "MasterUserRecord: %+v", mur)
	return nil
}

func DeletedRoleAndAwaitRecreation(t *testing.T, memberAwait *wait.MemberAwaitility, ns corev1.Namespace, role string) {
	userRole, err := memberAwait.WaitForRole(t, &ns, role)
	require.NoError(t, err)
	require.NotEmpty(t, userRole)
	require.Contains(t, userRole.Labels, "toolchain.dev.openshift.com/owner")

	//when role deleted
	err = memberAwait.Client.Delete(context.TODO(), userRole)
	require.NoError(t, err)

	// then verify role is recreated
	userRole, err = memberAwait.WaitForRole(t, &ns, role)
	require.NoError(t, err)
	require.NotEmpty(t, userRole)
}

func DeleteRoleBindingAndAwaitRecreation(t *testing.T, memberAwait *wait.MemberAwaitility, ns corev1.Namespace, rolebinding string) {
	userRoleBinding, err := memberAwait.WaitForRoleBinding(t, &ns, rolebinding)
	require.NoError(t, err)
	require.NotEmpty(t, userRoleBinding)
	require.Contains(t, userRoleBinding.Labels, "toolchain.dev.openshift.com/owner")

	//when rolebinding deleted
	err = memberAwait.Client.Delete(context.TODO(), userRoleBinding)
	require.NoError(t, err)

	// then verify role is recreated
	userRoleBinding, err = memberAwait.WaitForRoleBinding(t, &ns, rolebinding)
	require.NoError(t, err)
	require.NotEmpty(t, userRoleBinding)
}
