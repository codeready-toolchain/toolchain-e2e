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

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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

func (u *proxyUser) shareSpaceWith(t *testing.T, hostAwait *wait.HostAwaitility, guestUser *proxyUser) {
	// share primaryUser space with guestUser
	guestUserMur, err := hostAwait.GetMasterUserRecord(guestUser.compliantUsername)
	require.NoError(t, err)
	primaryUserSpace, err := hostAwait.WaitForSpace(t, u.compliantUsername, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)
	CreateSpaceBinding(t, hostAwait, guestUserMur, primaryUserSpace, "admin") // creating a spacebinding gives guestUser access to primaryUser's space
}

func (u *proxyUser) listWorkspaces(t *testing.T, hostAwait *wait.HostAwaitility) []toolchainv1alpha1.Workspace {
	proxyCl, err := hostAwait.CreateAPIProxyClient(t, u.token, hostAwait.APIProxyURL)
	require.NoError(t, err)

	workspaces := &toolchainv1alpha1.WorkspaceList{}
	err = proxyCl.List(context.TODO(), workspaces)
	require.NoError(t, err)
	return workspaces.Items
}

func (u *proxyUser) getWorkspace(t *testing.T, hostAwait *wait.HostAwaitility, workspaceName string) (*toolchainv1alpha1.Workspace, error) {
	proxyCl, err := hostAwait.CreateAPIProxyClient(t, u.token, hostAwait.APIProxyURL)
	require.NoError(t, err)

	workspace := &toolchainv1alpha1.Workspace{}
	err = proxyCl.Get(context.TODO(), types.NamespacedName{Name: workspaceName}, workspace)
	return workspace, err
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
	// create the users before the subtests, so they exist for the duration of the whole "ProxyFlow" test ;)
	for _, user := range users {
		createAppStudioUser(t, awaitilities, user)
	}

	// if there is an identity & user resources already present, but don't contain "owner" label, then they shouldn't be deleted
	preexistingUser, preexistingIdentity := createPreexistingUserAndIdentity(t, *users[0])

	for index, user := range users {
		t.Run(user.username, func(t *testing.T) {

			t.Run("use proxy to create a HAS Application CR in the user appstudio namespace via proxy API and use websocket to watch it created", func(t *testing.T) {
				// Start a new websocket watcher which watches for Application CRs in the user's namespace
				w := newWsWatcher(t, *user, user.compliantUsername, hostAwait.APIProxyURL)
				closeConnection := w.Start()
				defer closeConnection()
				proxyCl, err := hostAwait.CreateAPIProxyClient(t, user.token, hostAwait.APIProxyURL)
				require.NoError(t, err)

				// Create and retrieve the application resources multiple times for the same user to make sure the proxy cache kicks in.
				for i := 0; i < 2; i++ {
					// given
					applicationName := fmt.Sprintf("%s-test-app-%d", user.compliantUsername, i)
					expectedApp := newApplication(applicationName, tenantNsName(user.compliantUsername))

					// when
					err := proxyCl.Create(context.TODO(), expectedApp)
					require.NoError(t, err)

					// then
					// wait for the websocket watcher which uses the proxy to receive the Application CR
					found, err := w.WaitForApplication(
						user.expectedMemberCluster.RetryInterval,
						user.expectedMemberCluster.Timeout,
						expectedApp.Name,
					)
					require.NoError(t, err)
					assert.NotEmpty(t, found)

					// Double check that the Application does exist using a regular client (non-proxy)
					namespacedName := types.NamespacedName{Namespace: tenantNsName(user.compliantUsername), Name: applicationName}
					createdApp := &appstudiov1.Application{}
					err = user.expectedMemberCluster.Client.Get(context.TODO(), namespacedName, createdApp)
					require.NoError(t, err)
					require.NotEmpty(t, createdApp)
					assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
				}
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
				proxyCl, err := hostAwait.CreateAPIProxyClient(t, user.token, hostAwait.APIProxyURL)
				require.NoError(t, err)

				// then
				err = proxyCl.Create(context.TODO(), expectedApp)
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
					user.expectedMemberCluster.RetryInterval,
					user.expectedMemberCluster.Timeout,
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
						defer func() {
							resp.Body.Close()
						}()
						require.Equal(t, 403, resp.StatusCode) // should be forbidden
						r, _ := io.ReadAll(resp.Body)
						assert.NotContains(t, string(r), "NodeList") // request should be forbidden so there should be no NodeList in the response
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
				primaryUser.expectedMemberCluster.RetryInterval,
				primaryUser.expectedMemberCluster.Timeout,
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
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(t)
	user.signup, _ = req.Resources()
	user.token = req.GetToken()
	VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, "deactivate30", "appstudio")
	user.compliantUsername = user.signup.Status.CompliantUsername
	_, err := awaitilities.Host().WaitForMasterUserRecord(t, user.compliantUsername, wait.UntilMasterUserRecordHasCondition(Provisioned()))
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
		defer func() {
			resp.Body.Close()
		}()
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
		w.receivedApps[copyApp.Name] = &copyApp
		w.mu.Unlock()
	}
}

func (w *wsWatcher) WaitForApplication(retryInterval, timeout time.Duration, expectedAppName string) (*appstudiov1.Application, error) {
	var foundApp *appstudiov1.Application
	err := kubewait.Poll(retryInterval, timeout, func() (bool, error) {
		defer w.mu.RUnlock()
		w.mu.RLock()
		foundApp = w.receivedApps[expectedAppName]
		return foundApp != nil, nil
	})
	return foundApp, err
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
