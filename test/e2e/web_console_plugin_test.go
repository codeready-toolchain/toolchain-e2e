package e2e

import (
	"crypto/tls"
	"fmt"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"io"
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

	// Response should contain a ConsoleURL with a value something like:
	// https://console-openshift-console.apps.99b682869228f7464338-mgmt.ci.hypershift.devcluster.openshift.com/
	require.Contains(s.T(), response, "consoleURL")

	consoleURL := response["consoleURL"]
	manifestURL := fmt.Sprintf("%s%s", consoleURL, "api/plugins/toolchain-member-web-console-plugin/plugin-manifest.json")
	healthCheckURL := fmt.Sprintf("%s%s", consoleURL, "api/plugins/toolchain-member-web-console-plugin/status")

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
	var err error
	var resp *http.Response
	retries := 0

	req, err := http.NewRequest("GET", healthCheckURL, nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", signupRequest.GetToken())

	for retries < 5 {
		resp, err = httpClient.Do(req) //nolint
		if err != nil {
			// Wait an increasing amount of time before attempting again
			retries++
			time.Sleep(time.Duration(retries*5) * time.Second)
			continue
		}
		break
	}

	require.NoError(s.T(), err)
	require.Equal(s.T(), http.StatusOK, resp.StatusCode)

	req, err = http.NewRequest("GET", manifestURL, nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", signupRequest.GetToken())

	resp, err = httpClient.Do(req)
	require.NoError(s.T(), err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(s.T(), err)

	require.True(s.T(), strings.HasPrefix(string(body), "{\n  \"name\": \"toolchain-member-web-console-plugin\","))
}
