package e2e

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	identitypkg "github.com/codeready-toolchain/toolchain-common/pkg/identity"
	commonproxy "github.com/codeready-toolchain/toolchain-common/pkg/proxy"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	
	kubewait "k8s.io/apimachinery/pkg/util/wait"
)

type proxyUser struct {
	expectedMemberCluster *wait.MemberAwaitility
	username              string
	token                 string
	identityID            uuid.UUID
	signup                *toolchainv1alpha1.UserSignup
	compliantUsername     string
}

type patchStringValue struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

func (u *proxyUser) shareSpaceWith(t *testing.T, hostAwait *wait.HostAwaitility, guestUser *proxyUser) {
	// share primaryUser space with guestUser
	guestUserMur, err := hostAwait.GetMasterUserRecord(guestUser.compliantUsername)
	require.NoError(t, err)
	primaryUserSpace, err := hostAwait.WaitForSpace(t, u.compliantUsername, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)
	CreateSpaceBinding(t, hostAwait, guestUserMur, primaryUserSpace, "admin") // creating a spacebinding gives guestUser access to primaryUser's space
}

func (u *proxyUser) listWorkspaces(t *testing.T, hostAwait *wait.HostAwaitility) []toolchainv1alpha1.Workspace {
	proxyCl := u.createProxyClient(t, hostAwait)

	workspaces := &toolchainv1alpha1.WorkspaceList{}
	err := proxyCl.List(context.TODO(), workspaces)
	require.NoError(t, err)
	return workspaces.Items
}

func (u *proxyUser) createProxyClient(t *testing.T, hostAwait *wait.HostAwaitility) client.Client {
	proxyCl, err := hostAwait.CreateAPIProxyClient(t, u.token, hostAwait.APIProxyURL)
	require.NoError(t, err)
	return proxyCl
}

