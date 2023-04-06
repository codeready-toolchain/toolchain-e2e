package e2e

import (
	"crypto/tls"
	"fmt"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	v1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	wait2 "k8s.io/apimachinery/pkg/util/wait"
	"net/http"
	"strings"
	"testing"
	"time"
)

type webConsolePluginTest struct {
	suite.Suite
	wait.Awaitilities
}

func TestRunWebConsolePluginTest(t *testing.T) {
	suite.Run(t, &webConsolePluginTest{})
}

func (s *webConsolePluginTest) SetupSuite() {
	s.Awaitilities = WaitForDeployments(s.T())
}

func (s *webConsolePluginTest) TearDownTest() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	memberAwait2 := s.Member2()
	hostAwait.Clean(s.T())
	memberAwait.Clean(s.T())
	memberAwait2.Clean(s.T())
}

func (s *webConsolePluginTest) TestWebConsoleDeployedSuccessfully() {
	// Provision a new user
	hostAwait := s.Host()
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(true))

	signupRequest := NewSignupRequest(s.Awaitilities).
		Username("consoletest").
		Email("consoletest@redhat.com").
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
		Execute(s.T())
	response := signupRequest.GetSignupResponse(s.T())

	_, mur := signupRequest.Resources()

	memberAwait := GetMurTargetMember(s.T(), s.Awaitilities, mur)

	// Response should contain a ConsoleURL with a value something like:
	// https://console-openshift-console.apps.99b682869228f7464338-mgmt.ci.hypershift.devcluster.openshift.com/
	require.Contains(s.T(), response, "consoleURL")

	// Since we can't easily access the web console API resources directly (due to complex security requirements) we
	// will instead create a route in the member cluster with which to access the console plugin

	// Before we can create the route however, we need the certificate and private key values from the
	// member-operator-console-plugin secret
	secret, err := memberAwait.WaitForSecret(s.T(), memberAwait.Namespace, "member-operator-console-plugin")
	require.NoError(s.T(), err)

	// Now we can create the route resource
	route := &v1.Route{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consolepluginroute",
			Namespace: memberAwait.Namespace,
			Annotations: map[string]string{
				"openshift.io/host.generated": "true",
			},
		},
		Spec: v1.RouteSpec{
			To: v1.RouteTargetReference{
				Kind: "Service",
				Name: "member-operator-console-plugin",
			},
			Port: &v1.RoutePort{
				TargetPort: intstr.FromInt(9443),
			},
			TLS: &v1.TLSConfig{
				Termination: "reencrypt",
				Certificate: string(secret.Data["tls.crt"]),
				Key:         string(secret.Data["tls.key"]),
			},
			WildcardPolicy: "None",
		},
	}

	err = memberAwait.Create(s.T(), route)
	require.NoError(s.T(), err)

	reloaded, err := memberAwait.WaitForRouteToBeAvailable(s.T(), route.Namespace, route.Name, "/status")
	require.NoError(s.T(), err, "route not available", route)

	// Construct the routeURL from the route host
	routeURL := reloaded.Spec.Host + "/"

	manifestURL := fmt.Sprintf("%s%s", routeURL, "plugin-manifest.json")
	healthCheckURL := fmt.Sprintf("%s%s", routeURL, "status")

	var tr *http.Transport
	{ // #nosec G402
		tr = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	httpClient := &http.Client{Transport: tr}

	// First perform a health check - we will attempt up to 5 times to invoke the health check endpoint without error
	var healthCheckResponse *http.Response

	req, err := http.NewRequest("GET", healthCheckURL, nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", signupRequest.GetToken())

	err = wait2.Poll(time.Second*5, time.Minute, func() (bool, error) {
		healthCheckResponse, err = httpClient.Do(req) //nolint
		if err != nil {
			return false, err
		}
		defer healthCheckResponse.Body.Close()
		return true, nil
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), http.StatusOK, healthCheckResponse.StatusCode, "error polling health check endpoint", route)

	req, err = http.NewRequest("GET", manifestURL, nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", signupRequest.GetToken())

	manifestResponse, err := httpClient.Do(req)
	require.NoError(s.T(), err)
	defer manifestResponse.Body.Close()

	body, err := io.ReadAll(manifestResponse.Body)
	require.NoError(s.T(), err)

	require.True(s.T(), strings.HasPrefix(string(body), "{\n  \"name\": \"toolchain-member-web-console-plugin\","))
}
