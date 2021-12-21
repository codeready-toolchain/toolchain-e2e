package e2e

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gorilla/websocket"
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
	signup                *toolchainv1alpha1.UserSignup
}

// full flow from usersignup with approval down to namespaces creation and cleanup
func TestProxyFlow(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	// check that the tier exists, and all its namespace other cluster-scoped resource revisions
	// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

	users := []proxyUser{
		{
			expectedMemberCluster: memberAwait,
			username:              "proxymember1",
		},
		{
			expectedMemberCluster: memberAwait2,
			username:              "proxymember2",
		},
	}
	promotionTier := "appstudio"

	for index, user := range users {
		t.Run(user.username, func(t *testing.T) {
			// Create and approve signup
			req := NewSignupRequest(t, awaitilities).
				Username(user.username).
				ManuallyApprove().
				TargetCluster(user.expectedMemberCluster).
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute()

			user.signup, _ = req.Resources()
			user.token = req.GetToken()

			VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, "base")
			_, err := hostAwait.GetMasterUserRecord(wait.WithMurName(user.username))
			require.NoError(t, err)

			// since the registration service always provisions users to the default tier users need to be
			// promoted to the appstudio tier in order to test proxy scenarios
			t.Run("promote to appstudio tier", func(t *testing.T) {
				// given
				changeTierRequest := NewChangeTierRequest(hostAwait.Namespace, user.signup.Status.CompliantUsername, promotionTier)

				// when
				err = hostAwait.CreateWithCleanup(context.TODO(), changeTierRequest)

				// then
				require.NoError(t, err)
				_, err := hostAwait.WaitForChangeTierRequest(changeTierRequest.Name, toBeComplete)
				require.NoError(t, err)
				VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, promotionTier)

				// then - wait until ChangeTierRequest is deleted by our automatic GC
				err = hostAwait.WaitUntilChangeTierRequestDeleted(changeTierRequest.Name)
				assert.NoError(t, err)
			})

			t.Run("use proxy to create a configmap in the user appstudio namespace via proxy API and use websocket to watch it created", func(t *testing.T) {
				// given
				cmName := fmt.Sprintf("%s-test-cm", user.username)
				expectedCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: user.username,
					},
					Data: map[string]string{
						"planet": "venus",
					},
				}
				// Start a new websocket watcher which watches for config maps in the user's namespace
				w := newWsWatcher(t, proxyUser{})
				closeConnection := w.Start()
				defer closeConnection()

				// when
				proxyCl := hostAwait.CreateAPIProxyClient(user.token)
				err := proxyCl.Create(context.TODO(), expectedCM)
				require.NoError(t, err)

				// then
				// wait for the websocket watcher which uses the proxy to receive the config map
				found, err := w.WaitForConfigMap(
					user.expectedMemberCluster.RetryInterval,
					user.expectedMemberCluster.Timeout,
					expectedCM.Name,
				)
				require.NoError(t, err)
				assert.NotEmpty(t, found)

				// Double check that the CM does exist using a regular client (non-proxy)
				createdCM := &corev1.ConfigMap{}
				err = user.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: user.username, Name: cmName}, createdCM)
				require.NoError(t, err)
				require.NotEmpty(t, createdCM)
				require.Equal(t, "venus", createdCM.Data["planet"])
			})

			t.Run("try to create a resource in an unauthorized namespace", func(t *testing.T) {
				// given
				cmName := fmt.Sprintf("%s-proxy-test-cm", user.username)
				expectedCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: hostAwait.Namespace, // user should not be allowed to create a resource in the host operator namespace
					},
					Data: map[string]string{
						"planet": "venus",
					},
				}

				// when
				proxyCl := hostAwait.CreateAPIProxyClient(user.token)

				// then
				err := proxyCl.Create(context.TODO(), expectedCM)
				require.EqualError(t, err, fmt.Sprintf(`configmaps is forbidden: User "system:serviceaccount:%[1]s:appstudio-%[1]s" cannot create resource "configmaps" in API group "" in the namespace "%[2]s"`, user.username, hostAwait.Namespace))
			})

			if index == 1 { // only for the second user
				t.Run("try to create a resource in the other users namespace", func(t *testing.T) {
					// given
					// verify first user's namespace still exists
					ns := &corev1.Namespace{}
					err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: users[0].username}, ns)
					require.NoError(t, err, "failed to verify the first user's namespace still exists")

					cmName := fmt.Sprintf("%s-proxy-test-cm", users[0].username)
					expectedCM := &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      cmName,
							Namespace: users[0].expectedMemberCluster.Namespace, // user should not be allowed to create a resource in the first user's namespace
						},
						Data: map[string]string{
							"planet": "venus",
						},
					}

					// when
					proxyCl := hostAwait.CreateAPIProxyClient(user.token)
					err = proxyCl.Create(context.TODO(), expectedCM)

					// then
					require.EqualError(t, err, fmt.Sprintf(`configmaps is forbidden: User "system:serviceaccount:%[1]s:appstudio-%[1]s" cannot create resource "configmaps" in API group "" in the namespace "%[2]s"`, user.username, users[0].expectedMemberCluster.Namespace))
				})
			}
		})
	} // end users loop
}

