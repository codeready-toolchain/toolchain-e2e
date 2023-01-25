package e2e

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
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

	// member cluster configured to skip user creation to mimic stonesoup configuration where user & identity resources are not created
	memberConfigurationWithSkipUserCreation := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.SkipUserCreation(true))
	hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultUserTier("deactivate30").DefaultSpaceTier("appstudio"), testconfig.Members().Default(memberConfigurationWithSkipUserCreation.Spec))

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
		_, err := hostAwait.GetMasterUserRecord(user.compliantUsername)
		require.NoError(t, err)
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
					expectedApp := newApplication(applicationName, user.compliantUsername)

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
					createdApp := &appstudiov1.Application{}
					err = user.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: user.compliantUsername, Name: applicationName}, createdApp)
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
				require.EqualError(t, err, fmt.Sprintf(`applications.appstudio.redhat.com is forbidden: User "%[1]s" cannot create resource "applications" in API group "appstudio.redhat.com" in the namespace "%[2]s"`, user.compliantUsername, hostAwait.Namespace))
			})

			if index == 1 { // only for the second user
				t.Run("try to create a resource in the other users namespace", func(t *testing.T) {
					// given
					// verify first user's namespace still exists
					ns := &corev1.Namespace{}
					err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: users[0].username}, ns)
					require.NoError(t, err, "failed to verify the first user's namespace still exists")

					appName := fmt.Sprintf("%s-proxy-test-app", users[0].username)
					appToCreate := &appstudiov1.Application{
						ObjectMeta: metav1.ObjectMeta{
							Name:      appName,
							Namespace: users[0].expectedMemberCluster.Namespace, // user should not be allowed to create a resource in the first user's namespace
						},
						Spec: appstudiov1.ApplicationSpec{
							DisplayName: "Should be forbidden",
						},
					}

					// when
					proxyCl, err := hostAwait.CreateAPIProxyClient(t, user.token, hostAwait.APIProxyURL)
					require.NoError(t, err)
					err = proxyCl.Create(context.TODO(), appToCreate)

					// then
					require.EqualError(t, err, fmt.Sprintf(`applications.appstudio.redhat.com is forbidden: User "%[1]s" cannot create resource "applications" in API group "appstudio.redhat.com" in the namespace "%[2]s"`, user.compliantUsername, users[0].expectedMemberCluster.Namespace))
				})
			}

			t.Run("successful workspace context", func(t *testing.T) {
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(user.compliantUsername)
				// Start a new websocket watcher which watches for Application CRs in the user's namespace
				w := newWsWatcher(t, *user, user.compliantUsername, proxyWorkspaceURL)
				closeConnection := w.Start()
				defer closeConnection()
				proxyCl, err := hostAwait.CreateAPIProxyClient(t, user.token, proxyWorkspaceURL)
				require.NoError(t, err)

				// given
				applicationName := fmt.Sprintf("%s-workspace-context", user.compliantUsername)
				expectedApp := newApplication(applicationName, user.compliantUsername)

				// when
				err = proxyCl.Create(context.TODO(), expectedApp)
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
				createdApp := &hasv1alpha1.Application{}
				err = user.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: user.compliantUsername, Name: applicationName}, createdApp)
				require.NoError(t, err)
				require.NotEmpty(t, createdApp)
				assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
			})

			t.Run("invalid workspace context", func(t *testing.T) {
				proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext("notexist")
				// Start a new websocket watcher which watches for Application CRs in the user's namespace
				_, err := hostAwait.CreateAPIProxyClient(t, user.token, proxyWorkspaceURL)
				require.EqualError(t, err, `an error on the server ("unable to get target cluster: the requested space in not available") has prevented the request from succeeding`)
			})
		})
	} // end users loop

	t.Run("workspace sharing", func(t *testing.T) {
		// given
		userA := users[0]
		userB := users[1]
		applicationName := fmt.Sprintf("%s-share-workspace-context", userB.compliantUsername)
		proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(userB.compliantUsername) // set workspace context using userB's workspace

		// ensure the app exists in userB's space
		expectedApp := newApplication(applicationName, userB.compliantUsername)
		err := userB.expectedMemberCluster.Client.Create(context.TODO(), expectedApp)
		require.NoError(t, err)

		t.Run("userA request to unauthorized workspace", func(t *testing.T) {
			proxyCl, err := hostAwait.CreateAPIProxyClient(t, userA.token, proxyWorkspaceURL)
			require.NoError(t, err)

			// when
			actualApp := &hasv1alpha1.Application{}
			err = proxyCl.Get(context.TODO(), types.NamespacedName{Name: applicationName, Namespace: userB.compliantUsername}, actualApp)

			// then
			require.EqualError(t, err, fmt.Sprintf(`applications.appstudio.redhat.com "%s" is forbidden: User "%s" cannot get resource "applications" in API group "appstudio.redhat.com" in the namespace "%s"`, expectedApp.Name, userA.compliantUsername, userB.compliantUsername))

			// Double check that the Application does exist using a regular client (non-proxy)
			createdApp := &hasv1alpha1.Application{}
			err = userA.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: userB.compliantUsername, Name: applicationName}, createdApp)
			require.NoError(t, err)
			require.NotEmpty(t, createdApp)
			assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
		})

		t.Run("share userB workspace with userA", func(t *testing.T) {
			// share userB space with userA
			userAMur, err := hostAwait.GetMasterUserRecord(userA.compliantUsername)
			require.NoError(t, err)
			userBSpace, err := hostAwait.WaitForSpace(t, userB.compliantUsername, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
			require.NoError(t, err)
			CreateSpaceBinding(t, hostAwait, userAMur, userBSpace, "admin") // creating a spacebinding gives userA access to userB's space

			// VerifySpaceRelatedResources will verify the roles and rolebindings are updated to include userA's SpaceBinding
			VerifySpaceRelatedResources(t, awaitilities, userB.signup, "appstudio")

			// Start a new websocket watcher which watches for Application CRs in the user's namespace
			w := newWsWatcher(t, *userA, userB.compliantUsername, proxyWorkspaceURL)
			closeConnection := w.Start()
			defer closeConnection()
			proxyCl, err := hostAwait.CreateAPIProxyClient(t, userA.token, proxyWorkspaceURL)
			require.NoError(t, err)

			// when
			actualApp := &hasv1alpha1.Application{}
			err = proxyCl.Get(context.TODO(), types.NamespacedName{Name: applicationName, Namespace: userB.compliantUsername}, actualApp)

			// then
			require.NoError(t, err)

			// wait for the websocket watcher which uses the proxy to receive the Application CR
			found, err := w.WaitForApplication(
				userB.expectedMemberCluster.RetryInterval,
				userB.expectedMemberCluster.Timeout,
				expectedApp.Name,
			)
			require.NoError(t, err)
			assert.NotEmpty(t, found)

			// Double check that the Application does exist using a regular client (non-proxy)
			createdApp := &hasv1alpha1.Application{}
			err = userA.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: userB.compliantUsername, Name: applicationName}, createdApp)
			require.NoError(t, err)
			require.NotEmpty(t, createdApp)
			assert.Equal(t, expectedApp.Spec.DisplayName, createdApp.Spec.DisplayName)
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
	socketURL := fmt.Sprintf("wss://%s/apis/appstudio.redhat.com/v1alpha1/namespaces/%s/applications?watch=true", trimmedProxyURL, w.namespace)
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
		r, _ := ioutil.ReadAll(resp.Body)
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
