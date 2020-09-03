package perf

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/go-logr/logr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestPerformance(t *testing.T) {
	// given
	logger, out, err := initLogger()
	require.NoError(t, err)
	defer out.Close()
	ctx, hostAwait, memberAwait := WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	defer ctx.Cleanup()

	// host metrics should become available at this point
	metricsService, err := hostAwait.WaitForMetricsService("host-operator-metrics")
	require.NoError(t, err, "failed while waiting for the 'host-operator-metrics' service")
	// host metrics should become available again at this point
	metricsRoute, err := hostAwait.SetupRouteForService(metricsService, "/metrics")
	require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics' service to be available")

	count := 10
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

		start := time.Now()
		// measure time it takes to have an empty queue on the master-user-records
		err = host.WaitUntilMetricsCounterHasValue(metricsRoute.Status.Ingress[0].Host, "controller_runtime_reconcile_total", "controller", "usersignup-controller", float64(count))
		assert.NoError(t, err, "failed to reach the expected number of reconcile loops")
		err = host.WaitUntilMetricsCounterHasValue(metricsRoute.Status.Ingress[0].Host, "workqueue_depth", "name", "usersignup-controller", 0)
		assert.NoError(t, err, "failed to reach the expected queue depth")
		end := time.Now()
		logger.Info("done processing resources", "provisioned_users_count", count, "usersignup_processing_duration_ms", end.Sub(start).Milliseconds())
	})

}

// initLogger initializes a logger which will write to `$(ARTIFACT_DIR)/perf-<YYYYMMDD-HHmmSS>.log` or `./tmp/perf-<YYYYMMDD-HHmmSS>.log` if no `ARTIFACT_DIR`
// env var is defined.
// Notes:
// - the target directory will be created on-the-fly if needed
// - it's up to the caller to close the returned file at the end of the tests
func initLogger() (logr.Logger, *os.File, error) {
	// log messages that need to be retained after the OpenShift CI job completion must be written in a file located in `${ARTIFACT_DIR}`
	var artifactDir string
	if artifactDir = os.Getenv("ARTIFACT_DIR"); artifactDir == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		artifactDir = filepath.Join(pwd, "tmp")
	}
	if _, err := os.Open(artifactDir); os.IsNotExist(err) {
		// make sure that `./tmp` exists
		if err = os.MkdirAll(artifactDir, os.ModeDir+os.ModePerm); err != nil {
			return nil, nil, err
		}
	}

	out, err := os.Create(path.Join(artifactDir, fmt.Sprintf("perf-%s.log", time.Now().Format("20060102-030405"))))
	if err != nil {
		return nil, nil, err
	}
	logger := zap.New(zap.WriteTo(out))
	fmt.Printf("configured logger to write messages in '%s'\n", out.Name())
	return logger, out, nil
}
