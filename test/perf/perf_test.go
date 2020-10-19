package perf

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	. "github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/go-logr/logr"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestPerformance(t *testing.T) {
	// given
	// host metrics should become available at this point
	config := NewConfiguration()

	logger, out, err := initLogger()
	require.NoError(t, err)
	defer out.Close()
	ctx, hostAwait, memberAwait := WaitForDeployments(t, &toolchainv1alpha1.UserSignupList{})
	hostAwait.Timeout = 5 * time.Minute
	memberAwait.Timeout = 5 * time.Minute

	// host metrics should become available at this point
	hostMetricsService, err := hostAwait.WaitForMetricsService("host-operator-metrics")
	require.NoError(t, err, "failed while waiting for the 'host-operator-metrics' service")
	// host metrics should become available again at this point
	hostMetricsRoute, err := hostAwait.SetupRouteForService(hostMetricsService.Name, "/metrics")
	require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics' service to be available")
	defer ctx.Cleanup()

	t.Run(fmt.Sprintf("provision %d users", config.GetUserCount()), func(t *testing.T) {
		// given
		createSignupsByBatch(t, ctx, hostAwait, memberAwait, config, logger)

		t.Run("restart host operator pod", func(t *testing.T) {

			// when deleting the host-operator pod to emulate an operator restart during redeployment.
			err := hostAwait.DeletePods(client.MatchingLabels{"name": "host-operator"})

			// then check how much time it takes to restart and process all existing resources
			require.NoError(t, err)

			// host metrics should become available again at this point
			_, err = hostAwait.WaitForRouteToBeAvailable(hostMetricsRoute.Namespace, hostMetricsRoute.Name, "/metrics")
			require.NoError(t, err, "failed while setting up or waiting for the route to the 'host-operator-metrics' service to be available")

			start := time.Now()
			// measure time it takes to have an empty queue on the master-user-records
			err = hostAwait.WaitUntilMetricHasValueOrMore(hostMetricsRoute.Status.Ingress[0].Host, "controller_runtime_reconcile_total", "controller", "usersignup-controller", float64(config.GetUserCount()))
			assert.NoError(t, err, "failed to reach the expected number of reconcile loops")
			err = hostAwait.WaitUntilMetricHasValueOrLess(hostMetricsRoute.Status.Ingress[0].Host, "workqueue_depth", "name", "usersignup-controller", 0)
			assert.NoError(t, err, "failed to reach the expected queue depth")
			end := time.Now()
			hostOperatorPod, err := hostAwait.GetHostOperatorPod()
			require.NoError(t, err)
			hostOperatorPodMemory, err := hostAwait.GetMemoryUsage(hostOperatorPod.Name, hostAwait.Namespace)
			require.NoError(t, err)
			logger.Info("done processing resources",
				"host_operator_pod_restart_duration_ms", end.Sub(start).Milliseconds(),
				"host_operator_pod_memory_usage_kb", hostOperatorPodMemory)
		})

		t.Run("restart member operator pod", func(t *testing.T) {

			// when deleting the host-operator pod to emulate an operator restart during redeployment.
			err := memberAwait.DeletePods(client.MatchingLabels{"name": "member-operator"})

			// then check how much time it takes to restart and process all existing resources
			require.NoError(t, err)

			// member metrics should become available again at this point
			memberMetricsRoute, err := memberAwait.SetupRouteForService("member-operator-metrics", "/metrics")
			require.NoError(t, err, "failed while setting up or waiting for the route to the 'member-operator-metrics' service to be available")

			start := time.Now()
			// measure time it takes to have an empty queue on the master-user-records
			err = memberAwait.WaitUntilMetricHasValueOrMore(memberMetricsRoute.Status.Ingress[0].Host, "controller_runtime_reconcile_total", "controller", "useraccount-controller", float64(2*config.GetUserCount()))
			assert.NoError(t, err, "failed to reach the expected number of reconcile loops")
			err = memberAwait.WaitUntilMetricHasValueOrLess(memberMetricsRoute.Status.Ingress[0].Host, "workqueue_depth", "name", "useraccount-controller", 0)
			assert.NoError(t, err, "failed to reach the expected queue depth")
			end := time.Now()
			memberOperatorPod, err := memberAwait.GetMemberOperatorPod()
			require.NoError(t, err)
			memberOperatorPodMemory, err := memberAwait.GetMemoryUsage(memberOperatorPod.Name, memberAwait.Namespace)
			require.NoError(t, err)

			logger.Info("done processing resources",
				"member_operator_pod_restart_duration_ms", end.Sub(start).Milliseconds(),
				"member_operator_pod_memory_usage_kb", memberOperatorPodMemory)
		})
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

// createSignupsByBatch creates signups by batch (see `config.GetUserBatchSize()`) and monitors the CPU and memory while the
// provisioning is in progress. Logs the max CPU and memory during captured during each batch by polling the `/metrics`
// endpoint in a separate go routine.
func createSignupsByBatch(t *testing.T, ctx *framework.Context, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, config Configuration, logger logr.Logger) {

	require.Equal(t, 0, config.GetUserCount()%config.GetUserBatchSize(), "number of accounts must be a multiple of %d", config.GetUserBatchSize())
	hostMetricsRoute, err := hostAwait.SetupRouteForService("host-operator-metrics", "/metrics")
	require.NoError(t, err)

	t.Logf("provisionning %d accounts by batch of '%d", config.GetUserCount(), config.GetUserBatchSize())

	hostOperatorPod, err := hostAwait.GetHostOperatorPod()
	require.NoError(t, err)
	memberOperatorPod, err := memberAwait.GetMemberOperatorPod()
	require.NoError(t, err)

	for b := 0; b < config.GetUserCount()/config.GetUserBatchSize(); b++ { // provisioning users by batch of `config.GetUserBatchSize()` (eg: 100)

		signups := make([]toolchainv1alpha1.UserSignup, config.GetUserBatchSize())
		start := time.Now()

		for i := 0; i < config.GetUserBatchSize(); i++ {
			n := b*config.GetUserBatchSize() + i
			name := fmt.Sprintf("multiple-signup-testuser-%d", n)
			// Create an approved UserSignup resource
			userSignup := NewUserSignup(t, hostAwait, memberAwait, name, fmt.Sprintf("multiple-signup-testuser-%d@test.com", n), false)
			userSignup.Spec.Approved = true
			err := hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(ctx))
			hostAwait.T.Logf("created usersignup with username: '%s' and resource name: '%s'", userSignup.Spec.Username, userSignup.Name)
			require.NoError(t, err)
			signups[i] = *userSignup
		}
		err := hostAwait.WaitUntilMetricHasValueOrLess(hostMetricsRoute.Status.Ingress[0].Host, "workqueue_depth", "name", "masteruserrecord-controller", 0)
		require.NoError(t, err)

		for _, signup := range signups {
			mur, err := hostAwait.WaitForMasterUserRecord(signup.Spec.Username, UntilMasterUserRecordHasCondition(Provisioned()))
			require.NoError(t, err)
			// now, run a pod (with the `sleep 28800` command in each namespace)
			userAccount, err := memberAwait.WaitForUserAccount(mur.Name,
				wait.UntilUserAccountHasConditions(Provisioned()))
			require.NoError(t, err)
			for _, templateRef := range userAccount.Spec.NSTemplateSet.Namespaces {
				ns, err := memberAwait.WaitForNamespace(mur.Name, templateRef.TemplateRef)
				require.NoError(t, err)
				if ns.Labels["toolchain.dev.openshift.com/type"] != "stage" {
					// skip pod creation if the namespace is not "stage", otherwise, we may run out of capacity of pods on the nodes
					continue
				}
				pod := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ns.Name,
						Name:      "sleep",
						Labels: map[string]string{ // just so we can list them from the CLI if needed
							"toolchain.dev.openshift.com/type": "test",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "sleep",
								Image:   "busybox",
								Command: []string{"sleep", "28800"}, // 8 hours - same as for idler
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										"cpu":    resource.MustParse("1m"),
										"memory": resource.MustParse("8Mi"),
									},
								},
							},
						},
					},
				}
				err = memberAwait.Client.Create(context.TODO(), &pod)
				require.NoError(t, err)
			}
		}

		end := time.Now()
		t.Logf("sleeping for %ds...", int(config.GetUserBatchPause().Seconds()))
		time.Sleep(config.GetUserBatchPause())
		hostOperatorPodMemory, err := hostAwait.GetMemoryUsage(hostOperatorPod.Name, hostAwait.Namespace)
		require.NoError(t, err)
		memberOperatorPodMemory, err := memberAwait.GetMemoryUsage(memberOperatorPod.Name, memberAwait.Namespace)
		require.NoError(t, err)
		logger.Info("done provisioning resources",
			"user_count", config.GetUserBatchSize(),
			"duration_ms", end.Sub(start).Milliseconds(),
			"host_operator_pod_memory_usage_kb", hostOperatorPodMemory,
			"member_operator_pod_memory_usage_kb", memberOperatorPodMemory)
	}
	t.Logf("done provisionning the %d requested accounts", config.GetUserCount())

}
