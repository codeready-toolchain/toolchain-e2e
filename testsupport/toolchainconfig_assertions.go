package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
)

func VerifyToolchainConfig(t *testing.T, hostAwait *wait.HostAwaitility, criteria ...wait.ToolchainConfigWaitCriterion) {
	hostAwait.WaitForToolchainConfig(t, criteria...)
}

func VerifyMemberOperatorConfig(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, criteria ...wait.MemberOperatorConfigWaitCriterion) {
	memberAwait.WaitForMemberOperatorConfig(t, hostAwait, criteria...)
}
