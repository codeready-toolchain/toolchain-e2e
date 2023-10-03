package testsupport

import (
	"context"
	"fmt"
	"testing"
	"time"

	testsupportsb "github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
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
	space := VerifySpaceRelatedResources(t, awaitilities, signup, spaceTierName)
	spaceMember := GetSpaceTargetMember(t, awaitilities, space)
	VerifyUserRelatedResources(t, awaitilities, signup, userTierName, ExpectUserAccountIn(spaceMember))
}

func VerifyResourcesProvisionedForSignupWithoutSpace(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, userTierName string) {
	VerifyUserRelatedResources(t, awaitilities, signup, userTierName, NoUserAccount())

	// verify space does not exist
	space, err := awaitilities.Host().WithRetryOptions(wait.TimeoutOption(3*time.Second)).WaitForSpace(t, signup.Status.CompliantUsername)
	require.Error(t, err)
	require.Nil(t, space)
}

type UserAccountOption struct {
	expectUserAccount bool
	targetCluster     *wait.MemberAwaitility
}

func NoUserAccount() UserAccountOption {
	return UserAccountOption{}
}

func ExpectAnyUserAccount() UserAccountOption {
	return UserAccountOption{
		expectUserAccount: true,
	}
}

func ExpectUserAccountIn(targetCluster *wait.MemberAwaitility) UserAccountOption {
	return UserAccountOption{
		expectUserAccount: true,
		targetCluster:     targetCluster,
	}
}

func VerifyUserRelatedResources(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, tierName string, userAccountOption UserAccountOption) (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord) {

	hostAwait := awaitilities.Host()
	// Get the latest signup version, wait for usersignup to have the approved label and wait for the complete status to
	// ensure the compliantusername is available
	userSignup, err := hostAwait.WaitForUserSignup(t, signup.Name,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
		wait.ContainsCondition(wait.Complete()))
	require.NoError(t, err)

	userAccountStatusWaitCriterion := wait.UntilMasterUserRecordHasUserAccountStatusesInClusters()
	if userAccountOption.expectUserAccount {
		userAccountStatusWaitCriterion = wait.UntilMasterUserRecordHasAnyUserAccountStatus()
		if userAccountOption.targetCluster != nil {
			userAccountStatusWaitCriterion = wait.UntilMasterUserRecordHasUserAccountStatusesInClusters(userAccountOption.targetCluster.ClusterName)
		}
	}

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasTierName(tierName),
		wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()),
		userAccountStatusWaitCriterion)
	require.NoError(t, err)

	// Verify last target cluster annotation is set
	lastCluster, foundLastCluster := userSignup.Annotations[toolchainv1alpha1.UserSignupLastTargetClusterAnnotationKey]
	require.True(t, foundLastCluster)
	// todo - change that as soon as we set the last-target-cluster annotation based on the "home" Space location
	// todo - and as soon as we drop the embedded UserAccounts from MUR
	require.Equal(t, mur.Spec.UserAccounts[0].TargetCluster, lastCluster)

	if userAccountOption.expectUserAccount {
		verifyUserAccount(t, awaitilities, userSignup, mur, userAccountOption)
	} else {
		for _, memberAwait := range awaitilities.AllMembers() {
			// let's verify that the UserAccounts from all members are gone
			err := memberAwait.WaitUntilUserDeleted(t, mur.Name)
			require.NoError(t, err)
		}
	}

	return userSignup, mur
}