func (u *proxyUser) getWorkspace(t *testing.T, hostAwait *wait.HostAwaitility, workspaceName string) (*toolchainv1alpha1.Workspace, error) {
	proxyCl := u.createProxyClient(t, hostAwait)

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

func (u *proxyUser) getApplication(t *testing.T, proxyClient client.Client, applicationName string) *appstudiov1.Application {
	app := &appstudiov1.Application{}
	namespacedName := types.NamespacedName{Namespace: tenantNsName(u.compliantUsername), Name: applicationName}
	// Get Application
	err := proxyClient.Get(context.TODO(), namespacedName, app)
	require.NoError(t, err)
	require.NotEmpty(t, app)
	return app
}

func (u *proxyUser) getApplicationWithoutProxy(t *testing.T, applicationName string) *appstudiov1.Application {
	namespacedName := types.NamespacedName{Namespace: tenantNsName(u.compliantUsername), Name: applicationName}
	app := &appstudiov1.Application{}
	err := u.expectedMemberCluster.Client.Get(context.TODO(), namespacedName, app)
	require.NoError(t, err)
	require.NotEmpty(t, app)
	return app
}

func (u *proxyUser) getApplicationName(i int) string {
	return fmt.Sprintf("%s-test-app-%d", u.compliantUsername, i)
}

// full flow from usersignup with approval down to namespaces creation and cleanup
//
// !!! Additional context !!!
// The test uses a dummy HAS API type called Application. The reason is that the regular
// user doesn't have full permission for the standard types like ConfigMap. This means
// that we could do create/read operations on that resource from this test.
// To work around this limitation, we created a dummy HAS API type that has the same name
// and the same group as the actual one. The CRD is created as part of the test setup
// and since the CRD name & group name matches, then RBAC allow us to execute create/read
// operations on that resource using the user permissions.
func TestProxyFlow(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	setStoneSoupConfig(t, hostAwait, memberAwait)

	t.Logf("Proxy URL: %s", hostAwait.APIProxyURL)

	waitForWatcher := runWatcher(t, awaitilities)
	defer func() {
		t.Log("wait until the watcher is stopped")
		waitForWatcher.Wait()
	}()

	users := []*proxyUser{
		{
			expectedMemberCluster: memberAwait,
			username:              "proxymember1",
			identityID:            uuid.Must(uuid.NewV4()),
		},
		{
			expectedMemberCluster: memberAwait2,
			username:              "proxymember2",
			identityID:            uuid.Must(uuid.NewV4()),
		},
		{
			expectedMemberCluster: memberAwait,
			username:              "compliant.username", // contains a '.' that is valid in the username but should not be in the impersonation header since it should use the compliant username
			identityID:            uuid.Must(uuid.NewV4()),
		},
	}
	//create the users before the subtests, so they exist for the duration of the whole "ProxyFlow" test ;)
	for _, user := range users {
		createAppStudioUser(t, awaitilities, user)
	}

	// if there is an identity & user resources already present, but don't contain "owner" label, then they shouldn't be deleted
	preexistingUser, preexistingIdentity := createPreexistingUserAndIdentity(t, *users[0])

	for index, user := range users {
		t.Run(user.username, func(t *testing.T) {
			// Start a new websocket watcher
			w := newWsWatcher(t, *user, user.compliantUsername, hostAwait.APIProxyURL)
			closeConnection := w.Start()
			defer closeConnection()
			proxyCl := user.createProxyClient(t, hostAwait)
			applicationList := &appstudiov1.ApplicationList{}

			t.Run("use proxy to create a HAS Application CR in the user appstudio namespace via proxy API and use websocket to watch it created", func(t *testing.T) {
				// Create and retrieve the application resources multiple times for the same user to make sure the proxy cache kicks in.
				for i := 0; i < 2; i++ {
					// given
					applicationName := user.getApplicationName(i)
					expectedApp := newApplication(applicationName, tenantNsName(user.compliantUsername))

					// when
					err := proxyCl.Create(context.TODO(), expectedApp)
					require.NoError(t, err)

					// then
					// wait for the websocket watcher which uses the proxy to receive the Application CR
					found, err := w.WaitForApplication(
						expectedApp.Name,
					)
					require.NoError(t, err)
					assert.NotEmpty(t, found)
					assert.Equal(t, expectedApp.Spec, found.Spec)

					proxyApp := user.getApplication(t, proxyCl, applicationName)
					assert.NotEmpty(t, proxyApp)

					// Double check that the Application does exist using a regular client (non-proxy)
					noProxyApp := user.getApplicationWithoutProxy(t, applicationName)
					assert.Equal(t, expectedApp.Spec, noProxyApp.Spec)
				}

				t.Run("use proxy to update a HAS Application CR in the user appstudio namespace via proxy API", func(t *testing.T) {
					// Update application
					applicationName := user.getApplicationName(0)
					// Get application
					proxyApp := user.getApplication(t, proxyCl, applicationName)
					// Update DisplayName
					changedDisplayName := fmt.Sprintf("Proxy test for user %s - updated application", tenantNsName(user.compliantUsername))
					proxyApp.Spec.DisplayName = changedDisplayName
					err := proxyCl.Update(context.TODO(), proxyApp)
					require.NoError(t, err)

					// Find application and check, if it is updated
					updatedApp := user.getApplication(t, proxyCl, applicationName)
					assert.Equal(t, proxyApp.Spec.DisplayName, updatedApp.Spec.DisplayName)

					// Check that the Application is updated using a regular client (non-proxy)
					noProxyUpdatedApp := user.getApplicationWithoutProxy(t, applicationName)
					assert.Equal(t, proxyApp.Spec.DisplayName, noProxyUpdatedApp.Spec.DisplayName)
				})

				t.Run("use proxy to list a HAS Application CR in the user appstudio namespace", func(t *testing.T) {
					// Get List of applications.
					err := proxyCl.List(context.TODO(), applicationList, &client.ListOptions{Namespace: tenantNsName(user.compliantUsername)})
					// User should be able to list applications
					require.NoError(t, err)
					assert.NotEmpty(t, applicationList.Items)

					// Check that the applicationList using a regular client (non-proxy)
					applicationListWS := &appstudiov1.ApplicationList{}
					err = user.expectedMemberCluster.Client.List(context.TODO(), applicationListWS, &client.ListOptions{Namespace: tenantNsName(user.compliantUsername)})
					require.NoError(t, err)
					require.Len(t, applicationListWS.Items, 2)
					assert.Equal(t, applicationListWS.Items, applicationList.Items)
				})

				t.Run("use proxy to patch a HAS Application CR in the user appstudio namespace via proxy API", func(t *testing.T) {
					// Patch application
					applicationName := user.getApplicationName(1)
					patchString := "Patched application for proxy test"
					// Get application
					proxyApp := user.getApplication(t, proxyCl, applicationName)
					// Patch for DisplayName
					patchPayload := []patchStringValue{{
						Op:    "replace",
						Path:  "/spec/displayName",
						Value: patchString,
					}}
					patchPayloadBytes, err := json.Marshal(patchPayload)
					require.NoError(t, err)

					// Appply Patch
					err = proxyCl.Patch(context.TODO(), proxyApp, client.RawPatch(types.JSONPatchType, patchPayloadBytes))
					require.NoError(t, err)

					// Get patched app and verify patched DisplayName
					patchedApp := user.getApplication(t, proxyCl, applicationName)
					assert.Equal(t, patchString, patchedApp.Spec.DisplayName)

					// Double check that the Application is patched using a regular client (non-proxy)
					noProxyApp := user.getApplicationWithoutProxy(t, applicationName)
					assert.Equal(t, patchString, noProxyApp.Spec.DisplayName)
				})

				t.Run("use proxy to delete a HAS Application CR in the user appstudio namespace via proxy API and use websocket to watch it deleted", func(t *testing.T) {
					// Delete applications
					for i := 0; i < len(applicationList.Items); i++ {
						// Get application
						proxyApp := applicationList.Items[i].DeepCopy()
						// Delete
						err := proxyCl.Delete(context.TODO(), proxyApp)
						require.NoError(t, err)
						err = w.WaitForApplicationDeletion(
							proxyApp.Name,
						)
						require.NoError(t, err)

						// Check that the Application is deleted using a regular client (non-proxy)
						namespacedName := types.NamespacedName{Namespace: tenantNsName(user.compliantUsername), Name: proxyApp.Name}
						originalApp := &appstudiov1.Application{}
						err = user.expectedMemberCluster.Client.Get(context.TODO(), namespacedName, originalApp)
						require.Error(t, err) //not found
						require.True(t, k8serr.IsNotFound(err))
					}
				})
			})

			t.Run("try to create a resource in an unauthorized namespace", func(t *testing.T) {
				// given
				appName := fmt.Sprintf("%s-proxy-test-app", user.username)
				expectedApp := &appstudiov1.Application{
					ObjectMeta: metav1.ObjectMeta{
						Name:      appName,
						Namespace: hostAwait.Namespace, // user should not be allowed to create a resource in the host operator namespace
					},
					Spec: appstudiov1.ApplicationSpec{
						DisplayName: "Should be forbidden",
					},
				}

				// when
				proxyCl = user.createProxyClient(t, hostAwait)

				// then
				err := proxyCl.Create(context.TODO(), expectedApp)
				require.EqualError(t, err, fmt.Sprintf(`invalid workspace request: access to namespace '%s' in workspace '%s' is forbidden (post applications.appstudio.redhat.com)`, hostAwait.Namespace, user.compliantUsername))
			})

			t.Run("unable to create a resource in the other users namespace because the workspace is not shared", func(t *testing.T) {
				// given
				otherUser := users[(index+1)%len(users)]
				t.Log("other user: ", otherUser.username)
				// verify other user's namespace still exists
				ns := &corev1.Namespace{}
				namespaceName := tenantNsName(otherUser.compliantUsername)
				err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: namespaceName}, ns)
				require.NoError(t, err, "the other user's namespace should still exist")

				// when
				appName := fmt.Sprintf("%s-proxy-test-app", user.compliantUsername)
				appToCreate := &appstudiov1.Application{
					ObjectMeta: metav1.ObjectMeta{
						Name:      appName,
						Namespace: namespaceName, // user should not be allowed to create a resource in the other user's namespace
					},
					Spec: appstudiov1.ApplicationSpec{
						DisplayName: "Should be forbidden",
					},
				}
				workspaceName := otherUser.compliantUsername
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(workspaceName) // set workspace context to other user's workspace
				proxyCl, err := hostAwait.CreateAPIProxyClient(t, user.token, proxyWorkspaceURL)
				require.NoError(t, err)
				err = proxyCl.Create(context.TODO(), appToCreate)

				// then
				// note: the actual error message is "invalid workspace request: access to workspace '%s' is forbidden" but clients make api discovery calls
				// that fail because in this case the api discovery calls go through the proxyWorkspaceURL which is invalid. If using oc or kubectl and you
				// enable verbose logging you would see Response Body: invalid workspace request: access to workspace 'proxymember2' is forbidden
				require.EqualError(t, err, `no matches for kind "Application" in version "appstudio.redhat.com/v1alpha1"`)
			})

			t.Run("successful workspace context request", func(t *testing.T) {
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(user.compliantUsername)
				// Start a new websocket watcher which watches for Application CRs in the user's namespace
				w := newWsWatcher(t, *user, user.compliantUsername, proxyWorkspaceURL)
				closeConnection := w.Start()
				defer closeConnection()
				workspaceCl, err := hostAwait.CreateAPIProxyClient(t, user.token, proxyWorkspaceURL) // proxy client with workspace context
				require.NoError(t, err)

				// given
				applicationName := fmt.Sprintf("%s-workspace-context", user.compliantUsername)
				namespaceName := tenantNsName(user.compliantUsername)
				expectedApp := newApplication(applicationName, namespaceName)

				// when
				err = workspaceCl.Create(context.TODO(), expectedApp)
				require.NoError(t, err)

				// then
				// wait for the websocket watcher which uses the proxy to receive the Application CR
				found, err := w.WaitForApplication(
					expectedApp.Name,
				)
				require.NoError(t, err)
				assert.NotEmpty(t, found)

				// Double check that the Application does exist using a regular client (non-proxy)
				createdApp := &appstudiov1.Application{}
				err = user.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespaceName, Name: applicationName}, createdApp)
				require.NoError(t, err)
				require.NotEmpty(t, createdApp)
				assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
			}) // end of successful workspace context request

			t.Run("successful workspace context request with proxy plugin", func(t *testing.T) {
				// we are going to repurpose a well known, always running route as a proxy plugin to contact through the registration service
				CreateProxyPluginWithCleanup(t, hostAwait, "openshift-console", "openshift-console", "console")
				VerifyProxyPlugin(t, hostAwait, "openshift-console")
				proxyPluginWorkspaceURL := hostAwait.PluginProxyURLWithWorkspaceContext("openshift-console", user.compliantUsername)
				client := http.Client{
					Timeout: 30 * time.Second,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
					},
				}
				request, err := http.NewRequest("GET", proxyPluginWorkspaceURL, nil)
				require.NoError(t, err)

				request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", user.token))
				var resp *http.Response
				resp, err = client.Do(request)
				require.NoError(t, err)
				defer resp.Body.Close()
				var body []byte
				body, err = io.ReadAll(resp.Body)
				require.NoError(t, err)
				bodyStr := string(body)
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected http return code of %d with body text %s", resp.StatusCode, bodyStr)
				}
				if !strings.Contains(bodyStr, "Red") || !strings.Contains(bodyStr, "Open") {
					t.Errorf("unexpected http response body %s", bodyStr)
				}
			}) // end of successful workspace context request with proxy plugin

			t.Run("invalid workspace context request", func(t *testing.T) {
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext("notexist")
				hostAwaitWithShorterTimeout := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 3)) // we expect an error so we can use a shorter timeout
				_, err := hostAwaitWithShorterTimeout.CreateAPIProxyClient(t, user.token, proxyWorkspaceURL)
				require.EqualError(t, err, `an error on the server ("unable to get target cluster: the requested space is not available") has prevented the request from succeeding`)
			})

			t.Run("invalid request headers", func(t *testing.T) {
				// given
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(user.compliantUsername)
				rejectedHeaders := []headerKeyValue{
					{"Impersonate-Group", "system:cluster-admins"},
					{"Impersonate-Group", "system:node-admins"},
				}
				client := http.Client{
					Timeout: time.Duration(5 * time.Second), // because sometimes the network connection may be a bit slow
				}
				client.Transport = &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true, // nolint:gosec
					},
				}
				t.Logf("proxyWorkspaceURL: %s", proxyWorkspaceURL)
				nodesURL := fmt.Sprintf("%s/api/v1/nodes", proxyWorkspaceURL)
				t.Logf("nodesURL: %s", nodesURL)

				for _, header := range rejectedHeaders {
					t.Run(fmt.Sprintf("k=%s,v=%s", header.key, header.value), func(t *testing.T) {
						// given
						request, err := http.NewRequest("GET", nodesURL, nil)
						request.Header.Add(header.key, header.value)
						require.NoError(t, err)
						request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", user.token)) // uses the user's token with the impersonation headers

						// when
						resp, err := client.Do(request)

						// then
						require.NoError(t, err)
						require.NotNil(t, resp)
						defer resp.Body.Close()
						require.Equal(t, 403, resp.StatusCode) // should be forbidden
						r, _ := io.ReadAll(resp.Body)
						assert.Contains(t, string(r), fmt.Sprintf(`nodes is forbidden: User \"%s\" cannot list resource \"nodes\" in API group \"\" at the cluster scope`, user.compliantUsername))
					})
				}

			}) // end of invalid request headers
		})
	} // end users loop

	t.Run("proxy with shared workspace use cases", func(t *testing.T) {
		// given
		guestUser := users[0]
		primaryUser := users[1]
		applicationName := fmt.Sprintf("%s-share-workspace-context", primaryUser.compliantUsername)
		workspaceName := primaryUser.compliantUsername
		primaryUserWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(workspaceName) // set workspace context using primaryUser's workspace

		// ensure the app exists in primaryUser's space
		primaryUserNamespace := tenantNsName(primaryUser.compliantUsername)
		expectedApp := newApplication(applicationName, primaryUserNamespace)
		err := primaryUser.expectedMemberCluster.Client.Create(context.TODO(), expectedApp)
		require.NoError(t, err)

		t.Run("guestUser request to unauthorized workspace", func(t *testing.T) {
			proxyCl, err := hostAwait.CreateAPIProxyClient(t, guestUser.token, primaryUserWorkspaceURL)
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
			err = guestUser.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: primaryUserNamespace, Name: applicationName}, createdApp)
			require.NoError(t, err)
			require.NotEmpty(t, createdApp)
			assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
		})

		t.Run("share primaryUser workspace with guestUser", func(t *testing.T) {
			// given

			// share primaryUser space with guestUser
			primaryUser.shareSpaceWith(t, hostAwait, guestUser)

			// VerifySpaceRelatedResources will verify the roles and rolebindings are updated to include guestUser's SpaceBinding
			VerifySpaceRelatedResources(t, awaitilities, primaryUser.signup, "appstudio")

			// Start a new websocket watcher which watches for Application CRs in the user's namespace
			w := newWsWatcher(t, *guestUser, primaryUser.compliantUsername, primaryUserWorkspaceURL)
			closeConnection := w.Start()
			defer closeConnection()
			guestUserPrimaryWsCl, err := hostAwait.CreateAPIProxyClient(t, guestUser.token, primaryUserWorkspaceURL)
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
			err = guestUser.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: primaryUserNamespace, Name: applicationName}, createdApp)
			require.NoError(t, err)
			require.NotEmpty(t, createdApp)
			assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)

			t.Run("request for namespace that doesn't belong to workspace context should fail", func(t *testing.T) {
				// In this test the guest user has access to the primary user's namespace since the primary user's workspace has been shared, but if they specify the wrong
				// workspace context (guest user's workspace context) the request should fail. In order for proxy requests to succeed the namespace must belong to the workspace.

				// given
				workspaceName := guestUser.compliantUsername // guestUser's workspace
				guestUserWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(workspaceName)
				guestUserGuestWsCl, err := hostAwait.CreateAPIProxyClient(t, guestUser.token, guestUserWorkspaceURL)
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

// this test will:
//  1. provision a watcher user
//  2. run a goroutine which will:
//     I. create a long-running GET call with a watch=true parameter
//     II. the call will be terminated via a context timeout
//     III. check the expected error that it was terminated via a context and not on the server side
func runWatcher(t *testing.T, awaitilities wait.Awaitilities) *sync.WaitGroup {

	// ======================================================
	// let's define two timeouts

	// contextTimeout defines the time after which the GET (watch) call will be terminated (via the context)
	// this one is the expected timeout and should be bigger than the default one that was originally set
	// for OpenShift Route and the RoundTripper inside proxy to make sure that the call is terminated
	// via the context and not by the server.
	contextTimeout := 40 * time.Second

	// this timeout will be set when initializing the go client - just to be sure that
	// there is no other value set by default and is bigger than the contextTimeout.
	clientConfigTimeout := 50 * time.Second
	// ======================================================

	t.Log("provisioning the watcher")
	watchUser := &proxyUser{
		expectedMemberCluster: awaitilities.Member1(),
		username:              "watcher",
		identityID:            uuid.Must(uuid.NewV4()),
	}
	createAppStudioUser(t, awaitilities, watchUser)

	proxyConfig := awaitilities.Host().CreateAPIProxyConfig(t, watchUser.token, awaitilities.Host().APIProxyURL)
	proxyConfig.Timeout = clientConfigTimeout
	watcherClient, err := kubernetes.NewForConfig(proxyConfig)
	require.NoError(t, err)

	// we need to get a list of ConfigMaps, so we can use the resourceVersion
	// of the list resource in the watch call
	t.Log("getting the first list of ConfigMaps")
	list, err := watcherClient.CoreV1().
		ConfigMaps(tenantNsName(watchUser.compliantUsername)).
		List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)

	var waitForWatcher sync.WaitGroup
	waitForWatcher.Add(1)
	// run the watch in a goroutine because it will take 40 seconds until the call is terminated
	go func() {
		t.Run("running the watcher", func(t *testing.T) {
			defer waitForWatcher.Done()
			withTimeout, cancelFunc := context.WithTimeout(context.Background(), contextTimeout)
			defer cancelFunc()

			started := time.Now()
			t.Log("starting the watch call")
			_, err := watcherClient.RESTClient().Get().
				AbsPath(fmt.Sprintf("/api/v1/namespaces/%s/configmaps", tenantNsName(watchUser.compliantUsername))).
				Param("resourceVersion", list.GetResourceVersion()).
				Param("watch", "true").
				Do(withTimeout).
				Get()
			t.Logf("stopping the watch after %s", time.Since(started))

			assert.EqualError(t, err, "unexpected error when reading response body. Please retry. Original error: context deadline exceeded", "The call should be terminated by the context timeout")
			assert.NotContains(t, err.Error(), "unexpected EOF", "If it contains 'unexpected EOF' then the call was terminated on the server side, which is not expected.")
		})
	}()
	return &waitForWatcher
}

func TestSpaceLister(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	setStoneSoupConfig(t, hostAwait, memberAwait)

	t.Logf("Proxy URL: %s", hostAwait.APIProxyURL)

	users := map[string]*proxyUser{
		"car": {
			expectedMemberCluster: memberAwait,
			username:              "car",
			identityID:            uuid.Must(uuid.NewV4()),
		},
		"bus": {
			expectedMemberCluster: memberAwait2,
			username:              "bus",
			identityID:            uuid.Must(uuid.NewV4()),
		},
		"bicycle": {
			expectedMemberCluster: memberAwait,
			username:              "road.bicycle", // contains a '.' that is valid in the username but should not be in the impersonation header since it should use the compliant username
			identityID:            uuid.Must(uuid.NewV4()),
		},
	}

	// create the users before the subtests, so they exist for the duration of the whole test
	for _, user := range users {
		createAppStudioUser(t, awaitilities, user)
	}

	users["car"].shareSpaceWith(t, hostAwait, users["bus"])
	users["car"].shareSpaceWith(t, hostAwait, users["bicycle"])
	users["bus"].shareSpaceWith(t, hostAwait, users["bicycle"])

	t.Run("car lists workspaces", func(t *testing.T) {
		// when
		workspaces := users["car"].listWorkspaces(t, hostAwait)

		// then
		// car should see only car's workspace
		require.Len(t, workspaces, 1)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], true), workspaces...)
	})

	t.Run("car gets workspaces", func(t *testing.T) {
		t.Run("can get car workspace", func(t *testing.T) {
			// when
			workspace, err := users["car"].getWorkspace(t, hostAwait, users["car"].compliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], true), *workspace)
		})

		t.Run("cannot get bus workspace", func(t *testing.T) {
			// when
			workspace, err := users["car"].getWorkspace(t, hostAwait, users["bus"].compliantUsername)

			// then
			require.EqualError(t, err, "the server could not find the requested resource (get workspaces.toolchain.dev.openshift.com bus)")
			assert.Empty(t, workspace)
		})
	})

	t.Run("bus lists workspaces", func(t *testing.T) {
		// when
		workspaces := users["bus"].listWorkspaces(t, hostAwait)

		// then
		// bus should see both its own and car's workspace
		require.Len(t, workspaces, 2)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], true), workspaces...)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], false), workspaces...)
	})

	t.Run("bus gets workspaces", func(t *testing.T) {
		t.Run("can get bus workspace", func(t *testing.T) {
			// when
			busWS, err := users["bus"].getWorkspace(t, hostAwait, users["bus"].compliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], true), *busWS)
		})

		t.Run("can get car workspace", func(t *testing.T) {
			// when
			carWS, err := users["bus"].getWorkspace(t, hostAwait, users["car"].compliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], false), *carWS)
		})
	})

	t.Run("bicycle lists workspaces", func(t *testing.T) {
		// when
		workspaces := users["bicycle"].listWorkspaces(t, hostAwait)

		// then
		// car should see only car's workspace
		require.Len(t, workspaces, 3)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bicycle"], true), workspaces...)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], false), workspaces...)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], false), workspaces...)
	})

	t.Run("bicycle gets workspaces", func(t *testing.T) {
		t.Run("can get bus workspace", func(t *testing.T) {
			// when
			busWS, err := users["bicycle"].getWorkspace(t, hostAwait, users["bus"].compliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], false), *busWS)
		})

		t.Run("can get car workspace", func(t *testing.T) {
			// when
			carWS, err := users["bicycle"].getWorkspace(t, hostAwait, users["car"].compliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], false), *carWS)
		})

		t.Run("can get bicycle workspace", func(t *testing.T) {
			// when
			bicycleWS, err := users["bicycle"].getWorkspace(t, hostAwait, users["bicycle"].compliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bicycle"], true), *bicycleWS)
		})
	})

	t.Run("other workspace actions not permitted", func(t *testing.T) {
		t.Run("create not allowed", func(t *testing.T) {
			// given
			workspaceToCreate := expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], false)
			bicycleCl, err := hostAwait.CreateAPIProxyClient(t, users["bicycle"].token, hostAwait.APIProxyURL)
			require.NoError(t, err)

			// when
			// bicycle user tries to create a workspace
			err = bicycleCl.Create(context.TODO(), &workspaceToCreate)

			// then
			require.EqualError(t, err, fmt.Sprintf("workspaces.toolchain.dev.openshift.com is forbidden: User \"%s\" cannot create resource \"workspaces\" in API group \"toolchain.dev.openshift.com\" at the cluster scope", users["bicycle"].compliantUsername))
		})

		t.Run("delete not allowed", func(t *testing.T) {
			// given
			workspaceToDelete, err := users["bicycle"].getWorkspace(t, hostAwait, users["bicycle"].compliantUsername)
			require.NoError(t, err)
			bicycleCl, err := hostAwait.CreateAPIProxyClient(t, users["bicycle"].token, hostAwait.APIProxyURL)
			require.NoError(t, err)

			// bicycle user tries to delete a workspace
			err = bicycleCl.Delete(context.TODO(), workspaceToDelete)

			// then
			require.EqualError(t, err, fmt.Sprintf("workspaces.toolchain.dev.openshift.com \"%[1]s\" is forbidden: User \"%[1]s\" cannot delete resource \"workspaces\" in API group \"toolchain.dev.openshift.com\" at the cluster scope", users["bicycle"].compliantUsername))
		})

		t.Run("update not allowed", func(t *testing.T) {
			// when
			workspaceToUpdate := expectedWorkspaceFor(t, awaitilities.Host(), users["bicycle"], true)
			bicycleCl, err := hostAwait.CreateAPIProxyClient(t, users["bicycle"].token, hostAwait.APIProxyURL)
			require.NoError(t, err)

			// bicycle user tries to update a workspace
			err = bicycleCl.Update(context.TODO(), &workspaceToUpdate)

			// then
			require.EqualError(t, err, fmt.Sprintf("workspaces.toolchain.dev.openshift.com \"%[1]s\" is forbidden: User \"%[1]s\" cannot update resource \"workspaces\" in API group \"toolchain.dev.openshift.com\" at the cluster scope", users["bicycle"].compliantUsername))
		})
	})
}

