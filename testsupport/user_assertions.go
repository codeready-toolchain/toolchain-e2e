package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyMultipleSignups(t *testing.T, hostAwait *wait.HostAwaitility, signups []toolchainv1alpha1.UserSignup, members ...*wait.MemberAwaitility) {
	for _, signup := range signups {
		VerifyResourcesProvisionedForSignup(t, hostAwait, signup, "basic", members...)
	}
}

func VerifyResourcesProvisionedForSignup(t *testing.T, hostAwait *wait.HostAwaitility, signup toolchainv1alpha1.UserSignup, tier string, members ...*wait.MemberAwaitility) {
	templateRefs := tiers.GetTemplateRefs(hostAwait, tier)
	// Get the latest signup version
	userSignup, err := hostAwait.WaitForUserSignup(signup.Name, wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueApproved))
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)

	memberAwait := getMurTargetMember(t, mur, members)

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(Provisioned()),
		wait.UntilUserAccountHasSpec(ExpectedUserAccount(userSignup.Spec.UserID, tier, templateRefs)),
		wait.UntilUserAccountMatchesMur(hostAwait))
	require.NoError(t, err)
	require.NotNil(t, userAccount)

	// Verify provisioned User
	_, err = memberAwait.WaitForUser(userAccount.Name)
	assert.NoError(t, err)

	// Verify provisioned Identity
	_, err = memberAwait.WaitForIdentity(ToIdentityName(userAccount.Spec.UserID))
	assert.NoError(t, err)

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

func ExpectedUserAccount(userID string, tier string, templateRefs tiers.TemplateRefs) v1alpha1.UserAccountSpec {
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
	return v1alpha1.UserAccountSpec{
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
	}
}

func getMurTargetMember(t *testing.T, mur *toolchainv1alpha1.MasterUserRecord, members []*wait.MemberAwaitility) *wait.MemberAwaitility {
	for _, member := range members {
		for _, ua := range mur.Spec.UserAccounts {
			if ua.TargetCluster == member.ClusterName {
				return member
			}
		}
	}

	require.FailNowf(t, "Unable to find a target member cluster", "MasterUserRecord: %+v", mur)
	return nil
}
