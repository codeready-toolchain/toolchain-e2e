package proxy

import (
	"context"
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	testsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	spacebinding "github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	userv1 "github.com/openshift/api/user/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubewait "k8s.io/apimachinery/pkg/util/wait"
)

type ProxyUser struct {
	ExpectedMemberCluster *wait.MemberAwaitility
	Username              string
	Token                 string
	IdentityID            uuid.UUID
	Signup                *toolchainv1alpha1.UserSignup
	CompliantUsername     string
}

func (u *ProxyUser) ShareSpaceWith(t *testing.T, hostAwait *wait.HostAwaitility, guestUser *ProxyUser) {
	// share primaryUser space with guestUser
	guestUserMur, err := hostAwait.GetMasterUserRecord(guestUser.CompliantUsername)
	require.NoError(t, err)
	primaryUserSpace, err := hostAwait.WaitForSpace(t, u.CompliantUsername, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)
	spacebinding.CreateSpaceBinding(t, hostAwait, guestUserMur, primaryUserSpace, "admin") // creating a spacebinding gives guestUser access to primaryUser's space
}

func (u *ProxyUser) ListWorkspaces(t *testing.T, hostAwait *wait.HostAwaitility) []toolchainv1alpha1.Workspace {
	proxyCl := u.CreateProxyClient(t, hostAwait)

	workspaces := &toolchainv1alpha1.WorkspaceList{}
	err := proxyCl.List(context.TODO(), workspaces)
	require.NoError(t, err)
	return workspaces.Items
}

func (u *ProxyUser) CreateProxyClient(t *testing.T, hostAwait *wait.HostAwaitility) client.Client {
	proxyCl, err := hostAwait.CreateAPIProxyClient(t, u.Token, hostAwait.APIProxyURL)
	require.NoError(t, err)
	return proxyCl
}

func (u *ProxyUser) GetWorkspace(t *testing.T, hostAwait *wait.HostAwaitility, workspaceName string) (*toolchainv1alpha1.Workspace, error) {
	proxyCl := u.CreateProxyClient(t, hostAwait)

	workspace := &toolchainv1alpha1.Workspace{}
	var cause error
	// only wait up to 5 seconds because in some test cases the workspace is not expected to be found
	_ = kubewait.Poll(wait.DefaultRetryInterval, 5*time.Second, func() (bool, error) {
		cause = proxyCl.Get(context.TODO(), types.NamespacedName{Name: workspaceName}, workspace)
		return cause == nil, nil
	})

	// do not assert error before returning because in some test cases the workspace is not expected to be found
	return workspace, cause
}

func (u *ProxyUser) GetApplication(t *testing.T, proxyClient client.Client, applicationName string) *appstudiov1.Application {
	app := &appstudiov1.Application{}
	namespacedName := types.NamespacedName{Namespace: TenantNsName(u.CompliantUsername), Name: applicationName}
	// Get Application
	err := proxyClient.Get(context.TODO(), namespacedName, app)
	require.NoError(t, err)
	require.NotEmpty(t, app)
	return app
}

func (u *ProxyUser) GetApplicationWithoutProxy(t *testing.T, applicationName string) *appstudiov1.Application {
	namespacedName := types.NamespacedName{Namespace: TenantNsName(u.CompliantUsername), Name: applicationName}
	app := &appstudiov1.Application{}
	err := u.ExpectedMemberCluster.Client.Get(context.TODO(), namespacedName, app)
	require.NoError(t, err)
	require.NotEmpty(t, app)
	return app
}

func (u *ProxyUser) GetApplicationName(i int) string {
	return fmt.Sprintf("%s-test-app-%d", u.CompliantUsername, i)
}

func CreateProxyUsersForTest(t *testing.T, awaitilities wait.Awaitilities) []*ProxyUser {
	users := []*ProxyUser{
		{
			ExpectedMemberCluster: awaitilities.Member1(),
			Username:              "proxymember1",
			IdentityID:            uuid.Must(uuid.NewV4()),
		},
		{
			ExpectedMemberCluster: awaitilities.Member2(),
			Username:              "proxymember2",
			IdentityID:            uuid.Must(uuid.NewV4()),
		},
		{
			ExpectedMemberCluster: awaitilities.Member1(),
			Username:              "compliant.username", // contains a '.' that is valid in the username but should not be in the impersonation header since it should use the compliant username
			IdentityID:            uuid.Must(uuid.NewV4()),
		},
	}
	//create the users before the subtests, so they exist for the duration of the whole "ProxyFlow" test ;)
	for _, user := range users {
		CreateAppStudioUser(t, awaitilities, user)
	}
	return users
}

func CreateAppStudioUser(t *testing.T, awaitilities wait.Awaitilities, user *ProxyUser) {
	// Create and approve signup
	req := testsupport.NewSignupRequest(awaitilities).
		Username(user.Username).
		IdentityID(user.IdentityID).
		ManuallyApprove().
		TargetCluster(user.ExpectedMemberCluster).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	user.Signup, _ = req.Resources()
	user.Token = req.GetToken()
	testsupport.VerifyResourcesProvisionedForSignup(t, awaitilities, user.Signup, "deactivate30", "appstudio")
	user.CompliantUsername = user.Signup.Status.CompliantUsername
	_, err := awaitilities.Host().WaitForMasterUserRecord(t, user.CompliantUsername, wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
	require.NoError(t, err)
}

func CreatePreexistingUserAndIdentity(t *testing.T, user ProxyUser) (*userv1.User, *userv1.Identity) {
	preexistingUser := &userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: user.Username,
		},
		Identities: []string{
			identitypkg.NewIdentityNamingStandard(user.IdentityID.String(), "rhd").IdentityName(),
		},
	}
	require.NoError(t, user.ExpectedMemberCluster.CreateWithCleanup(t, preexistingUser))

	preexistingIdentity := &userv1.Identity{
		ObjectMeta: metav1.ObjectMeta{
			Name: identitypkg.NewIdentityNamingStandard(user.IdentityID.String(), "rhd").IdentityName(),
		},
		ProviderName:     "rhd",
		ProviderUserName: user.Username,
		User: corev1.ObjectReference{
			Name: preexistingUser.Name,
			UID:  preexistingUser.UID,
		},
	}
	require.NoError(t, user.ExpectedMemberCluster.CreateWithCleanup(t, preexistingIdentity))
	return preexistingUser, preexistingIdentity
}