func tenantNsName(username string) string {
	return fmt.Sprintf("%s-tenant", username)
}

func createAppStudioUser(t *testing.T, awaitilities wait.Awaitilities, user *proxyUser) {
	// Create and approve signup
	req := NewSignupRequest(awaitilities).
		Username(user.username).
		IdentityID(user.identityID).
		ManuallyApprove().
		TargetCluster(user.expectedMemberCluster).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	user.signup, _ = req.Resources()
	user.token = req.GetToken()
	VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, "deactivate30", "appstudio")
	user.compliantUsername = user.signup.Status.CompliantUsername
	_, err := awaitilities.Host().WaitForMasterUserRecord(t, user.compliantUsername, wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
	require.NoError(t, err)
}

func createPreexistingUserAndIdentity(t *testing.T, user proxyUser) (*userv1.User, *userv1.Identity) {
	preexistingUser := &userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: user.username,
		},
		Identities: []string{
			identitypkg.NewIdentityNamingStandard(user.identityID.String(), "rhd").IdentityName(),
		},
	}
	require.NoError(t, user.expectedMemberCluster.CreateWithCleanup(t, preexistingUser))

	preexistingIdentity := &userv1.Identity{
		ObjectMeta: metav1.ObjectMeta{
			Name: identitypkg.NewIdentityNamingStandard(user.identityID.String(), "rhd").IdentityName(),
		},
		ProviderName:     "rhd",
		ProviderUserName: user.username,
		User: corev1.ObjectReference{
			Name: preexistingUser.Name,
			UID:  preexistingUser.UID,
		},
	}
	require.NoError(t, user.expectedMemberCluster.CreateWithCleanup(t, preexistingIdentity))
	return preexistingUser, preexistingIdentity
}

