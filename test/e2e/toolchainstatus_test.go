package e2e

import (
	"testing"

	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

func TestToolchainStatusUnready(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	consoleURL := memberAwait.GetConsoleURL(t)
	VerifyMemberStatus(t, memberAwait, consoleURL)
	VerifyToolchainStatus(t, hostAwait, memberAwait)

	t.Run("verify updated toolchainconfig is synced - go to unready", func(t *testing.T) {
		// capture original member config before mutation
		origMemberCfg := memberAwait.GetMemberOperatorConfig(t)
		
		// set an invalid console route name to force an error on the memberstatus (console route won't be found)
		memberConfigurationWithInvalidConsole := testconfig.ModifyMemberOperatorConfigObj(origMemberCfg, testconfig.Console().RouteName("nonexistent-console-route"))
		hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithInvalidConsole.Spec))
		t.Cleanup(func() {
			// ensure restore even if the test fails earlier
			hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(origMemberCfg.Spec))
		})

		err := memberAwait.WaitForMemberStatus(t,
			wait.UntilMemberStatusHasConditions(wait.ToolchainStatusComponentsNotReady("[routes]")))
		require.NoError(t, err, "failed while waiting for MemberStatus to contain error due to invalid console route")

		_, err = hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(wait.ToolchainStatusComponentsNotReady("[members]"), wait.ToolchainStatusUnreadyNotificationNotCreated()))
		require.NoError(t, err, "failed while waiting for ToolchainStatus to contain error due to invalid console route")

		t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
			// restore original member config captured before mutation
			hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(origMemberCfg.Spec))

			VerifyMemberStatus(t, memberAwait, consoleURL)
			VerifyToolchainStatus(t, hostAwait, memberAwait)
		})
	})
}