func newWsWatcher(t *testing.T, user proxyUser) *wsWatcher {
	return &wsWatcher{
		t:    t,
		user: user,
	}
}

// wsWatcher represents a watcher which leverages a WebSocket connection to watch for config maps in the user's namespace.
// The connection is established with the reg-service proxy instead of direct connection to the API server.
type wsWatcher struct {
	done       chan interface{}
	interrupt  chan os.Signal
	t          *testing.T
	user       proxyUser
	connection *websocket.Conn

	mu          sync.RWMutex
	receivedCMs map[string]*corev1.ConfigMap
}

// start creates a new WebSocket connection. The method returns a function which is to be used to close the connection when done.
func (w *wsWatcher) Start() func() {
	w.done = make(chan interface{})    // Channel to indicate that the receiverHandler is done
	w.interrupt = make(chan os.Signal) // Channel to listen for interrupt signal to terminate gracefully

	signal.Notify(w.interrupt, os.Interrupt) // Notify the interrupt channel for SIGINT

	encodedToken := base64.RawURLEncoding.EncodeToString([]byte(w.user.token))
	protocol := fmt.Sprintf("base64url.bearer.authorization.k8s.io.%s", encodedToken)

	config := w.user.expectedMemberCluster.RestConfig
	socketURL := fmt.Sprintf("wss://%s/api/v1/namespaces/%s/configmaps?watch=true", config.Host, w.user.username)
	dialer := &websocket.Dialer{
		//HandshakeTimeout: 45 * time.Second,
		Subprotocols:    []string{protocol, "base64.binary.k8s.io"},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	extraHeaders := make(http.Header, 1)
	extraHeaders.Add("Origin", "http://localhost")

	conn, _, err := dialer.Dial(socketURL, extraHeaders)
	require.NoError(w.t, err)
	w.connection = conn
	w.receivedCMs = make(map[string]*corev1.ConfigMap)

	go w.receiveHandler()
	go w.startMainLoop()

	return func() {
		_ = w.connection.Close()
	}
}

// startMainLoop starts the main loop for the client. Packets are sent here.
func (w *wsWatcher) startMainLoop() {
	for {
		select {
		case <-time.After(time.Duration(1) * time.Millisecond * 1000):
			// Send an echo packet every second
			err := w.connection.WriteMessage(websocket.TextMessage, []byte("Hello from e2e tests!"))
			require.NoError(w.t, err, "error during writing to websocket")
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

// receiveHandler listens to the incoming messages and stores them as config map objects
func (w *wsWatcher) receiveHandler() {
	defer close(w.done)
	for {
		_, msg, err := w.connection.ReadMessage()
		require.NoError(w.t, err, "error in receive")
		w.t.Logf("Received: %s", msg)
		cm := corev1.ConfigMap{}
		err = json.Unmarshal(msg, &cm)
		require.NoError(w.t, err)
		copyCM := cm
		w.mu.Lock()
		w.receivedCMs[cm.Name] = &copyCM
		w.mu.Unlock()
	}
}

func (w *wsWatcher) WaitForConfigMap(retryInterval, timeout time.Duration, expectedCMName string) (*corev1.ConfigMap, error) {
	var foundCM *corev1.ConfigMap
	err := kubewait.Poll(retryInterval, timeout, func() (bool, error) {
		defer w.mu.RUnlock()
		w.mu.RLock()
		foundCM = w.receivedCMs[expectedCMName]
		return foundCM != nil, nil
	})
	return foundCM, err
}
