package testsupport

import (
	"encoding/base64"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
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

func VerifyResourcesProvisionedForSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup,
	tier string) {

	hostAwait := awaitilities.Host()
	templateRefs := tiers.GetTemplateRefs(hostAwait, tier)
	// Get the latest signup version, wait for usersignup to have the approved label and wait for the complete status to
	// ensure the compliantusername is available
	userSignup, err := hostAwait.WaitForUserSignup(signup.Name,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved),
		wait.ContainsCondition(Complete()))
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)

	memberAwait := GetMurTargetMember(t, awaitilities, mur)

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(Provisioned()),
		wait.UntilUserAccountHasSpec(ExpectedUserAccount(userSignup.Spec.Userid, tier, templateRefs, userSignup.Spec.OriginalSub)),
		wait.UntilUserAccountMatchesMur(hostAwait))
	require.NoError(t, err)
	require.NotNil(t, userAccount)

	// Verify last target cluster annotation is set
	lastCluster, foundLastCluster := userSignup.Annotations[toolchainv1alpha1.UserSignupLastTargetClusterAnnotationKey]
	require.True(t, foundLastCluster)
	require.Equal(t, memberAwait.ClusterName, lastCluster)

	// Verify provisioned User
	_, err = memberAwait.WaitForUser(userAccount.Name)
	assert.NoError(t, err)

	// Verify provisioned Identity
	_, err = memberAwait.WaitForIdentity(ToIdentityName(userAccount.Spec.UserID))
	assert.NoError(t, err)

	// Verify second (and third if relevant) identities also
	if userAccount.Spec.OriginalSub != "" {
		// Verify
		encodedName := fmt.Sprintf("b64:%s", base64.StdEncoding.EncodeToString([]byte(userAccount.Spec.OriginalSub)))
		_, err = memberAwait.WaitForIdentity(ToIdentityName(encodedName))
		assert.NoError(t, err)

		unpaddedName := fmt.Sprintf("b64:%s", base64.RawStdEncoding.EncodeToString([]byte(userAccount.Spec.OriginalSub)))
		if unpaddedName != encodedName {
			_, err = memberAwait.WaitForIdentity(ToIdentityName(unpaddedName))
			assert.NoError(t, err)
		}
	}

	tiers.VerifyNsTemplateSet(t, hostAwait, memberAwait, userAccount, tier)

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

func ExpectedUserAccount(userID string, tier string, templateRefs tiers.TemplateRefs, originalSub string) toolchainv1alpha1.UserAccountSpec {
	namespaces := make([]toolchainv1alpha1.NSTemplateSetNamespace, 0, len(templateRefs.Namespaces))
	for _, ref := range templateRefs.Namespaces {
		namespaces = append(namespaces, toolchainv1alpha1.NSTemplateSetNamespace{
			Template:    "", // must be empty
			TemplateRef: ref,
		})
	}
	var clusterResources *toolchainv1alpha1.NSTemplateSetClusterResources
	if templateRefs.ClusterResources != nil {
		clusterResources = &toolchainv1alpha1.NSTemplateSetClusterResources{
			TemplateRef: tier + "-" + "clusterresources" + "-" + *templateRefs.ClusterResources,
		}
	}
	return toolchainv1alpha1.UserAccountSpec{
		UserID:   userID,
		Disabled: false,
		UserAccountSpecBase: toolchainv1alpha1.UserAccountSpecBase{
			NSLimit: "default",
			NSTemplateSet: toolchainv1alpha1.NSTemplateSetSpec{
				TierName:         tier,
				Namespaces:       namespaces,
				ClusterResources: clusterResources,
			},
		},
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