func verifyUserAccount(t *testing.T, awaitilities wait.Awaitilities, userSignup *toolchainv1alpha1.UserSignup, mur *toolchainv1alpha1.MasterUserRecord, userAccountOption UserAccountOption) {
	hostAwait := awaitilities.Host()
	memberAwait := userAccountOption.targetCluster
	if memberAwait == nil {
		memberAwait = GetMurTargetMember(t, awaitilities, mur)
	}

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(t, mur.Name,
		wait.UntilUserAccountHasConditions(wait.Provisioned()),
		wait.UntilUserAccountHasSpec(ExpectedUserAccount(userSignup.Spec.IdentityClaims.PropagatedClaims)),
		wait.UntilUserAccountHasLabelWithValue(toolchainv1alpha1.TierLabelKey, mur.Spec.TierName),
		wait.UntilUserAccountHasAnnotation(toolchainv1alpha1.UserEmailAnnotationKey, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]),
		wait.UntilUserAccountMatchesMur(hostAwait))
	require.NoError(t, err)
	require.NotNil(t, userAccount)

	// Check the originalSub identity
	originalSubIdentityName := ""
	if userAccount.Spec.OriginalSub != "" {
		originalSubIdentityName = identitypkg.NewIdentityNamingStandard(userAccount.Spec.OriginalSub, "rhd").IdentityName()
	}

	// Check the UserID identity
	userIDIdentityName := ""
	val, ok := userAccount.Annotations[toolchainv1alpha1.SSOUserIDAnnotationKey]
	if ok {
		userIDIdentityName = identitypkg.NewIdentityNamingStandard(val, "rhd").IdentityName()
	}

	memberConfiguration := memberAwait.GetMemberOperatorConfig(t)

	// Verify User and Identity if SkipUserCreation is not set or it is set to false
	if memberConfiguration.Spec.SkipUserCreation == nil || !*memberConfiguration.Spec.SkipUserCreation {
		// Verify provisioned User
		user, err := memberAwait.WaitForUser(t, userAccount.Name,
			wait.UntilUserHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
			wait.UntilUserHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name),
			wait.UntilUserHasAnnotation(toolchainv1alpha1.UserEmailAnnotationKey, userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]))
		assert.NoError(t, err, fmt.Sprintf("no user with name '%s' found", userAccount.Name))

		userID, found := userSignup.Annotations[toolchainv1alpha1.SSOUserIDAnnotationKey]
		if found {
			accountID, found := userSignup.Annotations[toolchainv1alpha1.SSOAccountIDAnnotationKey]
			if found && userID != "" && accountID != "" {
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
			wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name))
		assert.NoError(t, err, fmt.Sprintf("no identity with name '%s' found", identityName))

		// Verify the originalSub identity
		if originalSubIdentityName != "" {
			_, err = memberAwait.WaitForIdentity(t, originalSubIdentityName,
				wait.UntilIdentityHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
				wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name))
			assert.NoError(t, err, fmt.Sprintf("no encoded identity with name '%s' found", identityName))
		}

		// Verify the userID identity
		if userIDIdentityName != "" {
			_, err = memberAwait.WaitForIdentity(t, userIDIdentityName,
				wait.UntilIdentityHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
				wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name))
			assert.NoError(t, err, fmt.Sprintf("no encoded identity with name '%s' found", identityName))
		}
	} else {
		// we don't expect User nor Identity resources to be present for AppStudio tier
		// This can be removed as soon as we don't create UserAccounts in AppStudio environment.
		err := memberAwait.WaitUntilUserDeleted(t, userAccount.Name)
		assert.NoError(t, err)
		err = memberAwait.WaitUntilIdentityDeleted(t, identitypkg.NewIdentityNamingStandard(userAccount.Spec.UserID, "rhd").IdentityName())
		assert.NoError(t, err)
		// Verify the originalSub identity
		if originalSubIdentityName != "" {
			err = memberAwait.WaitUntilIdentityDeleted(t, originalSubIdentityName)
			assert.NoError(t, err)
		}
		// Verify the userID identity
		if userIDIdentityName != "" {
			err = memberAwait.WaitUntilIdentityDeleted(t, userIDIdentityName)
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
			Name:        memberCluster.Name,
			APIEndpoint: memberCluster.Spec.APIEndpoint,
			ConsoleURL:  memberAwait.GetConsoleURL(t),
		},
		UserAccountStatus: userAccount.Status,
	}
	_, err = hostAwait.WaitForMasterUserRecord(t, mur.Name,
		wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()),
		wait.UntilMasterUserRecordHasUserAccountStatuses(expectedEmbeddedUaStatus))
	assert.NoError(t, err)
}

