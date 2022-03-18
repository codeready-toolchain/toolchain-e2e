package testsupport

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/codeready-toolchain/toolchain-common/pkg/identity"

	corev1 "k8s.io/api/core/v1"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	testtier "github.com/codeready-toolchain/toolchain-common/pkg/test/tier"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyMultipleSignups(t *testing.T, awaitilities wait.Awaitilities, signups []*toolchainv1alpha1.UserSignup) {
	for _, signup := range signups {
		VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "base")
	}
}

func VerifyResourcesProvisionedForSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, tierName string) {

	hostAwait := awaitilities.Host()
	// Get the latest signup version, wait for usersignup to have the approved label and wait for the complete status to
	// ensure the compliantusername is available
	userSignup, err := hostAwait.WaitForUserSignup(signup.Name,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
		wait.ContainsCondition(Complete()))
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername,
		wait.UntilMasterUserRecordHasTierName(tierName),
		wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)

	memberAwait := GetMurTargetMember(t, awaitilities, mur)

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
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
	encodedName := ""
	if userAccount.Spec.OriginalSub != "" {
		encodedName = fmt.Sprintf("b64:%s", base64.RawStdEncoding.EncodeToString([]byte(userAccount.Spec.OriginalSub)))
	}

	if tierName != "appstudio" {
		// Verify provisioned User
		_, err = memberAwait.WaitForUser(userAccount.Name,
			wait.UntilUserHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
			wait.UntilUserHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name),
			wait.UntilUserHasAnnotation(toolchainv1alpha1.UserEmailAnnotationKey, signup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]))
		assert.NoError(t, err, fmt.Sprintf("no user with name '%s' found", userAccount.Name))

		// Verify provisioned Identity
		identityName := identity.NewIdentityNamingStandard(userAccount.Spec.UserID, "rhd").IdentityName()

		_, err = memberAwait.WaitForIdentity(identityName,
			wait.UntilIdentityHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
			wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name))
		assert.NoError(t, err, fmt.Sprintf("no identity with name '%s' found", identityName))

		// Verify the second identity
		if encodedName != "" {
			_, err = memberAwait.WaitForIdentity(ToIdentityName(encodedName),
				wait.UntilIdentityHasLabel(toolchainv1alpha1.ProviderLabelKey, toolchainv1alpha1.ProviderLabelValue),
				wait.UntilIdentityHasLabel(toolchainv1alpha1.OwnerLabelKey, userAccount.Name))
			assert.NoError(t, err, fmt.Sprintf("no encoded identity with name '%s' found", ToIdentityName(encodedName)))
		}
	} else {
		// we don't expect User nor Identity resources to be present for AppStudio tier
		// This can be removed as soon as we don't create UserAccounts in AppStudio environment.
		err := memberAwait.WaitUntilUserDeleted(userAccount.Name)
		assert.NoError(t, err)
		err = memberAwait.WaitUntilIdentityDeleted(ToIdentityName(userAccount.Spec.UserID))
		assert.NoError(t, err)
		// Verify the second identity
		if encodedName != "" {
			err = memberAwait.WaitUntilIdentityDeleted(ToIdentityName(encodedName))
			assert.NoError(t, err)
		}
	}

	tier, err := hostAwait.WaitForNSTemplateTier(mur.Spec.TierName)
	require.NoError(t, err)
	hash, err := testtier.ComputeTemplateRefsHash(tier) // we can assume the JSON marshalling will always work
	require.NoError(t, err)

	space, err := hostAwait.WaitForSpace(mur.Name,
		wait.UntilSpaceHasTier(mur.Spec.TierName),
		wait.UntilSpaceHasLabelWithValue(toolchainv1alpha1.SpaceCreatorLabelKey, userSignup.Name),
		wait.UntilSpaceHasLabelWithValue(fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", mur.Spec.TierName), hash),
		wait.UntilSpaceHasConditions(Provisioned()),
		wait.UntilSpaceHasStateLabel(toolchainv1alpha1.SpaceStateLabelValueClusterAssigned),
		wait.UntilSpaceHasStatusTargetCluster(mur.Spec.UserAccounts[0].TargetCluster))
	require.NoError(t, err)

	VerifySpaceBinding(t, hostAwait, mur.Name, space.Name, "admin")

	// Verify provisioned NSTemplateSet
	nsTemplateSet, err := memberAwait.WaitForNSTmplSet(space.Name, wait.UntilNSTemplateSetHasTier(tier.Name))
	require.NoError(t, err)

	tierChecks, err := tiers.NewChecksForTier(tier)
	require.NoError(t, err)

	tiers.VerifyNSTemplateSet(t, memberAwait, nsTemplateSet, tierChecks, tierChecks.GetExpectedTemplateRefs(hostAwait))

	// Get member cluster to verify that it was used to provision user accounts
	memberCluster, ok, err := hostAwait.GetToolchainCluster(cluster.Member, memberAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, ok)

	// Then finally check again the MasterUserRecord with the expected (embedded) UserAccount status, on top of the other criteria
	expectedEmbeddedUaStatus := toolchainv1alpha1.UserAccountStatusEmbedded{
		Cluster: toolchainv1alpha1.Cluster{
			Name:        mur.Spec.UserAccounts[0].TargetCluster,
			APIEndpoint: memberCluster.Spec.APIEndpoint,
			ConsoleURL:  memberAwait.GetConsoleURL(),
		},
		UserAccountStatus: userAccount.Status,
	}
	_, err = hostAwait.WaitForMasterUserRecord(mur.Name,
		wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()),
		wait.UntilMasterUserRecordHasUserAccountStatuses(expectedEmbeddedUaStatus))
	assert.NoError(t, err)
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
	userRole, err := memberAwait.WaitForRole(&ns, role)
	require.NoError(t, err)
	require.NotEmpty(t, userRole)
	require.Contains(t, userRole.Labels, "toolchain.dev.openshift.com/owner")

	//when role deleted
	err = memberAwait.Client.Delete(context.TODO(), userRole)
	require.NoError(t, err)

	// then verify role is recreated
	userRole, err = memberAwait.WaitForRole(&ns, role)
	require.NoError(t, err)
	require.NotEmpty(t, userRole)
}

func DeleteRoleBindingAndAwaitRecreation(t *testing.T, memberAwait *wait.MemberAwaitility, ns corev1.Namespace, rolebinding string) {
	userRoleBinding, err := memberAwait.WaitForRoleBinding(&ns, rolebinding)
	require.NoError(t, err)
	require.NotEmpty(t, userRoleBinding)
	require.Contains(t, userRoleBinding.Labels, "toolchain.dev.openshift.com/owner")

	//when rolebinding deleted
	err = memberAwait.Client.Delete(context.TODO(), userRoleBinding)
	require.NoError(t, err)

	// then verify role is recreated
	userRoleBinding, err = memberAwait.WaitForRoleBinding(&ns, rolebinding)
	require.NoError(t, err)
	require.NotEmpty(t, userRoleBinding)
}
