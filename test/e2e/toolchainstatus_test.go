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
		// set the che required flag to true to force an error on the memberstatus (che is not installed in e2e test environments)
		memberConfigurationWithCheRequired := testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.Che().Required(true))
		hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

		err := memberAwait.WaitForMemberStatus(t,
			wait.UntilMemberStatusHasConditions(wait.ToolchainStatusComponentsNotReady("[routes]")))
		require.NoError(t, err, "failed while waiting for MemberStatus to contain error due to che being required")

		_, err = hostAwait.WaitForToolchainStatus(t,
			wait.UntilToolchainStatusHasConditions(wait.ToolchainStatusComponentsNotReady("[members]"), wait.ToolchainStatusUnreadyNotificationNotCreated()))
		require.NoError(t, err, "failed while waiting for ToolchainStatus to contain error due to che being required")

		t.Run("verify member and toolchain status go back to ready", func(t *testing.T) {
			// change che required flag back to true to resolve the error on the memberstatus
			memberConfigurationWithCheRequired = testconfig.ModifyMemberOperatorConfigObj(memberAwait.GetMemberOperatorConfig(t), testconfig.Che().Required(false))
			hostAwait.UpdateToolchainConfig(t, testconfig.Members().Default(memberConfigurationWithCheRequired.Spec))

			VerifyMemberStatus(t, memberAwait, consoleURL)
			VerifyToolchainStatus(t, hostAwait, memberAwait)
		})
	})
}
