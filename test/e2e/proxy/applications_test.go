package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio"
	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

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
func TestProxyApplicationsFlow(t *testing.T) {
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
	for index, user := range users {
		t.Run(user.Username, func(t *testing.T) {
			// Start a new websocket watcher
			w := NewWsWatcher(t, *user, user.CompliantUsername, hostAwait.APIProxyURL)
			closeConnection := w.Start()
			defer closeConnection()
			proxyCl := user.CreateProxyClient(t, hostAwait)
			applicationList := &appstudiov1.ApplicationList{}

			t.Run("use proxy to create a HAS Application CR in the user appstudio namespace via proxy API and use websocket to watch it created", func(t *testing.T) {
				// Create and retrieve the application resources multiple times for the same user to make sure the proxy cache kicks in.
				for i := 0; i < 2; i++ {
					// given
					applicationName := user.GetApplicationName(i)
					expectedApp := NewApplication(applicationName, TenantNsName(user.CompliantUsername))

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

					proxyApp := user.GetApplication(t, proxyCl, applicationName)
					assert.NotEmpty(t, proxyApp)

					// Double check that the Application does exist using a regular client (non-proxy)
					noProxyApp := user.GetApplicationWithoutProxy(t, applicationName)
					assert.Equal(t, expectedApp.Spec, noProxyApp.Spec)
				}

				t.Run("use proxy to update a HAS Application CR in the user appstudio namespace via proxy API", func(t *testing.T) {
					// Update application
					applicationName := user.GetApplicationName(0)
					// Get application
					proxyApp := user.GetApplication(t, proxyCl, applicationName)
					// Update DisplayName
					changedDisplayName := fmt.Sprintf("Proxy test for user %s - updated application", TenantNsName(user.CompliantUsername))
					proxyApp.Spec.DisplayName = changedDisplayName
					err := proxyCl.Update(context.TODO(), proxyApp)
					require.NoError(t, err)

					// Find application and check, if it is updated
					updatedApp := user.GetApplication(t, proxyCl, applicationName)
					assert.Equal(t, proxyApp.Spec.DisplayName, updatedApp.Spec.DisplayName)

					// Check that the Application is updated using a regular client (non-proxy)
					noProxyUpdatedApp := user.GetApplicationWithoutProxy(t, applicationName)
					assert.Equal(t, proxyApp.Spec.DisplayName, noProxyUpdatedApp.Spec.DisplayName)
				})

				t.Run("use proxy to list a HAS Application CR in the user appstudio namespace", func(t *testing.T) {
					// Get List of applications.
					err := proxyCl.List(context.TODO(), applicationList, &client.ListOptions{Namespace: TenantNsName(user.CompliantUsername)})
					// User should be able to list applications
					require.NoError(t, err)
					assert.NotEmpty(t, applicationList.Items)

					// Check that the applicationList using a regular client (non-proxy)
					applicationListWS := &appstudiov1.ApplicationList{}
					err = user.ExpectedMemberCluster.Client.List(context.TODO(), applicationListWS, &client.ListOptions{Namespace: TenantNsName(user.CompliantUsername)})
					require.NoError(t, err)
					require.Len(t, applicationListWS.Items, 2)
					assert.Equal(t, applicationListWS.Items, applicationList.Items)
				})

				t.Run("use proxy to patch a HAS Application CR in the user appstudio namespace via proxy API", func(t *testing.T) {
					// Patch application
					applicationName := user.GetApplicationName(1)
					patchString := "Patched application for proxy test"
					// Get application
					proxyApp := user.GetApplication(t, proxyCl, applicationName)
					// Patch for DisplayName
					patchPayload := []PatchStringValue{{
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
					patchedApp := user.GetApplication(t, proxyCl, applicationName)
					assert.Equal(t, patchString, patchedApp.Spec.DisplayName)

					// Double check that the Application is patched using a regular client (non-proxy)
					noProxyApp := user.GetApplicationWithoutProxy(t, applicationName)
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
						namespacedName := types.NamespacedName{Namespace: TenantNsName(user.CompliantUsername), Name: proxyApp.Name}
						originalApp := &appstudiov1.Application{}
						err = user.ExpectedMemberCluster.Client.Get(context.TODO(), namespacedName, originalApp)
						require.Error(t, err) //not found
						require.True(t, k8serr.IsNotFound(err))
					}
				})
			})

			t.Run("try to create a resource in an unauthorized namespace", func(t *testing.T) {
				// given
				appName := fmt.Sprintf("%s-proxy-test-app", user.Username)
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
				proxyCl = user.CreateProxyClient(t, hostAwait)

				// then
				err := proxyCl.Create(context.TODO(), expectedApp)
				require.EqualError(t, err, fmt.Sprintf(`invalid workspace request: access to namespace '%s' in workspace '%s' is forbidden (post applications.appstudio.redhat.com)`, hostAwait.Namespace, user.CompliantUsername))
			})

			t.Run("unable to create a resource in the other users namespace because the workspace is not shared", func(t *testing.T) {
				// given
				otherUser := users[(index+1)%len(users)]
				t.Log("other user: ", otherUser.Username)
				// verify other user's namespace still exists
				ns := &corev1.Namespace{}
				namespaceName := TenantNsName(otherUser.CompliantUsername)
				err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: namespaceName}, ns)
				require.NoError(t, err, "the other user's namespace should still exist")

				// when
				appName := fmt.Sprintf("%s-proxy-test-app", user.CompliantUsername)
				appToCreate := &appstudiov1.Application{
					ObjectMeta: metav1.ObjectMeta{
						Name:      appName,
						Namespace: namespaceName, // user should not be allowed to create a resource in the other user's namespace
					},
					Spec: appstudiov1.ApplicationSpec{
						DisplayName: "Should be forbidden",
					},
				}
				workspaceName := otherUser.CompliantUsername
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(workspaceName) // set workspace context to other user's workspace
				proxyCl, err := hostAwait.CreateAPIProxyClient(t, user.Token, proxyWorkspaceURL)
				require.NoError(t, err)
				err = proxyCl.Create(context.TODO(), appToCreate)

				// then
				// note: the actual error message is "invalid workspace request: access to workspace '%s' is forbidden" but clients make api discovery calls
				// that fail because in this case the api discovery calls go through the proxyWorkspaceURL which is invalid. If using oc or kubectl and you
				// enable verbose logging you would see Response Body: invalid workspace request: access to workspace 'proxymember2' is forbidden
				require.EqualError(t, err, `no matches for kind "Application" in version "appstudio.redhat.com/v1alpha1"`)
			})

			t.Run("successful workspace context request", func(t *testing.T) {
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(user.CompliantUsername)
				// Start a new websocket watcher which watches for Application CRs in the user's namespace
				w := NewWsWatcher(t, *user, user.CompliantUsername, proxyWorkspaceURL)
				closeConnection := w.Start()
				defer closeConnection()
				workspaceCl, err := hostAwait.CreateAPIProxyClient(t, user.Token, proxyWorkspaceURL) // proxy client with workspace context
				require.NoError(t, err)

				// given
				applicationName := fmt.Sprintf("%s-workspace-context", user.CompliantUsername)
				namespaceName := TenantNsName(user.CompliantUsername)
				expectedApp := NewApplication(applicationName, namespaceName)

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
				err = user.ExpectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespaceName, Name: applicationName}, createdApp)
				require.NoError(t, err)
				require.NotEmpty(t, createdApp)
				assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
			}) // end of successful workspace context request

			t.Run("successful workspace context request with proxy plugin", func(t *testing.T) {
				// we are going to repurpose a well known, always running route as a proxy plugin to contact through the registration service
				openshiftConsoleString := "openshift-console"
				CreateProxyPluginWithCleanup(t, hostAwait, openshiftConsoleString, openshiftConsoleString, "console")
				VerifyProxyPlugin(t, hostAwait, openshiftConsoleString)
				proxyPluginWorkspaceURL := hostAwait.PluginProxyURLWithWorkspaceContext(openshiftConsoleString, user.CompliantUsername)
				client := http.Client{
					Timeout: 30 * time.Second,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
					},
				}
				request, err := http.NewRequest("GET", proxyPluginWorkspaceURL, nil)
				require.NoError(t, err)

				request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", user.Token))
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
				_, err := hostAwaitWithShorterTimeout.CreateAPIProxyClient(t, user.Token, proxyWorkspaceURL)
				require.EqualError(t, err, `an error on the server ("unable to get target cluster: the requested space is not available") has prevented the request from succeeding`)
			})

			t.Run("invalid request headers", func(t *testing.T) {
				// given
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(user.CompliantUsername)
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
					t.Run(fmt.Sprintf("k=%s,v=%s", header.Key, header.Value), func(t *testing.T) {
						// given
						request, err := http.NewRequest("GET", nodesURL, nil)
						request.Header.Add(header.Key, header.Value)
						require.NoError(t, err)
						request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", user.Token)) // uses the user's token with the impersonation headers

						// when
						resp, err := client.Do(request)

						// then
						require.NoError(t, err)
						require.NotNil(t, resp)
						defer resp.Body.Close()
						require.Equal(t, 403, resp.StatusCode) // should be forbidden
						r, _ := io.ReadAll(resp.Body)
						assert.Contains(t, string(r), fmt.Sprintf(`nodes is forbidden: User \"%s\" cannot list resource \"nodes\" in API group \"\" at the cluster scope`, user.CompliantUsername))
					})
				}

			}) // end of invalid request headers
		})
	} // end users loop
}

type headerKeyValue struct {
	Key, Value string
}
