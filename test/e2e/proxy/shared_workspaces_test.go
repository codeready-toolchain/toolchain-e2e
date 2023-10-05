package proxy

import (
	"context"
	"fmt"
	"testing"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/proxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestSharedWorkspaces(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	SetAppstudioConfig(t, hostAwait, memberAwait)

	t.Logf("Proxy URL: %s", hostAwait.APIProxyURL)

	waitForWatcher := RunWatcher(t, awaitilities)
	defer func() {
		t.Log("wait until the watcher is stopped")
		waitForWatcher.Wait()
	}()

	users := CreateProxyUsersForTest(t, awaitilities)

	// if there is an identity & user resources already present, but don't contain "owner" label, then they shouldn't be deleted
	preexistingUser, preexistingIdentity := CreatePreexistingUserAndIdentity(t, *users[0])

	t.Run("proxy with shared workspace use cases", func(t *testing.T) {
		// given
		guestUser := users[0]
		primaryUser := users[1]
		applicationName := fmt.Sprintf("%s-share-workspace-context", primaryUser.CompliantUsername)
		workspaceName := primaryUser.CompliantUsername
		primaryUserWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(workspaceName) // set workspace context using primaryUser's workspace

		// ensure the app exists in primaryUser's space
		primaryUserNamespace := TenantNsName(primaryUser.CompliantUsername)
		expectedApp := NewApplication(applicationName, primaryUserNamespace)
		err := primaryUser.ExpectedMemberCluster.Client.Create(context.TODO(), expectedApp)
		require.NoError(t, err)

		t.Run("guestUser request to unauthorized workspace", func(t *testing.T) {
			proxyCl, err := hostAwait.CreateAPIProxyClient(t, guestUser.Token, primaryUserWorkspaceURL)
			require.NoError(t, err)

			// when
			actualApp := &appstudiov1.Application{}
			err = proxyCl.Get(context.TODO(), types.NamespacedName{Name: applicationName, Namespace: primaryUserNamespace}, actualApp)

			// then
			// note: the actual error message is "invalid workspace request: access to workspace '%s' is forbidden" but clients make api discovery calls
			// that fail because in this case the api discovery calls go through the proxyWorkspaceURL which is invalid. If using oc or kubectl and you
			// enable verbose logging you would see Response Body: invalid workspace request: access to workspace 'proxymember2' is forbidden
			require.EqualError(t, err, `no matches for kind "Application" in version "appstudio.redhat.com/v1alpha1"`)

			// Double check that the Application does exist using a regular client (non-proxy)
			createdApp := &appstudiov1.Application{}
			err = guestUser.ExpectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: primaryUserNamespace, Name: applicationName}, createdApp)
			require.NoError(t, err)
			require.NotEmpty(t, createdApp)
			assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
		})

		t.Run("share primaryUser workspace with guestUser", func(t *testing.T) {
			// given

			// share primaryUser space with guestUser
			primaryUser.ShareSpaceWith(t, hostAwait, guestUser)

			// VerifySpaceRelatedResources will verify the roles and rolebindings are updated to include guestUser's SpaceBinding
			VerifySpaceRelatedResources(t, awaitilities, primaryUser.Signup, "appstudio")

			// Start a new websocket watcher which watches for Application CRs in the user's namespace
			w := NewWsWatcher(t, *guestUser, primaryUser.CompliantUsername, primaryUserWorkspaceURL)
			closeConnection := w.Start()
			defer closeConnection()
			guestUserPrimaryWsCl, err := hostAwait.CreateAPIProxyClient(t, guestUser.Token, primaryUserWorkspaceURL)
			require.NoError(t, err)

			// when
			// user A requests the Application CR in primaryUser's namespace using the proxy
			actualApp := &appstudiov1.Application{}
			err = guestUserPrimaryWsCl.Get(context.TODO(), types.NamespacedName{Name: applicationName, Namespace: primaryUserNamespace}, actualApp)

			// then
			require.NoError(t, err) // allowed since guestUser has access to primaryUser's space

			// wait for the websocket watcher which uses the proxy to receive the Application CR
			found, err := w.WaitForApplication(
				expectedApp.Name,
			)
			require.NoError(t, err)
			assert.NotEmpty(t, found)

			// Double check that the Application does exist using a regular client (non-proxy)
			createdApp := &appstudiov1.Application{}
			err = guestUser.ExpectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: primaryUserNamespace, Name: applicationName}, createdApp)
			require.NoError(t, err)
			require.NotEmpty(t, createdApp)
			assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)

			t.Run("request for namespace that doesn't belong to workspace context should fail", func(t *testing.T) {
				// In this test the guest user has access to the primary user's namespace since the primary user's workspace has been shared, but if they specify the wrong
				// workspace context (guest user's workspace context) the request should fail. In order for proxy requests to succeed the namespace must belong to the workspace.

				// given
				workspaceName := guestUser.CompliantUsername // guestUser's workspace
				guestUserWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(workspaceName)
				guestUserGuestWsCl, err := hostAwait.CreateAPIProxyClient(t, guestUser.Token, guestUserWorkspaceURL)
				require.NoError(t, err)

				// when
				actualApp := &appstudiov1.Application{}
				err = guestUserGuestWsCl.Get(context.TODO(), types.NamespacedName{Name: applicationName, Namespace: primaryUserNamespace}, actualApp) // primaryUser's namespace

				// then
				require.EqualError(t, err, fmt.Sprintf(`invalid workspace request: access to namespace '%s' in workspace '%s' is forbidden (get applications.appstudio.redhat.com %s)`, primaryUserNamespace, workspaceName, applicationName))
			})
		})

	})

	// preexisting user & identity are still there
	// Verify provisioned User
	_, err := memberAwait.WaitForUser(t, preexistingUser.Name)
	assert.NoError(t, err)

	// Verify provisioned Identity
	_, err = memberAwait.WaitForIdentity(t, preexistingIdentity.Name)
	assert.NoError(t, err)
}
