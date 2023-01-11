package e2e

import (
	"context"
	"encoding/json"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestForceMetricsSynchronization(t *testing.T) {
	t.Skip("skipping this test due to flakyness")

	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	hostAwait.UpdateToolchainConfig(t,
		testconfig.AutomaticApproval().Enabled(true),
		testconfig.Metrics().ForceSynchronization(false))

	userSignups := CreateMultipleSignups(t, awaitilities, memberAwait, 2)

	// delete the current toolchainstatus/toolchain-status resource and restart the host-operator pod,
	// so we can start with accurate counters/metrics and not get flaky because of previous tests,
	// in particular w.r.t the `userSignupsPerActivationAndDomain` counter which is not decremented when a user
	// is deleted
	err := hostAwait.DeleteToolchainStatus(t, "toolchain-status")
	require.NoError(t, err)
	// restarting the pod after the `toolchain-status` resource was deleted will trigger a recount based on resources
	err = hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})
	require.NoError(t, err)

	metricsAssertion := InitMetricsAssertion(t, awaitilities)

	t.Run("tampering activation-counter annotations", func(t *testing.T) {

		// change the `toolchain.dev.openshift.com/activation-counter` annotation value
		for _, userSignup := range userSignups {
			// given
			annotations := userSignup.Annotations
			annotations[toolchainv1alpha1.UserSignupActivationCounterAnnotationKey] = "10"
			// when
			mergePatch, err := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": annotations,
				},
			})
			require.NoError(t, err)
			err = hostAwait.Client.Patch(context.TODO(), userSignup, client.RawPatch(types.MergePatchType, mergePatch))
			// then
			require.NoError(t, err)
		}

		t.Run("verify metrics did not change after restarting pod without forcing recount", func(t *testing.T) {
			// given
			hostAwait.UpdateToolchainConfig(t, testconfig.Metrics().ForceSynchronization(false))

			// when restarting the pod
			err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

			// then
			require.NoError(t, err)
			// metrics have not changed yet
			metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "external")                       // value was increased by 1
			metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // value was increased by 1
		})

		t.Run("verify metrics are still correct after restarting pod and forcing recount", func(t *testing.T) {
			// given
			hostAwait.UpdateToolchainConfig(t, testconfig.Metrics().ForceSynchronization(true))

			// when restarting the pod
			// TODO: unneeded once the ToolchainConfig controller will be in place ?
			err := hostAwait.DeletePods(client.InNamespace(hostAwait.Namespace), client.MatchingLabels{"name": "controller-manager"})

			// then
			require.NoError(t, err)
			// metrics have been updated
			metricsAssertion.WaitForMetricDelta(t, MasterUserRecordsPerDomainMetric, 0, "domain", "external")                        // unchanged
			metricsAssertion.WaitForMetricDelta(t, UsersPerActivationsAndDomainMetric, 2, "activations", "10", "domain", "external") // updated

		})
	})

}
