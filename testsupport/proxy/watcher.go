package proxy

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

	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubewait "k8s.io/apimachinery/pkg/util/wait"
)

// wsWatcher represents a watcher which leverages a WebSocket connection to watch for Applications in the user's namespace.
// The connection is established with the reg-service proxy instead of direct connection to the API server.
type WsWatcher struct {
	done         chan interface{}
	interrupt    chan os.Signal
	t            *testing.T
	user         UserProxy
	namespace    string
	connection   *websocket.Conn
	proxyBaseURL string

	mu           sync.RWMutex
	receivedApps map[string]*appstudiov1.Application
}

// this test will:
//  1. provision a watcher user
//  2. run a goroutine which will:
//     I. create a long-running GET call with a watch=true parameter
//     II. the call will be terminated via a context timeout
//     III. check the expected error that it was terminated via a context and not on the server side
func RunWatcher(t *testing.T, awaitilities wait.Awaitilities) *sync.WaitGroup {

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
	watchUser := &UserProxy{
		ExpectedMemberCluster: awaitilities.Member1(),
		Username:              "watcher",
		IdentityID:            uuid.Must(uuid.NewV4()),
	}
	CreateAppStudioUser(t, awaitilities, watchUser)

	proxyConfig := awaitilities.Host().CreateAPIProxyConfig(t, watchUser.Token, awaitilities.Host().APIProxyURL)
	proxyConfig.Timeout = clientConfigTimeout
	watcherClient, err := kubernetes.NewForConfig(proxyConfig)
	require.NoError(t, err)

	// we need to get a list of ConfigMaps, so we can use the resourceVersion
	// of the list resource in the watch call
	t.Log("getting the first list of ConfigMaps")
	list, err := watcherClient.CoreV1().
		ConfigMaps(TenantNsName(watchUser.CompliantUsername)).
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
				AbsPath(fmt.Sprintf("/api/v1/namespaces/%s/configmaps", TenantNsName(watchUser.CompliantUsername))).
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

func NewWsWatcher(t *testing.T, user UserProxy, namespace, proxyURL string) *WsWatcher {
	_, err := url.Parse(proxyURL)
	require.NoError(t, err)
	return &WsWatcher{
		t:            t,
		namespace:    namespace,
		user:         user,
		proxyBaseURL: proxyURL,
	}
}

// start creates a new WebSocket connection. The method returns a function which is to be used to close the connection when done.
func (w *WsWatcher) Start() func() {
	w.done = make(chan interface{})    // Channel to indicate that the receiverHandler is done
	w.interrupt = make(chan os.Signal) // Channel to listen for interrupt signal to terminate gracefully

	signal.Notify(w.interrupt, os.Interrupt) // Notify the interrupt channel for SIGINT

	encodedToken := base64.RawURLEncoding.EncodeToString([]byte(w.user.Token))
	protocol := fmt.Sprintf("base64url.bearer.authorization.k8s.io.%s", encodedToken)

	trimmedProxyURL := strings.TrimPrefix(w.proxyBaseURL, "https://")
	socketURL := fmt.Sprintf("wss://%s/apis/appstudio.redhat.com/v1alpha1/namespaces/%s/applications?watch=true", trimmedProxyURL, TenantNsName(w.namespace))
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
func (w *WsWatcher) startMainLoop() {
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
func (w *WsWatcher) receiveHandler() {
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

func (w *WsWatcher) WaitForApplication(expectedAppName string) (*appstudiov1.Application, error) {
	var foundApp *appstudiov1.Application
	err := kubewait.Poll(wait.DefaultRetryInterval, wait.DefaultTimeout, func() (bool, error) {
		defer w.mu.RUnlock()
		w.mu.RLock()
		foundApp = w.receivedApps[expectedAppName]
		return foundApp != nil, nil
	})
	return foundApp, err
}

func (w *WsWatcher) WaitForApplicationDeletion(expectedAppName string) error {
	err := kubewait.PollImmediate(wait.DefaultRetryInterval, wait.DefaultTimeout, func() (bool, error) {
		defer w.mu.RUnlock()
		w.mu.RLock()
		_, present := w.receivedApps[expectedAppName]
		return !present, nil
	})
	return err
}