func newWsWatcher(t *testing.T, user proxyUser, namespace, proxyURL string) *wsWatcher {
	_, err := url.Parse(proxyURL)
	require.NoError(t, err)
	return &wsWatcher{
		t:            t,
		namespace:    namespace,
		user:         user,
		proxyBaseURL: proxyURL,
	}
}

// wsWatcher represents a watcher which leverages a WebSocket connection to watch for Applications in the user's namespace.
// The connection is established with the reg-service proxy instead of direct connection to the API server.
type wsWatcher struct {
	done         chan interface{}
	interrupt    chan os.Signal
	t            *testing.T
	user         proxyUser
	namespace    string
	connection   *websocket.Conn
	proxyBaseURL string

	mu           sync.RWMutex
	receivedApps map[string]*appstudiov1.Application
}

// start creates a new WebSocket connection. The method returns a function which is to be used to close the connection when done.
func (w *wsWatcher) Start() func() {
	w.done = make(chan interface{})    // Channel to indicate that the receiverHandler is done
	w.interrupt = make(chan os.Signal) // Channel to listen for interrupt signal to terminate gracefully

	signal.Notify(w.interrupt, os.Interrupt) // Notify the interrupt channel for SIGINT

	encodedToken := base64.RawURLEncoding.EncodeToString([]byte(w.user.token))
	protocol := fmt.Sprintf("base64url.bearer.authorization.k8s.io.%s", encodedToken)

	trimmedProxyURL := strings.TrimPrefix(w.proxyBaseURL, "https://")
	socketURL := fmt.Sprintf("wss://%s/apis/appstudio.redhat.com/v1alpha1/namespaces/%s/applications?watch=true", trimmedProxyURL, tenantNsName(w.namespace))
	w.t.Logf("opening connection to '%s'", socketURL)
	dialer := &websocket.Dialer{
		Subprotocols: []string{protocol, "base64.binary.k8s.io"},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // nolint:gosec
		},
	}

	extraHeaders := make(http.Header, 1)
	extraHeaders.Add("Origin", "http://localhost")

	conn, resp, err := dialer.Dial(socketURL, extraHeaders) // nolint:bodyclose // see `return func() {...}`
	if errors.Is(err, websocket.ErrBadHandshake) {
		r, _ := io.ReadAll(resp.Body)
		defer resp.Body.Close()
		w.t.Logf("handshake failed with status %d / response %s", resp.StatusCode, string(r))
	}
	require.NoError(w.t, err)
	w.connection = conn
	w.receivedApps = make(map[string]*appstudiov1.Application)

	go w.receiveHandler()
	go w.startMainLoop()

	return func() {
		_ = w.connection.Close()
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
}

