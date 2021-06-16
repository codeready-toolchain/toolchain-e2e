package e2e

import (
	"context"
	"strconv"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestToolchainClusterE2E(t *testing.T) {
	toolchainClusterList := &toolchainv1alpha1.ToolchainClusterList{}
	ctx, hostAwait, memberAwait, _ := WaitForDeployments(t, toolchainClusterList)
	defer ctx.Cleanup()

	verifyToolchainCluster(t, hostAwait.Awaitility, memberAwait.Awaitility)
	verifyToolchainCluster(t, memberAwait.Awaitility, hostAwait.Awaitility)
}

// verifyToolchainCluster verifies existence and correct conditions of ToolchainCluster CRD
// in the target cluster type operator
func verifyToolchainCluster(t *testing.T, await *wait.Awaitility, otherAwait *wait.Awaitility) {
	// given
	current, ok, err := await.GetToolchainCluster(otherAwait.Type, otherAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, ok, "ToolchainCluster should exist")

	t.Run("create new ToolchainCluster with correct data and expect to be ready for cluster type "+string(await.Type), func(t *testing.T) {
		// given
		name := "new-ready-" + string(await.Type)
		toolchainCluster := newToolchainCluster(await.Namespace, name,
			clusterType(await.Type),
			apiEndpoint(current.Spec.APIEndpoint),
			caBundle(current.Spec.CABundle),
			secretRef(current.Spec.SecretRef.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		t.Cleanup(func() {
			if err := await.Client.Delete(context.TODO(), toolchainCluster); err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)

		// then the ToolchainCluster should be ready
		require.NoError(t, err)
		_, err = await.WaitForNamedToolchainClusterWithCondition(toolchainCluster.Name, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainClusterWithCondition(otherAwait.Type, otherAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainClusterWithCondition(await.Type, await.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
	})

	t.Run("create new ToolchainCluster with incorrect data and expect to be offline for cluster type "+string(await.Type), func(t *testing.T) {
		// given
		name := "new-offline-" + string(await.Type)
		toolchainCluster := newToolchainCluster(await.Namespace, name,
			clusterType(await.Type),
			apiEndpoint("https://1.2.3.4:8443"),
			caBundle(current.Spec.CABundle),
			secretRef(current.Spec.SecretRef.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		t.Cleanup(func() {
			if err := await.Client.Delete(context.TODO(), toolchainCluster); err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)
		// then the ToolchainCluster should be offline
		require.NoError(t, err)
		_, err = await.WaitForNamedToolchainClusterWithCondition(toolchainCluster.Name, &toolchainv1alpha1.ToolchainClusterCondition{
			Type:   toolchainv1alpha1.ToolchainClusterOffline,
			Status: corev1.ConditionTrue,
		})
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainClusterWithCondition(otherAwait.Type, otherAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainClusterWithCondition(await.Type, await.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
	})
}

func newToolchainCluster(namespace, name string, options ...clusterOption) *toolchainv1alpha1.ToolchainCluster {
	toolchainCluster := &toolchainv1alpha1.ToolchainCluster{
		Spec: toolchainv1alpha1.ToolchainClusterSpec{
			SecretRef: toolchainv1alpha1.LocalSecretReference{
				Name: "", // default
			},
			APIEndpoint: "", // default
			CABundle:    "", // default
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"type":             "member",
				"ownerClusterName": "east",
			},
		},
	}
	for _, configure := range options {
		configure(toolchainCluster)
	}
	return toolchainCluster
}

// clusterOption an option to configure the cluster to use in the tests
type clusterOption func(*toolchainv1alpha1.ToolchainCluster)

// capacityExhausted an option to state that the cluster capacity has exhausted
var capacityExhausted clusterOption = func(c *toolchainv1alpha1.ToolchainCluster) {
	c.Labels["toolchain.dev.openshift.com/capacity-exhausted"] = strconv.FormatBool(true)
}

// Type sets the label which defines the type of cluster
func clusterType(t cluster.Type) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["type"] = string(t)
	}
}

// Owner sets the 'ownerClusterName' label
func owner(name string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["ownerClusterName"] = name
	}
}

// Namespace sets the 'namespace' label
func namespace(name string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["namespace"] = name
	}
}

// SecretRef sets the SecretRef in the cluster's Spec
func secretRef(ref string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.SecretRef = toolchainv1alpha1.LocalSecretReference{
			Name: ref,
		}
	}
}

// APIEndpoint sets the APIEndpoint in the cluster's Spec
func apiEndpoint(url string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.APIEndpoint = url
	}
}

// CABundle sets the CABundle in the cluster's Spec
func caBundle(bundle string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.CABundle = bundle
	}
}

func TestForceMetricsSynchronization(t *testing.T) {
	// given
	toolchainClusterList := &toolchainv1alpha1.ToolchainClusterList{}
	ctx, hostAwait, memberAwait, member2Await := WaitForDeployments(t, toolchainClusterList)
	defer ctx.Cleanup()
	hostAwait.UpdateToolchainConfig(
		testconfig.AutomaticApproval().Enabled(),
		testconfig.Metrics().ForceSynchronization(false))
	t.Cleanup(func() {
		err := hostAwait.ScaleDeployment(hostAwait.Namespace, "host-operator", 1)
		require.NoError(t, err)
	})
	// before creating a batch of users, let's remove all remainings of previous tests, so
	// we know exactly what numbers we're dealing with
	userSignups := &toolchainv1alpha1.UserSignupList{}
	err := hostAwait.Client.List(context.TODO(), userSignups, client.InNamespace(hostAwait.Namespace))
	require.NoError(t, err)
	for _, userSignup := range userSignups.Items {
		err := hostAwait.Client.Delete(context.TODO(), &userSignup)
		require.NoError(t, err)
	}
	// then wait until all UserSignups have been deleted
	for _, userSignup := range userSignups.Items {
		hostAwait.WaitUntilUserSignupDeleted(userSignup.Name)
	}
	// now we can create our new users ;)
	CreateMultipleSignups(t, ctx, hostAwait, memberAwait, 5) // 5 external users

	// when
	metricsAssertion := InitMetricsAssertion(t, hostAwait, []string{memberAwait.ClusterName, member2Await.ClusterName})

	t.Run("verify metrics are still correct after restarting pod", func(t *testing.T) {
		// when
		err := hostAwait.DeletePods(client.MatchingLabels{"name": "host-operator"})
		// then
		require.NoError(t, err)
		metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 0, "domain", "external")                       // unchanged compared to before deleting the pod
		metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 0, "activations", "1", "domain", "external") // unchanged compared to before deleting the pod
	})

	t.Run("tampering ToolchainStatus", func(t *testing.T) {

		t.Run("verify metrics are still correct after restarting pod but not forcing recount", func(t *testing.T) {
			// given
			hostAwait.UpdateToolchainConfig(testconfig.Metrics().ForceSynchronization(false))
			err := hostAwait.ScaleDeployment(hostAwait.Namespace, "host-operator", 0) // make sure it's "shut-down" while tampering the ToolchainStatus resource
			require.NoError(t, err)
			// tamper the values in ToolchainStatus.status.metrics
			toolchainStatus, err := hostAwait.WaitForToolchainStatus()
			require.NoError(t, err)
			toolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey] = toolchainv1alpha1.Metric{
				"external": toolchainStatus.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]["external"] + 1, // increase current value by 1
			}
			toolchainStatus.Status.Metrics[toolchainv1alpha1.UserSignupsPerActivationAndDomainMetricKey] = toolchainv1alpha1.Metric{
				"1,external": toolchainStatus.Status.Metrics[toolchainv1alpha1.UserSignupsPerActivationAndDomainMetricKey]["1,external"] + 1, // increase current value by 1
			}
			err = hostAwait.Client.Status().Update(context.TODO(), toolchainStatus)
			require.NoError(t, err)
			// when restarting the pod
			err = hostAwait.ScaleDeployment(hostAwait.Namespace, "host-operator", 1)
			require.NoError(t, err)

			// then values changed
			metricsAssertion.WaitForMetricDelta(MasterUserRecordsPerDomainMetric, 1, "domain", "external")                       // value was increased by 1
			metricsAssertion.WaitForMetricDelta(UsersPerActivationsAndDomainMetric, 1, "activations", "1", "domain", "external") // value was increased by 1
		})

		t.Run("verify metrics are still correct after restarting pod and forcing recount", func(t *testing.T) {
			// given
			hostAwait.UpdateToolchainConfig(testconfig.Metrics().ForceSynchronization(true))
			// when (forcing restart)
			err := hostAwait.DeletePods(client.MatchingLabels{"name": "host-operator"})
			// then
			require.NoError(t, err)
			// here we can check the exact metric values (not the deltas) because we know exactly how many usersignups we have
			metricsAssertion.WaitForMetric(MasterUserRecordsPerDomainMetric, 5, "domain", "external")                       // unchanged compared to tampering the ToolchainStatus
			metricsAssertion.WaitForMetric(UsersPerActivationsAndDomainMetric, 5, "activations", "1", "domain", "external") // unchanged compared to tampering the ToolchainStatus

		})
	})

}
