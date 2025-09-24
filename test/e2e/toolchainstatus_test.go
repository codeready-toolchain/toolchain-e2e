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
		// set an invalid console route name to force an error on the memberstatus (console route won't be found)
		memberConfigurationWithInvalidConsole := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.Console().RouteName("nonexistent-console-route"))
		hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithInvalidConsole.Spec))

		err := memberAwait.WaitForMemberStatus(t,
			wait.UntilMemberStatusHasConditions(wait.ToolchainStatusComponentsNotReady("[routes]")))
		require.NoError(t, err, "failed while waiting for MemberStatus to contain error due to invalid console route")

		_, err = hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(wait.ToolchainStatusComponentsNotReady("[members]"), wait.ToolchainStatusUnreadyNotificationNotCreated()))
		require.NoError(t, err, "failed while waiting for ToolchainStatus to contain error due to invalid console route")

		t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
			// change console route name back to default to resolve the error on the memberstatus
			memberConfigurationWithValidConsole := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.Console().RouteName("console"))
			hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithValidConsole.Spec))

			VerifyMemberStatus(t, memberAwait, consoleURL)
			VerifyToolchainStatus(t, hostAwait, memberAwait)
		})
	})
}