// startMainLoop starts the main loop for the client. Packets are sent here.
func (w *wsWatcher) startMainLoop() {
	for {
		select {
		case <-time.After(time.Duration(1) * time.Millisecond * 1000):
			// Send an echo packet every second
			err := w.connection.WriteMessage(websocket.TextMessage, []byte("Hello from e2e tests!"))
			if err != nil {
				w.t.Logf("Exiting main loop. It's normal if the connection has been closed. Reason: %s\n", err.Error())
				return
			}
		case <-w.interrupt:
			// Received a SIGINT (Ctrl + C). Terminate gracefully...
			// Close the websocket connection
			err := w.connection.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				w.t.Logf("Error during closing websocket: %s", err.Error())
				return
			}

			select {
			case <-w.done:
				w.t.Log("Receiver Channel Closed! Exiting...")
			case <-time.After(time.Duration(1) * time.Second):
				w.t.Log("Timeout in closing receiving channel. Exiting...")
			}
			return
		}
	}
}

type message struct {
	MessageType string                  `json:"type"`
	Application appstudiov1.Application `json:"object"`
}

// receiveHandler listens to the incoming messages and stores them as Applications objects
func (w *wsWatcher) receiveHandler() {
	defer close(w.done)
	for {
		_, msg, err := w.connection.ReadMessage()
		if err != nil {
			w.t.Logf("Exiting message receiving loop. It's normal if the connection has been closed. Reason: %s\n", err.Error())
			return
		}
		w.t.Logf("Received: %s", msg)
		message := message{}
		err = json.Unmarshal(msg, &message)
		require.NoError(w.t, err)
		copyApp := message.Application
		w.mu.Lock()
		if message.MessageType == "DELETED" {
			delete(w.receivedApps, copyApp.Name)
		} else {
			w.receivedApps[copyApp.Name] = &copyApp
		}
		w.mu.Unlock()
	}
}

