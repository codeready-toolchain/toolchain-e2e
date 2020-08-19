package testsupport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func CreateMultipleSignups(t *testing.T, ctx *framework.Context, awaitility *wait.Awaitility, capacity int) []toolchainv1alpha1.UserSignup {
	signups := make([]toolchainv1alpha1.UserSignup, capacity)
	for i := 0; i < capacity; i++ {
		name := fmt.Sprintf("multiple-signup-testuser-%d", i)
		// check if there is already a MUR with the expected name, in which case, continue with the next one
		mur := toolchainv1alpha1.MasterUserRecord{}
		if err := awaitility.Host().Client.Get(context.TODO(), types.NamespacedName{Namespace: awaitility.HostNs, Name: name}, &mur); err == nil {
			t.Logf("no need to create a UserSignup for '%s', the MasterUserRecord resource already exists", name)
			// skip this one, it already exists
			continue
		}
		// Create an approved UserSignup resource
		userSignup := NewUserSignup(t, awaitility.Host(), name, fmt.Sprintf("multiple-signup-testuser-%d@test.com", i))
		userSignup.Spec.Approved = true
		err := awaitility.Host().Client.Create(context.TODO(), userSignup, CleanupOptions(ctx))
		awaitility.T.Logf("created usersignup with username: '%s' and resource name: '%s'", userSignup.Spec.Username, userSignup.Name)
		require.NoError(t, err)
		signups[i] = *userSignup
	}
	return signups
}

func CreateAndApproveSignup(t *testing.T, awaitility *wait.Awaitility, username string) toolchainv1alpha1.UserSignup {
	// 1. Create a UserSignup resource via calling registration service
	identity := &authsupport.Identity{
		ID:       uuid.NewV4(),
		Username: username,
	}
	postSignup(t, awaitility.RegistrationServiceURL, *identity)

	// at this stage, the usersignup should not be approved nor completed
	userSignup, err := awaitility.Host().WaitForUserSignup(identity.ID.String(), wait.UntilUserSignupHasConditions(PendingApproval()...))
	require.NoError(t, err)
	require.Equal(t, userSignup.Spec.GivenName, identity.Username+"-First-Name")
	require.Equal(t, userSignup.Spec.FamilyName, identity.Username+"-Last-Name")
	require.Equal(t, userSignup.Spec.Company, identity.Username+"-Company-Name")

	// 2. approve the UserSignup
	userSignup.Spec.Approved = true
	err = awaitility.Host().Client.Update(context.TODO(), userSignup)
	require.NoError(t, err)
	// Check the updated conditions
	_, err = awaitility.Host().WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(ApprovedByAdmin()...))
	require.NoError(t, err)

	return *userSignup
}

func NewUserSignup(t *testing.T, host *wait.HostAwaitility, username string, email string) *toolchainv1alpha1.UserSignup {
	memberCluster, ok, err := host.GetToolchainCluster(cluster.Member, wait.ReadyToolchainCluster)
	require.NoError(t, err)
	require.True(t, ok)

	return &toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.NewV4().String(),
			Namespace: host.Ns,
			Annotations: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailAnnotationKey: email,
			},
			Labels: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: toolchainv1alpha1.UserSignupSpec{
			Username:      username,
			TargetCluster: memberCluster.Name,
		},
	}
}

var HttpClient = &http.Client{
	Timeout: time.Second * 10,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
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
	client := HttpClient
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
}

func ToIdentityName(userID string) string {
	return fmt.Sprintf("%s:%s", "rhd", userID)
}
