package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/stretchr/testify/require"
)

// VerifyHostMetricsService verifies that there is a service called `host-operator-metrics`
// in the host namespace.
func VerifyHostMetricsService(t *testing.T, hostAwait *wait.HostAwaitility) {
	_, err := hostAwait.WaitForMetricsService()
	require.NoError(t, err, "failed while waiting for 'host-operator-metrics' service")
}

// VerifyMemberMetricsService verifies that there is a service called `member-operator-metrics`
// in the host namespace.
func VerifyMemberMetricsService(t *testing.T, memberAwait *wait.MemberAwaitility) {
	_, err := memberAwait.WaitForMetricsService()
	require.NoError(t, err, "failed while waiting for 'host-operator-metrics' service")
}