func (w *wsWatcher) WaitForApplication(expectedAppName string) (*appstudiov1.Application, error) {
	var foundApp *appstudiov1.Application
	err := kubewait.Poll(wait.DefaultRetryInterval, wait.DefaultTimeout, func() (bool, error) {
		defer w.mu.RUnlock()
		w.mu.RLock()
		foundApp = w.receivedApps[expectedAppName]
		return foundApp != nil, nil
	})
	return foundApp, err
}

func (w *wsWatcher) WaitForApplicationDeletion(expectedAppName string) error {
	err := kubewait.PollImmediate(wait.DefaultRetryInterval, wait.DefaultTimeout, func() (bool, error) {
		defer w.mu.RUnlock()
		w.mu.RLock()
		_, present := w.receivedApps[expectedAppName]
		return !present, nil
	})
	return err
}

func newApplication(applicationName, namespace string) *appstudiov1.Application {
	return &appstudiov1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      applicationName,
			Namespace: namespace,
		},
		Spec: appstudiov1.ApplicationSpec{
			DisplayName: fmt.Sprintf("Proxy test for user %s", namespace),
		},
	}
}

// setStoneSoupConfig applies toolchain configuration for stone soup scenarios
func setStoneSoupConfig(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility) {
	// member cluster configured to skip user creation to mimic stonesoup configuration where user & identity resources are not created
	memberConfigurationWithSkipUserCreation := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.SkipUserCreation(true))
	// configure default space tier to appstudio
	hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultUserTier("deactivate30").DefaultSpaceTier("appstudio"), testconfig.Members().Default(memberConfigurationWithSkipUserCreation.Spec))
}

