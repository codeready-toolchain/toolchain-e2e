package testsupport

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dns1123Value string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
)

var dns1123ValueRegexp = regexp.MustCompile("^" + dns1123Value + "$")

func VerifyMultipleSignups(t *testing.T, awaitilities wait.Awaitilities, signups []*toolchainv1alpha1.UserSignup) {
	for _, signup := range signups {
		VerifyResourcesProvisionedForSignup(t, awaitilities, signup, "base")
	}
}

func VerifyResourcesProvisionedForSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup, tierName string) {

	hostAwait := awaitilities.Host()
	templateRefs := tiers.GetTemplateRefs(hostAwait, tierName)
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
		wait.UntilUserAccountHasSpec(ExpectedUserAccount(userSignup.Spec.Userid, tierName, templateRefs, userSignup.Spec.OriginalSub)),
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
		_, err = memberAwait.WaitForUser(userAccount.Name)
		assert.NoError(t, err)

		// Verify provisioned Identity
		userID := userAccount.Spec.UserID
		if !dns1123ValueRegexp.MatchString(userAccount.Spec.UserID) {
			userID = fmt.Sprintf("b64:%s", base64.RawStdEncoding.EncodeToString([]byte(userAccount.Spec.UserID)))
		}

		_, err = memberAwait.WaitForIdentity(ToIdentityName(userID))
		assert.NoError(t, err)

		// Verify the second identity
		if encodedName != "" {
			_, err = memberAwait.WaitForIdentity(ToIdentityName(encodedName))
			assert.NoError(t, err)
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

	tiers.VerifyNsTemplateSet(t, hostAwait, memberAwait, userAccount, tierName)

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
			NSTemplateSet: &toolchainv1alpha1.NSTemplateSetSpec{
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
