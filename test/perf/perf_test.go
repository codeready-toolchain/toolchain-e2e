package perf

import (
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPerformance(t *testing.T) {
	// given
	ctx, hostAwait, memberAwait := WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	defer ctx.Cleanup()

	// host metrics should become available at this point
	metricsService, err := hostAwait.WaitForMetricsService("host-operator-metrics")
	require.NoError(t, err, "failed while waiting for the 'host-operator-metrics' service")

	count := 1000
	t.Run(fmt.Sprintf("%d users", count), func(t *testing.T) {
		// given
		users := CreateMultipleSignups(t, ctx, hostAwait, memberAwait, count)
		for _, user := range users {
			_, err := hostAwait.WaitForMasterUserRecord(user.Spec.Username, UntilMasterUserRecordHasCondition(Provisioned()))
			require.NoError(t, err)
		}

		// when deleting the host-operator pod to emulate an operator restart during redeployment.
		err := hostAwait.DeletePods(client.MatchingLabels{"name": "host-operator"})

		// then check how much time it takes to restart and process all existing resources
		require.NoError(t, err)

		host := hostAwait
		host.Timeout = 30 * time.Minute
		// host metrics should become available again at this point
		metricsRoute, err := hostAwait.SetupRouteForService(metricsService, "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics' service to be available")

		start := time.Now()
		// measure time it takes to have an empty queue on the master-user-records
		err = host.WaitUntilMetricsCounterHasValue(metricsRoute.Status.Ingress[0].Host, "controller_runtime_reconcile_total", "controller", "usersignup-controller", float64(count))
		assert.NoError(t, err, "failed to reach the expected number of reconcile loops")
		err = host.WaitUntilMetricsCounterHasValue(metricsRoute.Status.Ingress[0].Host, "workqueue_depth", "name", "usersignup-controller", 0)
		assert.NoError(t, err, "failed to reach the expected queue depth")
		end := time.Now()
		fmt.Printf("time to process the resource: %dms\n", end.Sub(start).Milliseconds())
	})

}
