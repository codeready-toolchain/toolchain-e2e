package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kubefed/pkg/apis/core/v1beta1"
)

func TestE2EFlow(t *testing.T) {
	// given
	// full flow from usersignup with approval down to namespaces creation
	ctx, awaitility := testsupport.WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	defer ctx.Cleanup()

	// host and member cluster statuses should be available at this point
	t.Run("verify cluster statuses are valid", func(t *testing.T) {
		t.Run("verify member cluster status", func(t *testing.T) {
			verifyMemberStatus(t, awaitility.Member())
		})
	})

	// Create multiple accounts and let them get provisioned while we are executing the main flow for "johnsmith" and "extrajohn"
	// We will verify them in the end of the test
	signups := createMultipleSignups(t, ctx, awaitility, 5)

	// Create and approve "johnsmith" and "extrajohn" signups
	johnsmithName := "johnsmith"
	johnSignup := createAndApproveSignup(t, awaitility, johnsmithName)
	extrajohnName := "extrajohn"
	johnExtraSignup := createAndApproveSignup(t, awaitility, extrajohnName)

	verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
	verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")

	t.Run("try to break UserAccount", func(t *testing.T) {

		t.Run("delete user and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), user)

			// then
			require.NoError(t, err)
			verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete identity and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(johnSignup.Name)}, identity)
			require.NoError(t, err)

			// when
			err = awaitility.Client.Delete(context.TODO(), identity)

			// then
			require.NoError(t, err)
			verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete user mapping and wait until recreated", func(t *testing.T) {
			// given
			user := &userv1.User{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: johnsmithName}, user)
			require.NoError(t, err)

			// when
			user.Identities = []string{}
			err = awaitility.Client.Update(context.TODO(), user)

			// then
			require.NoError(t, err)
			verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete identity mapping and wait until recreated", func(t *testing.T) {
			// given
			identity := &userv1.Identity{}
			err := awaitility.Client.Get(context.TODO(), types.NamespacedName{Name: toIdentityName(johnSignup.Name)}, identity)
			require.NoError(t, err)
			identity.User = corev1.ObjectReference{Name: "", UID: ""}

			// when
			err = awaitility.Client.Update(context.TODO(), identity)

			// then
			require.NoError(t, err)
			verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})

		t.Run("delete namespaces and wait until recreated", func(t *testing.T) {
			// given
			namespaces := make([]*corev1.Namespace, 0, 3)
			templateRefs := tiers.GetTemplateRefs(awaitility.Host(), "basic")
			for _, ref := range templateRefs.Namespaces {
				ns, err := awaitility.Member().WaitForNamespace(johnSignup.Spec.Username, ref)
				require.NoError(t, err)
				namespaces = append(namespaces, ns)
			}
			// when
			for _, ns := range namespaces {
				err := awaitility.Client.Delete(context.TODO(), ns)
				require.NoError(t, err)
			}

			// then
			// wait for the namespaces to be re-created before validating all other resources to avoid race condition
			for _, ref := range templateRefs.Namespaces {
				_, err := awaitility.Member().WaitForNamespace(johnSignup.Spec.Username, ref)
				require.NoError(t, err)
			}
			verifyResourcesProvisionedForSignup(t, awaitility, johnSignup, "basic")
			verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
		})
	})

	t.Run("delete usersignup and expect all resources to be deleted", func(t *testing.T) {
		// given
		hostAwait := wait.NewHostAwaitility(awaitility)
		memberAwait := wait.NewMemberAwaitility(awaitility)
		johnSignup, err := hostAwait.WaitForUserSignup(johnSignup.Name)
		require.NoError(t, err)

		// when
		err = awaitility.Client.Delete(context.TODO(), johnSignup)

		// then
		require.NoError(t, err)
		t.Logf("usersignup '%s' deleted (resource name='%s')", johnsmithName, johnSignup.Name)

		err = hostAwait.WaitUntilMasterUserRecordDeleted(johnsmithName)
		assert.NoError(t, err, "MasterUserRecord is not deleted")

		err = memberAwait.WaitUntilUserAccountDeleted(johnsmithName)
		assert.NoError(t, err, "UserAccount is not deleted")

		err = memberAwait.WaitUntilUserDeleted(johnsmithName)
		assert.NoError(t, err, "User is not deleted")

		err = memberAwait.WaitUntilIdentityDeleted(johnsmithName)
		assert.NoError(t, err, "Identity is not deleted")

		err = memberAwait.WaitUntilNSTemplateSetDeleted(johnsmithName)
		assert.NoError(t, err, "NSTemplateSet id not deleted")

		err = memberAwait.WaitUntilClusterResourceQuotasDeleted(johnsmithName)
		assert.NoError(t, err, "ClusterResourceQuotas were not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "code")
		assert.NoError(t, err, "johnsmith-code namespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "dev")
		assert.NoError(t, err, "johnsmith-dev namespace is not deleted")

		err = memberAwait.WaitUntilNamespaceDeleted(johnsmithName, "stage")
		assert.NoError(t, err, "johnsmith-stage namespace is not deleted")

		// also, verify that other user's resource are left intact
		verifyResourcesProvisionedForSignup(t, awaitility, johnExtraSignup, "basic")
	})

	t.Run("multiple MasterUserRecord resources provisioned", func(t *testing.T) {
		// Now when the main flow has been tested we can verify the signups we created in the very beginning
		verifyMultipleSignups(t, awaitility, signups)
	})
}

func createAndApproveSignup(t *testing.T, awaitility *wait.Awaitility, username string) toolchainv1alpha1.UserSignup {
	// 1. Create a UserSignup resource via calling registration service
	identity := &authsupport.Identity{
		ID:       uuid.NewV4(),
		Username: username,
	}
	postSignup(t, awaitility.RegistrationServiceURL, *identity)

	// at this stage, the usersignup should not be approved nor completed
	userSignup, err := awaitility.Host().WaitForUserSignup(identity.ID.String(), wait.UntilUserSignupHasConditions(pendingApproval()...))
	require.NoError(t, err)
	require.Equal(t, userSignup.Spec.GivenName, identity.Username+"-First-Name")
	require.Equal(t, userSignup.Spec.FamilyName, identity.Username+"-Last-Name")
	require.Equal(t, userSignup.Spec.Company, identity.Username+"-Company-Name")

	// 2. approve the UserSignup
	userSignup.Spec.Approved = true
	err = awaitility.Host().Client.Update(context.TODO(), userSignup)
	require.NoError(t, err)
	// Check the updated conditions
	_, err = awaitility.Host().WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedByAdmin()...))
	require.NoError(t, err)

	return *userSignup
}

