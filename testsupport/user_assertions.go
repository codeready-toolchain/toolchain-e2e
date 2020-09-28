package testsupport

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func VerifyMultipleSignups(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, signups []toolchainv1alpha1.UserSignup) {
	for _, signup := range signups {
		VerifyResourcesProvisionedForSignup(t, hostAwait, memberAwait, signup, "basic")
	}
}

func VerifyResourcesProvisionedForSignup(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, signup toolchainv1alpha1.UserSignup, tier string) {
	templateRefs := tiers.GetTemplateRefs(hostAwait, tier)
	// Get the latest signup version
	userSignup, err := hostAwait.WaitForUserSignup(signup.Name)
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(Provisioned()),
		wait.UntilUserAccountHasSpec(ExpectedUserAccount(userSignup.Name, tier, templateRefs)),
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
			ConsoleURL:  ExpectedConsoleURL(t, memberAwait, memberCluster),
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

func ExpectedConsoleURL(t *testing.T, memberAwait *wait.MemberAwaitility, cluster toolchainv1alpha1.ToolchainCluster) string {
	// If OpenShift 3.x console available then we expect its URL in the status
	consoleURL := openShift3XConsoleURL(cluster.Spec.APIEndpoint)
	if consoleURL == "" {
		// Expect OpenShift 4.x console URL
		route, err := memberAwait.GetConsoleRoute()
		require.NoError(t, err)
		consoleURL = fmt.Sprintf("https://%s/%s", route.Spec.Host, route.Spec.Path)
	}
	return consoleURL
}

// openShift3XConsoleURL checks if <apiEndpoint>/console URL is reachable.
// This URL is used by web console in OpenShift 3.x
func openShift3XConsoleURL(apiEndpoint string) string {
	client := http.Client{
		Timeout: time.Duration(1 * time.Second),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	url := fmt.Sprintf("%s/console", apiEndpoint)
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer func() {
		_, _ = ioutil.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	return url
}
