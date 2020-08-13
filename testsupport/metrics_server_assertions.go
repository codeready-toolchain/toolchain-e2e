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
