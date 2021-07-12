package testsupport

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
)

// VerifyHostMetricsService verifies that there is a service called `host-operator-metrics-service`
// in the host namespace.
func VerifyHostMetricsService(t *testing.T, hostAwait *wait.HostAwaitility) {
	_, err := hostAwait.WaitForMetricsService("host-operator-metrics-service")
	require.NoError(t, err, "failed while waiting for 'host-operator-metrics-service' service")
}

// VerifyMemberMetricsService verifies that there is a service called `member-operator-metrics-service`
// in the member namespace.
func VerifyMemberMetricsService(t *testing.T, memberAwait *wait.MemberAwaitility) {
	_, err := memberAwait.WaitForMetricsService("member-operator-metrics-service")
	require.NoError(t, err, "failed while waiting for 'member-operator-metrics-service' service")
}