func VerifySpaceRelatedResources(t *testing.T, awaitilities wait.Awaitilities, userSignup *toolchainv1alpha1.UserSignup, spaceTierName string) *toolchainv1alpha1.Space {

	hostAwait := awaitilities.Host()

	userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
		wait.ContainsCondition(wait.Complete()))
	require.NoError(t, err)

	tier, err := hostAwait.WaitForNSTemplateTier(t, spaceTierName)
	require.NoError(t, err)
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	space, err := hostAwait.WaitForSpace(t, userSignup.Status.CompliantUsername,
		wait.UntilSpaceHasTier(spaceTierName),
		wait.UntilSpaceHasLabelWithValue(toolchainv1alpha1.SpaceCreatorLabelKey, userSignup.Name),
		wait.UntilSpaceHasLabelWithValue(fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", spaceTierName), hash),
		wait.UntilSpaceHasConditions(wait.Provisioned()),
		wait.UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValueClusterAssigned),
		wait.UntilSpaceHasAnyTargetClusterSet())
	require.NoError(t, err)

	mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasConditions(wait.Provisioned(), wait.ProvisionedNotificationCRCreated()),
		wait.UntilMasterUserRecordHasUserAccountStatusesInClusters(space.Spec.TargetCluster))
	require.NoError(t, err)

	testsupportsb.VerifySpaceBinding(t, hostAwait, mur.Name, space.Name, "admin")

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

	return space
}

func ExpectedUserAccount(claims toolchainv1alpha1.PropagatedClaims) toolchainv1alpha1.UserAccountSpec {
	return toolchainv1alpha1.UserAccountSpec{
		UserID:           claims.Sub,
		Disabled:         false,
		OriginalSub:      claims.OriginalSub,
		PropagatedClaims: claims,
	}
}

func GetMurTargetMember(t *testing.T, awaitilities wait.Awaitilities, mur *toolchainv1alpha1.MasterUserRecord) *wait.MemberAwaitility {
	for _, member := range awaitilities.AllMembers() {
		for _, ua := range mur.Status.UserAccounts {
			if ua.Cluster.Name == member.ClusterName {
				return member
			}
		}
	}

	require.FailNowf(t, "Unable to find a target member cluster", "MasterUserRecord: %+v", mur)
	return nil
}

func GetSpaceTargetMember(t *testing.T, awaitilities wait.Awaitilities, space *toolchainv1alpha1.Space) *wait.MemberAwaitility {
	for _, member := range awaitilities.AllMembers() {
		if space.Spec.TargetCluster == member.ClusterName {
			return member
		}
	}

	require.FailNowf(t, "Unable to find a target member cluster", "Space: %+v", space)
	return nil
}

func DeletedRoleAndAwaitRecreation(t *testing.T, memberAwait *wait.MemberAwaitility, ns corev1.Namespace, role string) {
	userRole, err := memberAwait.WaitForRole(t, &ns, role)
	require.NoError(t, err)
	require.NotEmpty(t, userRole)
	require.Contains(t, userRole.Labels, toolchainv1alpha1.SpaceLabelKey)

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
	require.Contains(t, userRoleBinding.Labels, toolchainv1alpha1.SpaceLabelKey)

	//when rolebinding deleted
	err = memberAwait.Client.Delete(context.TODO(), userRoleBinding)
	require.NoError(t, err)

	// then verify role is recreated
	userRoleBinding, err = memberAwait.WaitForRoleBinding(t, &ns, rolebinding)
	require.NoError(t, err)
	require.NotEmpty(t, userRoleBinding)
}
