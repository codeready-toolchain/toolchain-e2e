package e2e

import (
	"fmt"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
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

	response := NewSignupRequest(s.Awaitilities).
		Username("consoletest").
		Email("consoletest@redhat.com").
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
		Execute(s.T()).GetSignupResponse(s.T())

	// Response should contain a ConsoleURL with a value something like:
	// https://console-openshift-console.apps.99b682869228f7464338-mgmt.ci.hypershift.devcluster.openshift.com/
	fmt.Printf("#### response value: %s", response)

	require.Contains(s.T(), response, "ConsoleURL")

	consoleURL := response["ConsoleURL"]
	fmt.Printf("#### Got Console URL: %s", consoleURL)
}