func postSignup(t *testing.T, route string, identity authsupport.Identity) {
	// Call signup endpoint with a valid token.
	emailClaim := authsupport.WithEmailClaim(uuid.NewV4().String() + "@email.tld")
	givenNameClaim := authsupport.WithGivenNameClaim(identity.Username + "-First-Name")
	familyNameClaim := authsupport.WithFamilyNameClaim(identity.Username + "-Last-Name")
	companyClaim := authsupport.WithCompanyClaim(identity.Username + "-Company-Name")
	iatClaim := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))
	token, err := authsupport.GenerateSignedE2ETestToken(identity, emailClaim, companyClaim, givenNameClaim, familyNameClaim, iatClaim)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")
	client := httpClient

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
}

func expectedUserAccount(userID string, tier string, templateRefs tiers.TemplateRefs) v1alpha1.UserAccountSpec {
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

func createMultipleSignups(t *testing.T, ctx *framework.Context, awaitility *wait.Awaitility, capacity int) []toolchainv1alpha1.UserSignup {
	signups := make([]toolchainv1alpha1.UserSignup, capacity)
	for i := 0; i < capacity; i++ {
		// Create an approved UserSignup resource
		userSignup := newUserSignup(t, awaitility.Host(), fmt.Sprintf("multiple-signup-testuser-%d", i), fmt.Sprintf("multiple-signup-testuser-%d@test.com", i))
		userSignup.Spec.Approved = true
		err := awaitility.Host().Client.Create(context.TODO(), userSignup, testsupport.CleanupOptions(ctx))
		awaitility.T.Logf("created usersignup with username: '%s' and resource name: '%s'", userSignup.Spec.Username, userSignup.Name)
		require.NoError(t, err)
		signups[i] = *userSignup
	}
	return signups
}

func verifyMultipleSignups(t *testing.T, awaitility *wait.Awaitility, signups []toolchainv1alpha1.UserSignup) {
	for _, signup := range signups {
		verifyResourcesProvisionedForSignup(t, awaitility, signup, "basic")
	}
}

func verifyResourcesProvisionedForSignup(t *testing.T, awaitility *wait.Awaitility, signup toolchainv1alpha1.UserSignup, tier string) {
	hostAwait := wait.NewHostAwaitility(awaitility)
	memberAwait := wait.NewMemberAwaitility(awaitility)
	templateRefs := tiers.GetTemplateRefs(hostAwait, tier)
	// Get the latest signup version
	userSignup, err := awaitility.Host().WaitForUserSignup(signup.Name)
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(t, err)

	// Then wait for the associated UserAccount to be provisioned
	userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
		wait.UntilUserAccountHasConditions(provisioned()),
		wait.UntilUserAccountHasSpec(expectedUserAccount(userSignup.Name, tier, templateRefs)),
		wait.UntilUserAccountMatchesMur(hostAwait))
	require.NoError(t, err)
	require.NotNil(t, userAccount)

	// Verify provisioned User
	_, err = memberAwait.WaitForUser(userAccount.Name)
	assert.NoError(t, err)

	// Verify provisioned Identity
	_, err = memberAwait.WaitForIdentity(toIdentityName(userAccount.Spec.UserID))
	assert.NoError(t, err)

	tiers.VerifyNsTemplateSet(t, awaitility, userAccount, tier)

	// Get member cluster to verify that it was used to provision user accounts
	memberCluster, ok, err := hostAwait.GetKubeFedCluster(cluster.Member, nil)
	require.NoError(t, err)
	require.True(t, ok)

	// Then finally check again the MasterUserRecord with the expected (embedded) UserAccount status, on top of the other criteria
	expectedEmbeddedUaStatus := toolchainv1alpha1.UserAccountStatusEmbedded{
		Cluster: toolchainv1alpha1.Cluster{
			Name:        mur.Spec.UserAccounts[0].TargetCluster,
			APIEndpoint: memberCluster.Spec.APIEndpoint,
			ConsoleURL:  expectedConsoleURL(t, memberAwait, memberCluster),
		},
		UserAccountStatus: userAccount.Status,
	}
	_, err = hostAwait.WaitForMasterUserRecord(mur.Name,
		wait.UntilMasterUserRecordHasConditions(provisioned(), provisionedNotificationCRCreated()),
		wait.UntilMasterUserRecordHasUserAccountStatuses(expectedEmbeddedUaStatus))
	assert.NoError(t, err)
}

func verifyMemberStatus(t *testing.T, memberAwait *wait.MemberAwaitility) {
	_, err := memberAwait.WaitForMemberStatus(wait.UntilMemberStatusHasConditions(memberStatusReady()))
	require.NoError(t, err, "failed while waiting for toolchain member status")
}

func toIdentityName(userID string) string {
	return fmt.Sprintf("%s:%s", "testIdP", userID)
}

func expectedConsoleURL(t *testing.T, memberAwait *wait.MemberAwaitility, cluster v1beta1.KubeFedCluster) string {
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
