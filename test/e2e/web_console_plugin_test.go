package e2e

import (
	"fmt"
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
	// when & then
	response := NewSignupRequest(s.Awaitilities).
		Username("consoletest").
		Email("consoletest@redhat.com").
		EnsureMUR().
		RequireConditions(ConditionSet(Default(), ApprovedAutomatically())...).
		Execute(s.T()).SignupResponse()

	require.Contains(s.T(), response, "ConsoleURL")

	consoleURL := response["ConsoleURL"]
	fmt.Printf("#### Got Console URL: %s", consoleURL)
}