func verifyHasExpectedWorkspace(t *testing.T, expectedWorkspace toolchainv1alpha1.Workspace, actualWorkspaces ...toolchainv1alpha1.Workspace) {
	for _, actualWorkspace := range actualWorkspaces {
		if actualWorkspace.Name == expectedWorkspace.Name {
			assert.Equal(t, expectedWorkspace.Status, actualWorkspace.Status)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.ResourceVersion, "Workspace.ObjectMeta.ResourceVersion field is empty: %#v", actualWorkspace)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.Generation, "Workspace.ObjectMeta.Generation field is empty: %#v", actualWorkspace)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.CreationTimestamp, "Workspace.ObjectMeta.CreationTimestamp field is empty: %#v", actualWorkspace)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.UID, "Workspace.ObjectMeta.UID field is empty: %#v", actualWorkspace)
			return
		}
	}
	t.Errorf("expected workspace %s not found", expectedWorkspace.Name)
}

func expectedWorkspaceFor(t *testing.T, hostAwait *wait.HostAwaitility, user *proxyUser, isHomeWorkspace bool) toolchainv1alpha1.Workspace {
	space, err := hostAwait.WaitForSpace(t, user.compliantUsername, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)

	ws := commonproxy.NewWorkspace(user.compliantUsername,
		commonproxy.WithObjectMetaFrom(space.ObjectMeta),
		commonproxy.WithNamespaces([]toolchainv1alpha1.SpaceNamespace{
			{
				Name: user.compliantUsername + "-tenant",
				Type: "default",
			},
		}),
		commonproxy.WithOwner(user.signup.Name),
		commonproxy.WithRole("admin"),
	)
	// if the user is the same as the one who created the workspace, then expect type should be "home"
	if isHomeWorkspace {
		ws.Status.Type = "home"
	}
	return *ws
}

type headerKeyValue struct {
	key, value string
}
