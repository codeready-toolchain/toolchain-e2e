package e2e

import (
	"testing"

	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
)

func TestSetDefaultTier(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	t.Run("original default tier", func(t *testing.T) {
		// Create and approve a new user that should be provisioned to the base tier
		NewSignupRequest(awaitilities).
			Username("defaulttier").
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)
	})

	t.Run("changed default tier configuration", func(t *testing.T) {
		hostAwait.UpdateToolchainConfig(t, testconfig.Tiers().DefaultUserTier("deactivate30").DefaultSpaceTier("advanced"))
		// Create and approve a new user that should be provisioned to the advanced tier
		NewSignupRequest(awaitilities).
			Username("defaulttierchanged").
			ManuallyApprove().
			TargetCluster(memberAwait).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			Execute(t)
	})
}
